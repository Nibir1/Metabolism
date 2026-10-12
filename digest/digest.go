// Package digest is the breakdown stage: it hands experience to a Reflector
// and turns what comes back into candidate lessons, or refuses it.
//
// The reflector proposes; this package disposes. Everything a reflector says
// is treated as untrusted output: its sources must be episodes it was actually
// shown, its kinds and scopes must be ones the host accepts, its text must pass
// the immune screen, and it must not repeat a lesson already known (including
// one already eliminated, so a rejected lesson cannot return by being proposed
// again, or one the user forgot with proof). Only then is it recorded, as a candidate or, if learned from
// untrusted content, quarantined.
package digest

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/immune"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Config tunes a digest run.
type Config struct {
	Policy immune.Policy
	// MaxLessons bounds what one run may record (default 10): a reflector
	// that floods the metabolome is broken, not insightful.
	MaxLessons int
	// Exclude refuses lessons whose text matches it: what a host knows is
	// about its own machinery, not the task (the coding host's provider
	// errors and answer formats), so advice about it cannot help the work.
	Exclude *regexp.Regexp
}

// Outcome of one proposal.
type Decision struct {
	Text   string   `json:"text"`
	Lesson types.ID `json:"lesson,omitempty"`
	State  string   `json:"state"` // candidate | quarantined | rejected
	Reason string   `json:"reason,omitempty"`
}

// Result summarises a digest run.
type Result struct {
	Reflector string     `json:"reflector"`
	Batch     int        `json:"batch"`
	Proposed  int        `json:"proposed"`
	Recorded  int        `json:"recorded"`
	Decisions []Decision `json:"decisions"`
}

// Run reflects on batch and records what survives.
func Run(ctx context.Context, m *metabolome.Metabolome, r ports.Reflector, batch []types.Experience, cfg Config) (Result, error) {
	if len(cfg.Policy.Kinds) == 0 {
		cfg.Policy = immune.DefaultPolicy()
	}
	if cfg.MaxLessons <= 0 {
		cfg.MaxLessons = 10
	}
	res := Result{Reflector: r.Name(), Batch: len(batch)}
	if len(batch) == 0 {
		return res, nil
	}

	byID := map[types.ID]types.Episode{}
	for _, x := range batch {
		byID[x.Episode.ID] = x.Episode
	}
	known := map[string]types.LessonState{}
	for _, l := range m.Lessons() {
		known[normalize(l.Text)] = l.State
	}

	proposals, err := r.Reflect(ctx, batch, m.Lessons())
	if err != nil {
		return res, fmt.Errorf("reflect: %w", err)
	}
	res.Proposed = len(proposals)

	for _, p := range proposals {
		d := Decision{Text: p.Text}
		reject := func(reason string) {
			d.State, d.Reason = "rejected", reason
			res.Decisions = append(res.Decisions, d)
		}
		if res.Recorded >= cfg.MaxLessons {
			reject(fmt.Sprintf("over the per-run limit of %d", cfg.MaxLessons))
			continue
		}
		lesson, why := shape(p, byID, r.Name())
		if why != "" {
			reject(why)
			continue
		}
		if cfg.Exclude != nil && cfg.Exclude.MatchString(lesson.Text) {
			reject("about the host's own machinery, not the task (-exclude)")
			continue
		}
		if err := immune.Screen(lesson, cfg.Policy); err != nil {
			reject(err.Error())
			continue
		}
		// A lesson the user forgot with proof is gone, words and all; only
		// its digest is kept, so it is not learned again (D-028).
		if m.ForgottenText(lesson.Text) {
			reject("forgotten by the user")
			continue
		}
		key := normalize(lesson.Text)
		if st, dup := known[key]; dup {
			reject("already known (" + string(st) + ")")
			continue
		}
		created, err := m.Create(ctx, lesson, "proposed by "+r.Name()+": "+orDefault(lesson.Rationale, "no rationale given"))
		if err != nil {
			return res, err
		}
		known[key] = created.State
		d.Lesson, d.State = created.ID, string(created.State)
		res.Decisions = append(res.Decisions, d)
		res.Recorded++
	}
	return res, nil
}

// shape turns a proposal into a lesson the engine owns, or says why it can't.
func shape(p types.Lesson, byID map[types.ID]types.Episode, origin string) (types.Lesson, string) {
	if len(p.Sources) == 0 {
		return types.Lesson{}, "names no source episodes"
	}
	var provs []types.Provenance
	seen := map[types.ID]bool{}
	var sources []types.ID
	scopeKeys := map[string]bool{}
	for _, id := range p.Sources {
		ep, ok := byID[id]
		if !ok {
			return types.Lesson{}, fmt.Sprintf("cites episode %s, which it was not shown", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		sources = append(sources, id)
		provs = append(provs, ep.Request.Provenance)
		scopeKeys[string(ep.Scope.Level)+":"+ep.Scope.Key] = true
		if m := ep.Attrs["machine"]; m != "" {
			scopeKeys["machine:"+m] = true
		}
	}
	scope := p.Scope
	if scope.Level == "" {
		scope.Level = types.ScopeGlobal
	}
	if err := scope.Validate(); err != nil {
		return types.Lesson{}, "scope: " + err.Error()
	}
	if scope.Level != types.ScopeGlobal && !scopeKeys[string(scope.Level)+":"+scope.Key] {
		return types.Lesson{}, fmt.Sprintf("scope %s:%s matches none of its sources", scope.Level, scope.Key)
	}
	return types.Lesson{
		Kind:       p.Kind,
		Scope:      scope,
		Text:       strings.TrimSpace(p.Text),
		Sources:    sources,
		Provenance: immune.LeastTrusted(provs...),
		Rationale:  strings.TrimSpace(p.Rationale),
		Origin:     origin,
	}, ""
}

// normalize folds case, punctuation and spacing, so trivially reworded copies
// of a lesson count as the same lesson.
func normalize(s string) string { return types.NormalizeText(s) }

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// Batch builds experiences from journal episodes started in [since, until),
// with every outcome known now.
func Batch(ctx context.Context, j ports.Journal, since, until time.Time) ([]types.Experience, error) {
	var (
		eps  []types.Episode
		outs = map[types.ID][]types.Outcome{}
	)
	q := types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode, types.RecordOutcome}}
	for r, err := range j.Scan(ctx, q) {
		if err != nil {
			return nil, err
		}
		switch {
		case r.Episode != nil:
			at := r.Episode.StartedAt
			if (!since.IsZero() && at.Before(since)) || (!until.IsZero() && !at.Before(until)) {
				continue
			}
			eps = append(eps, *r.Episode)
		case r.Outcome != nil:
			outs[r.Outcome.EpisodeID] = append(outs[r.Outcome.EpisodeID], *r.Outcome)
		}
	}
	batch := make([]types.Experience, 0, len(eps))
	for _, ep := range eps {
		batch = append(batch, types.Experience{Episode: ep, Outcomes: outs[ep.ID], Succeeded: types.Succeeded(ep, outs[ep.ID])})
	}
	return batch, nil
}
