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
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"slices"

	"github.com/Nibir1/metabolism/episodic"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Config holds the decision rule (D-018). The zero value selects the defaults.
type Config struct {
	MaxRelevant      int // relevant episodes replayed (default 8)
	RegressionSample int // past successes replayed (default 6)
	MinCompared      int // fewer relevant comparisons is inconclusive (default 3)
	MinWins          int // decisive wins needed to pass (default 2)
	// Alpha is the level for "helps", which moves a lesson to trial
	// (default 0.1). Trial is reversible: outcome credit keeps testing it.
	Alpha float64
	// HarmAlpha is the level for "harms" and "makes things worse", which
	// eliminate a lesson (default 0.02). Elimination is permanent, so it
	// needs much stronger evidence than promotion. MinHarm is the fewest
	// decisive losses that can eliminate (default 2).
	HarmAlpha float64
	MinHarm   int
	// QuarantineMinWins and QuarantineAlpha are the stricter bar for a lesson
	// learned from untrusted content (defaults 4 and 0.02).
	QuarantineMinWins int
	QuarantineAlpha   float64
	// MinConfidence below which a verdict counts as a tie (default 0.6).
	MinConfidence float64
}

func (c Config) withDefaults() Config {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	deff := func(v *float64, d float64) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&c.MaxRelevant, 8)
	def(&c.RegressionSample, 6)
	def(&c.MinCompared, 3)
	def(&c.MinWins, 2)
	def(&c.MinHarm, 2)
	def(&c.QuarantineMinWins, 4)
	deff(&c.Alpha, 0.1)
	deff(&c.HarmAlpha, 0.02)
	deff(&c.QuarantineAlpha, 0.02)
	deff(&c.MinConfidence, 0.6)
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
	aa       map[types.ID]types.Verdict
}

// Comparison is one replayed pair, for reports and for labelling.
type Comparison struct {
	Episode    types.ID      `json:"episode"`
	Regression bool          `json:"regression_set"`
	Verdict    types.Verdict `json:"verdict"`
	// Judged is false when the two plans were identical and no judge was
	// asked. Only judged comparisons are worth a person's label.
	Judged bool `json:"judged"`
	// Noise is the episode's A/A verdict (baseline against baseline).
	Noise   types.Verdict      `json:"noise"`
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
	// Skipped lists episodes left out because a replay or judge call failed
	// for them, with why.
	Skipped []string `json:"skipped,omitempty"`
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
			// One episode's failed replay or judge call costs that episode,
			// not the test: a planner that varies run to run occasionally
			// returns a plan Helix rejects (found on the first automated
			// day, where one such replay ended a whole run). A host that
			// cannot replay the lesson at all, or a cancelled run, still
			// ends it.
			c, err := v.compare(ctx, eid, lesson)
			var aa types.Verdict
			if err == nil {
				aa, err = v.noise(ctx, eid)
			}
			if err != nil {
				if errors.Is(err, ports.ErrCannotReplay) || ctx.Err() != nil {
					return Report{}, err
				}
				rep.Skipped = append(rep.Skipped, err.Error())
				ev.Skipped++
				continue
			}
			c.Regression, c.Noise = set.regression, aa
			ev.NoiseCompared++
			if decisive(aa, cfg) {
				ev.NoiseDecisive++
			}
			ev.Episodes = append(ev.Episodes, eid)
			rep.Comparisons = append(rep.Comparisons, c)
			win := decisive(c.Verdict, cfg) && c.Verdict.Prefer == types.PreferWith
			loss := decisive(c.Verdict, cfg) && c.Verdict.Prefer == types.PreferWithout
			switch {
			case set.regression && win:
				ev.RegressionWins++
			case set.regression && loss:
				ev.Regressions++
			case set.regression:
			case win:
				ev.Compared++
				ev.Wins++
			case loss:
				ev.Compared++
				ev.Losses++
			default:
				ev.Compared++
				ev.Ties++
			}
		}
	}

	if len(ev.Episodes) == 0 && ev.Skipped > 0 {
		return Report{}, fmt.Errorf("every replay for lesson %s failed (%d); first: %s", id, ev.Skipped, rep.Skipped[0])
	}
	Calibrate(&ev)
	ev.Result, ev.Summary = decide(ev, lesson.State == types.StateQuarantined, cfg)
	if ev.Skipped > 0 {
		ev.Summary += fmt.Sprintf("; %d episode(s) skipped: a replay or judge call failed", ev.Skipped)
	}
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

func decisive(v types.Verdict, cfg Config) bool {
	return v.Prefer != types.PreferNeither && v.Confidence >= cfg.MinConfidence
}

// decide applies the rule (D-018). A planner that varies run to run makes
// two plans differ for reasons that have nothing to do with the lesson, and a
// judge confidently prefers one of them some of the time. The A/A replays
// measure how often: r, the share of baseline-against-baseline comparisons
// the judge called decisively. If the lesson did nothing, each comparison
// would come out a decisive win with probability r/2, a decisive loss with
// r/2, and a tie otherwise. Every question below is "how likely is this many
// wins (or losses) from noise alone?", and a lesson moves only when that is
// unlikely. Otherwise the result is inconclusive, never a guess: elimination
// is permanent.
//
// Every branch says why, with the counts, because the summary is what a
// person reads in `lessons why`. ev.PHelp and ev.PHarm must already be set
// (Test does, with Calibrate).
func decide(ev types.Evidence, quarantined bool, cfg Config) (types.TestResult, string) {
	r := noiseRate(ev)
	pHurt := tail(ev.Losses, ev.Compared, r/2)
	counts := fmt.Sprintf("relevant: %d wins, %d losses, %d ties (p=%.3f); past successes: %d worse, %d better (p=%.3f); noise: %d of %d A/A pairs decisive (r=%.2f)",
		ev.Wins, ev.Losses, ev.Ties, ev.PHelp, ev.Regressions, ev.RegressionWins, ev.PHarm, ev.NoiseDecisive, ev.NoiseCompared, r)
	switch {
	case ev.Regressions >= cfg.MinHarm && ev.PHarm <= cfg.HarmAlpha:
		return types.ResultFail, "harms past successes it is not about, beyond noise: " + counts
	case ev.Compared < cfg.MinCompared:
		return types.ResultInconclusive, fmt.Sprintf("too few relevant episodes to judge (need %d): %s", cfg.MinCompared, counts)
	case ev.Wins == 0 && ev.Losses == 0:
		return types.ResultFail, "changes nothing where it should help: " + counts
	case ev.Losses >= cfg.MinHarm && pHurt <= cfg.HarmAlpha:
		return types.ResultFail, "makes things worse where it applies, beyond noise: " + counts
	case quarantined && ev.Wins >= cfg.QuarantineMinWins && ev.PHelp <= cfg.QuarantineAlpha:
		return types.ResultPass, "clears the quarantine bar: " + counts
	case quarantined:
		return types.ResultFail, fmt.Sprintf("quarantined lessons need %d decisive wins and p ≤ %g: %s", cfg.QuarantineMinWins, cfg.QuarantineAlpha, counts)
	case ev.Wins >= cfg.MinWins && ev.PHelp <= cfg.Alpha:
		return types.ResultPass, "helps where it should, beyond noise, and harms nothing: " + counts
	}
	return types.ResultInconclusive, "not distinguishable from noise yet; the lesson stays where it is: " + counts
}

// Calibrate sets the evidence's p-values from its counts and noise floor.
func Calibrate(ev *types.Evidence) {
	r := noiseRate(*ev)
	ev.PHelp = tail(ev.Wins, ev.Compared, r/2)
	ev.PHarm = tail(ev.Regressions, ev.Regressions+ev.RegressionWins+tiesIn(*ev), r/2)
}

// tiesIn is how many regression episodes were neither better nor worse:
// every episode replayed, minus the relevant ones and the decisive regressions.
func tiesIn(ev types.Evidence) int {
	return max(len(ev.Episodes)-ev.Compared-ev.Regressions-ev.RegressionWins, 0)
}

// noiseRate is the share of A/A pairs the judge called decisively, smoothed
// so it is never 0 or 1: (decisive + 1) / (compared + 2). With no A/A pairs
// at all it is ½, the most cautious assumption.
func noiseRate(ev types.Evidence) float64 {
	return float64(ev.NoiseDecisive+1) / float64(ev.NoiseCompared+2)
}

// tail is the chance of k or more successes in n trials of probability p.
// With k ≤ 0 there is nothing to explain: 1.
func tail(k, n int, p float64) float64 {
	if k <= 0 || n <= 0 {
		return 1
	}
	sum := 0.0
	for i := k; i <= n; i++ {
		sum += binom(n, i) * math.Pow(p, float64(i)) * math.Pow(1-p, float64(n-i))
	}
	return math.Min(1, sum)
}

func binom(n, k int) float64 {
	r := 1.0
	for i := 1; i <= k; i++ {
		r = r * float64(n-k+i) / float64(i)
	}
	return r
}

// noise returns the A/A verdict for an episode: its baseline replayed a
// second time and judged against the first. Identical baselines (a
// deterministic host) are a tie without asking the judge. Cached: one A/A
// per episode per run, shared by every lesson tested.
func (v *Verifier) noise(ctx context.Context, eid types.ID) (types.Verdict, error) {
	if aa, ok := v.aa[eid]; ok {
		return aa, nil
	}
	if v.aa == nil {
		v.aa = map[types.ID]types.Verdict{}
	}
	ep := v.History.byID[eid]
	first := v.baseline[eid]
	second, err := v.Host.Replay(ctx, ep, nil)
	if err != nil {
		return types.Verdict{}, fmt.Errorf("replay %s again without lessons: %w", eid, err)
	}
	aa := types.Verdict{Prefer: types.PreferNeither, Confidence: 1, Reason: "identical baselines"}
	if !sameReplay(first, second) {
		if aa, err = v.Judge.Compare(ctx, ep, v.History.outcomes[eid], second, first); err != nil {
			return types.Verdict{}, fmt.Errorf("judge A/A %s: %w", eid, err)
		}
	}
	v.aa[eid] = aa
	return aa, nil
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

func inScope(ep types.Episode, s types.Scope) bool { return s.Covers(ep) }

func mix(a, b types.ID) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(a))
	_, _ = h.Write([]byte(b))
	return h.Sum64()
}

func sameReplay(a, b types.ReplayResult) bool {
	return a.Text == b.Text && slices.Equal(a.Steps, b.Steps)
}
