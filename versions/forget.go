package versions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// ForgetRequest says what to forget.
type ForgetRequest struct {
	Episodes []types.ID
	Lessons  []types.ID
	// Match forgets every episode and lesson whose records contain this
	// text anywhere (a pasted key, a name, a path).
	Match string
	// KeepLessons keeps the lessons learned from a forgotten episode,
	// eliminated with the reason, instead of forgetting them with it.
	KeepLessons bool
}

// ForgetPlan is what a request would delete, before anything is.
type ForgetPlan struct {
	// Episodes and Lessons are the subjects; Records every record that goes
	// with them (an episode's outcomes; a lesson's events and evidence).
	Episodes []types.ID     `json:"episodes"`
	Lessons  []types.Lesson `json:"lessons"`
	// Learned are the lessons forgotten because they were learned from a
	// forgotten episode (a subset of Lessons).
	Learned []types.ID `json:"learned_from_forgotten,omitempty"`
	// Eliminate are those lessons when KeepLessons is set: kept, eliminated.
	Eliminate []types.Lesson       `json:"eliminate,omitempty"`
	Targets   []types.ForgetTarget `json:"targets"`
}

// IDs returns every ID the plan deletes, for purging host files too.
func (p ForgetPlan) IDs() map[types.ID]bool {
	out := map[types.ID]bool{}
	for _, t := range p.Targets {
		out[t.ID] = true
	}
	return out
}

// PlanForget works out what a request deletes (D-028). An episode goes with
// its outcomes; a lesson with its events and evidence. A lesson learned from
// a forgotten episode goes too, since its words may carry what was meant to
// be forgotten, unless KeepLessons asks to eliminate it instead. Match finds
// episodes and lessons whose records contain the text: an outcome that
// matches takes its episode, and an event or evidence takes its lesson.
func PlanForget(ctx context.Context, j ports.Journal, m *metabolome.Metabolome, req ForgetRequest) (ForgetPlan, error) {
	var plan ForgetPlan
	episodes := map[types.ID]bool{}
	lessons := map[types.ID]bool{}
	for _, id := range req.Episodes {
		episodes[id] = true
	}
	for _, id := range req.Lessons {
		if _, ok := m.Get(id); !ok {
			return plan, fmt.Errorf("%w: %s", ports.ErrUnknownLesson, id)
		}
		lessons[id] = true
	}
	known := map[types.ID]bool{}
	for r, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{
		types.RecordEpisode, types.RecordOutcome, types.RecordLesson, types.RecordEvent, types.RecordEvidence}}) {
		if err != nil {
			return plan, err
		}
		if r.Episode != nil {
			known[r.Episode.ID] = true
		}
		if req.Match == "" {
			continue
		}
		body, err := json.Marshal(r)
		if err != nil {
			return plan, err
		}
		if !strings.Contains(string(body), req.Match) && !strings.Contains(string(body), jsonEscaped(req.Match)) {
			continue
		}
		switch {
		case r.Episode != nil:
			episodes[r.Episode.ID] = true
		case r.Outcome != nil:
			episodes[r.Outcome.EpisodeID] = true
		case r.Lesson != nil:
			lessons[r.Lesson.ID] = true
		case r.Event != nil:
			lessons[r.Event.LessonID] = true
		case r.Evidence != nil:
			lessons[r.Evidence.LessonID] = true
		}
	}
	for id := range episodes {
		if !known[id] {
			return plan, fmt.Errorf("%w: no episode %s", ports.ErrInvalid, id)
		}
	}
	direct := map[types.ID]bool{}
	for id := range lessons {
		direct[id] = true
	}
	for _, l := range m.Lessons() {
		if direct[l.ID] || !slices.ContainsFunc(l.Sources, func(id types.ID) bool { return episodes[id] }) {
			continue
		}
		if req.KeepLessons {
			if l.State != types.StateEliminated {
				plan.Eliminate = append(plan.Eliminate, l)
			}
			continue
		}
		lessons[l.ID] = true
		plan.Learned = append(plan.Learned, l.ID)
	}
	if len(episodes) == 0 && len(lessons) == 0 {
		return plan, errNothing
	}

	add := func(q types.JournalQuery) error {
		for r, err := range j.Scan(ctx, q) {
			if err != nil {
				return err
			}
			t := types.ForgetTarget{Kind: r.Kind}
			switch {
			case r.Episode != nil:
				t.ID = r.Episode.ID
			case r.Outcome != nil:
				t.ID = r.Outcome.ID
			case r.Lesson != nil:
				t.ID, t.TextSHA256 = r.Lesson.ID, types.TextDigest(r.Lesson.Text)
			case r.Event != nil:
				t.ID = r.Event.ID
			case r.Evidence != nil:
				t.ID = r.Evidence.ID
			default:
				continue
			}
			plan.Targets = append(plan.Targets, t)
		}
		return nil
	}
	for _, id := range sorted(episodes) {
		plan.Episodes = append(plan.Episodes, id)
		if err := add(types.JournalQuery{EpisodeID: id}); err != nil {
			return plan, err
		}
	}
	for _, id := range sorted(lessons) {
		e, _ := m.Get(id)
		plan.Lessons = append(plan.Lessons, e.Lesson)
		if err := add(types.JournalQuery{LessonID: id}); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

var errNothing = errors.New("nothing matches: no record would be forgotten")

// ErrNothingToForget reports whether err says a request matched nothing.
func ErrNothingToForget(err error) bool { return errors.Is(err, errNothing) }

// Forget carries out a plan: the kept lessons are eliminated with the
// reason, then every target is deleted under one audit record, and the file
// is compacted so the bytes leave the disk.
func Forget(ctx context.Context, j ports.Journal, m *metabolome.Metabolome, plan ForgetPlan, reason, by string, now time.Time) (types.Forget, error) {
	for _, l := range plan.Eliminate {
		if err := m.Move(ctx, l.ID, types.StateEliminated, metabolome.ReasonSourceForgotten+": "+reason); err != nil {
			return types.Forget{}, fmt.Errorf("lesson %s: %w", l.ID, err)
		}
	}
	f, err := j.Forget(ctx, types.Forget{ID: types.NewID(now), At: now.UTC(), Reason: reason, By: by,
		Targets: slices.Clone(plan.Targets)})
	if err != nil {
		return f, err
	}
	if err := j.Compact(ctx); err != nil {
		return f, err
	}
	return f, m.Refresh(ctx)
}

func sorted(set map[types.ID]bool) []types.ID {
	out := make([]types.ID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// jsonEscaped is s as it appears inside a JSON string.
func jsonEscaped(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
