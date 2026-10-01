// Package verify is the nutrient test: a candidate lesson is replayed against
// past episodes with and without it, a judge compares each pair, and the
// lesson moves forward only if it measurably helps where it should and harms
// nothing where it shouldn't (rubric C6, docs/architecture.md).
//
// Two sets of episodes are replayed:
//
//   - relevant: the episodes the lesson was learned from, plus similar ones
//     found through episodic memory. Here the lesson must win.
//   - regression: past successes in the lesson's scope that it is not about.
//     Here it must not lose. A lesson that fixes one thing by breaking
//     another is not nourishing.
//
// The decision is deliberately simple and recorded with every count, so a
// reader can recompute it from the evidence (docs/decisions.md, D-015).
package verify

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"

	"github.com/Nibir1/metabolism/episodic"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Config holds the decision rule. The zero value selects the defaults.
type Config struct {
	MaxRelevant      int     // relevant episodes replayed (default 8)
	RegressionSample int     // past successes replayed (default 6)
	MinCompared      int     // fewer relevant comparisons is inconclusive (default 3)
	MinWins          int     // wins needed to pass (default 2)
	WinRatio         float64 // wins must be ≥ WinRatio × losses (default 3)
	// QuarantineMinWins is the bar for a lesson learned from untrusted
	// content: this many wins and no losses at all (default 4).
	QuarantineMinWins int
	// MinConfidence below which a verdict counts as a tie (default 0.6).
	MinConfidence float64
}

func (c Config) withDefaults() Config {
	if c.MaxRelevant <= 0 {
		c.MaxRelevant = 8
	}
	if c.RegressionSample <= 0 {
		c.RegressionSample = 6
	}
	if c.MinCompared <= 0 {
		c.MinCompared = 3
	}
	if c.MinWins <= 0 {
		c.MinWins = 2
	}
	if c.WinRatio <= 0 {
		c.WinRatio = 3
	}
	if c.QuarantineMinWins <= 0 {
		c.QuarantineMinWins = 4
	}
	if c.MinConfidence <= 0 {
		c.MinConfidence = 0.6
	}
	return c
}

// History is the past the test replays: episodes, their outcomes, and an
// index to find which ones a lesson is about.
type History struct {
	byID     map[types.ID]types.Episode
	outcomes map[types.ID][]types.Outcome
	order    []types.ID
	index    *episodic.Index
}

// NewHistory builds a History.
func NewHistory(eps []types.Episode, outcomes map[types.ID][]types.Outcome) *History {
	h := &History{byID: map[types.ID]types.Episode{}, outcomes: outcomes, index: episodic.New(episodic.Options{})}
	for _, ep := range eps {
		if _, dup := h.byID[ep.ID]; dup {
			continue
		}
		h.byID[ep.ID] = ep
		h.order = append(h.order, ep.ID)
		h.index.Add(ep)
		for _, o := range outcomes[ep.ID] {
			h.index.AddOutcome(o)
		}
	}
	return h
}

// Episode returns a past episode and its outcomes.
func (h *History) Episode(id types.ID) (types.Episode, []types.Outcome, bool) {
	ep, ok := h.byID[id]
	return ep, h.outcomes[id], ok
}

// LoadHistory reads every episode and outcome from a journal.
func LoadHistory(ctx context.Context, j ports.Journal) (*History, error) {
	var eps []types.Episode
	outs := map[types.ID][]types.Outcome{}
	q := types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode, types.RecordOutcome}}
	for r, err := range j.Scan(ctx, q) {
		if err != nil {
			return nil, err
		}
		if r.Episode != nil {
			eps = append(eps, *r.Episode)
		} else {
			outs[r.Outcome.EpisodeID] = append(outs[r.Outcome.EpisodeID], *r.Outcome)
		}
	}
	return NewHistory(eps, outs), nil
}

// Verifier runs nutrient tests. It caches the without-lesson replays, which
// are the same for every lesson tested against an episode; with a real host
// each replay is a model call.
type Verifier struct {
	Host    ports.Host
	Judge   ports.Judge
	History *History
	Config  Config

	baseline map[types.ID]types.ReplayResult
}

// Comparison is one replayed pair, for reports and for labelling.
type Comparison struct {
	Episode    types.ID      `json:"episode"`
	Regression bool          `json:"regression_set"`
	Verdict    types.Verdict `json:"verdict"`
	// Judged is false when the two plans were identical and no judge was
	// asked. Only judged comparisons are worth a person's label.
	Judged  bool               `json:"judged"`
	With    types.ReplayResult `json:"with"`
	Without types.ReplayResult `json:"without"`
}

// Report is what a test did, beyond the evidence it recorded.
type Report struct {
	Lesson      types.ID          `json:"lesson"`
	From        types.LessonState `json:"from"`
	To          types.LessonState `json:"to"`
	Evidence    types.Evidence    `json:"evidence"`
	Comparisons []Comparison      `json:"comparisons"`
}

// Test runs the nutrient test on one candidate or quarantined lesson,
// records the evidence, and moves the lesson: to trial on a pass, to
// eliminated on a fail, nowhere when inconclusive.
func (v *Verifier) Test(ctx context.Context, m *metabolome.Metabolome, id types.ID) (Report, error) {
	cfg := v.Config.withDefaults()
	entry, ok := m.Get(id)
	if !ok {
		return Report{}, fmt.Errorf("%w: %s", ports.ErrUnknownLesson, id)
	}
	lesson := entry.Lesson
	if lesson.State != types.StateCandidate && lesson.State != types.StateQuarantined {
		return Report{}, fmt.Errorf("lesson %s is %s; only candidates and quarantined lessons are tested", id, lesson.State)
	}
	if v.baseline == nil {
		v.baseline = map[types.ID]types.ReplayResult{}
	}

	relevant := v.relevant(lesson, cfg.MaxRelevant)
	regression := v.regression(lesson, relevant, cfg.RegressionSample)

	rep := Report{Lesson: id, From: lesson.State, To: lesson.State}
	ev := types.Evidence{LessonID: id, Kind: types.EvidenceNutrientTest, Judge: v.Judge.Name()}
	for _, set := range []struct {
		eps        []types.ID
		regression bool
	}{{relevant, false}, {regression, true}} {
		for _, eid := range set.eps {
			c, err := v.compare(ctx, eid, lesson)
			if err != nil {
				return Report{}, err
			}
			c.Regression = set.regression
			verdict := c.Verdict
			ev.Episodes = append(ev.Episodes, eid)
			rep.Comparisons = append(rep.Comparisons, c)
			decisive := verdict.Confidence >= cfg.MinConfidence
			switch {
			case set.regression:
				if decisive && verdict.Prefer == types.PreferWithout {
					ev.Regressions++
				}
			case decisive && verdict.Prefer == types.PreferWith:
				ev.Compared++
				ev.Wins++
			case decisive && verdict.Prefer == types.PreferWithout:
				ev.Compared++
				ev.Losses++
			default:
				ev.Compared++
				ev.Ties++
			}
		}
	}

	ev.Result, ev.Summary = decide(ev, lesson.State == types.StateQuarantined, cfg)
	recorded, err := m.RecordEvidence(ctx, ev)
	if err != nil {
		return Report{}, err
	}
	rep.Evidence = recorded

	switch recorded.Result {
	case types.ResultPass:
		if err := m.Move(ctx, id, types.StateTrial, "passed the nutrient test: "+recorded.Summary, recorded.ID); err != nil {
			return Report{}, err
		}
		rep.To = types.StateTrial
	case types.ResultFail:
		if err := m.Move(ctx, id, types.StateEliminated, "failed the nutrient test: "+recorded.Summary, recorded.ID); err != nil {
			return Report{}, err
		}
		rep.To = types.StateEliminated
	}
	return rep, nil
}

// decide applies the rule. Every branch says why, in a sentence that names the
// counts, because the summary is what a person reads in `lessons why`.
func decide(ev types.Evidence, quarantined bool, cfg Config) (types.TestResult, string) {
	counts := fmt.Sprintf("%d wins, %d losses, %d ties over %d relevant episodes; %d regressions",
		ev.Wins, ev.Losses, ev.Ties, ev.Compared, ev.Regressions)
	switch {
	case ev.Regressions > 0:
		return types.ResultFail, "harms past successes it is not about: " + counts
	case ev.Compared < cfg.MinCompared:
		return types.ResultInconclusive, fmt.Sprintf("too few relevant episodes to judge (need %d): %s", cfg.MinCompared, counts)
	case ev.Wins == 0 && ev.Losses == 0:
		return types.ResultFail, "changes nothing where it should help: " + counts
	case quarantined && (ev.Wins < cfg.QuarantineMinWins || ev.Losses > 0):
		return types.ResultFail, fmt.Sprintf("quarantined lessons need %d wins and no losses: %s", cfg.QuarantineMinWins, counts)
	case !quarantined && (ev.Wins < cfg.MinWins || float64(ev.Wins) < cfg.WinRatio*float64(ev.Losses)):
		return types.ResultFail, fmt.Sprintf("needs %d wins and %g× more wins than losses: %s", cfg.MinWins, cfg.WinRatio, counts)
	}
	return types.ResultPass, "helps where it should and harms nothing: " + counts
}

func (v *Verifier) compare(ctx context.Context, eid types.ID, lesson types.Lesson) (Comparison, error) {
	ep := v.History.byID[eid]
	without, ok := v.baseline[eid]
	if !ok {
		var err error
		without, err = v.Host.Replay(ctx, ep, nil)
		if err != nil {
			return Comparison{}, fmt.Errorf("replay %s without lessons: %w", eid, err)
		}
		v.baseline[eid] = without
	}
	with, err := v.Host.Replay(ctx, ep, []types.Lesson{lesson})
	if err != nil {
		return Comparison{}, fmt.Errorf("replay %s with lesson %s: %w", eid, lesson.ID, err)
	}
	c := Comparison{Episode: eid, With: with, Without: without}
	// The lesson changed nothing here. That is a tie by definition, so no
	// judge is asked: a model shown two identical plans can only add noise,
	// and one confident coin-flip on a regression episode would fail a good
	// lesson. It also saves a judge call for most regression episodes.
	if sameReplay(with, without) {
		c.Verdict = types.Verdict{Prefer: types.PreferNeither, Confidence: 1, Reason: "identical plans"}
		return c, nil
	}
	c.Verdict, err = v.Judge.Compare(ctx, ep, v.History.outcomes[eid], with, without)
	if err != nil {
		return Comparison{}, fmt.Errorf("judge %s: %w", eid, err)
	}
	c.Judged = true
	return c, nil
}

// relevant returns the lesson's sources first, then the episodes episodic
// memory finds for the lesson's text and for each source's request, in its
// scope, up to max.
func (v *Verifier) relevant(l types.Lesson, max int) []types.ID {
	var out []types.ID
	add := func(id types.ID) {
		if len(out) < max && !slices.Contains(out, id) {
			if _, ok := v.History.byID[id]; ok && inScope(v.History.byID[id], l.Scope) {
				out = append(out, id)
			}
		}
	}
	for _, id := range l.Sources {
		add(id)
	}
	queries := []string{l.Text}
	for _, id := range l.Sources {
		queries = append(queries, v.History.byID[id].Request.Text)
	}
	for _, q := range queries {
		for _, c := range v.History.index.Retrieve(episodic.Query{Text: q, Scope: l.Scope, K: max}) {
			add(c.Episode.ID)
		}
	}
	return out
}

// regression returns a deterministic sample of past successes in the lesson's
// scope that are not in the relevant set. The order is a hash of lesson and
// episode IDs, so a test reruns identically and different lessons sample
// different successes.
func (v *Verifier) regression(l types.Lesson, relevant []types.ID, n int) []types.ID {
	var pool []types.ID
	for _, id := range v.History.order {
		ep := v.History.byID[id]
		if slices.Contains(relevant, id) || !inScope(ep, l.Scope) || !ep.Request.Provenance.Trusted() {
			continue
		}
		if types.Succeeded(ep, v.History.outcomes[id]) {
			pool = append(pool, id)
		}
	}
	slices.SortFunc(pool, func(a, b types.ID) int {
		ha, hb := mix(l.ID, a), mix(l.ID, b)
		switch {
		case ha < hb:
			return -1
		case ha > hb:
			return 1
		}
		return 0
	})
	if len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

func inScope(ep types.Episode, s types.Scope) bool {
	switch s.Level {
	case types.ScopeProject:
		return ep.Scope == s
	case types.ScopeMachine:
		return ep.Attrs["machine"] == s.Key || ep.Scope == s
	}
	return true
}

func mix(a, b types.ID) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(a))
	_, _ = h.Write([]byte(b))
	return h.Sum64()
}

func sameReplay(a, b types.ReplayResult) bool {
	return a.Text == b.Text && slices.Equal(a.Steps, b.Steps)
}
