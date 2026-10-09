package verify

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

type world struct {
	m   *metabolome.Metabolome
	h   *History
	eps []types.Episode
}

func newWorld(t *testing.T) world {
	t.Helper()
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	eps, outs := helixsim.Generate(helixsim.Config{Seed: 11, Count: 400, Start: t0, Span: 28 * 24 * time.Hour})
	for _, e := range eps {
		if err := j.AppendEpisode(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range outs {
		if err := j.AppendOutcome(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	h, err := LoadHistory(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	return world{m: m, h: h, eps: eps}
}

// lessonFrom creates a lesson learned from up to three typed episodes of a
// scenario.
func (w world) lessonFrom(t *testing.T, scenario, text string, prov types.Provenance) types.ID {
	t.Helper()
	var src []types.ID
	for _, ep := range w.eps {
		if ep.Attrs["scenario"] == scenario && ep.Request.Provenance == types.ProvUserTyped && len(src) < 3 {
			src = append(src, ep.ID)
		}
	}
	if len(src) == 0 {
		t.Fatalf("no %s episodes", scenario)
	}
	l, err := w.m.Create(context.Background(), types.Lesson{
		Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
		Text: text, Sources: src, Provenance: prov,
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	return l.ID
}

func (w world) verifier() *Verifier {
	return &Verifier{Host: helixsim.Host{}, Judge: judge.Heuristic{}, History: w.h}
}

func spec(t *testing.T, tag string, good bool) string {
	t.Helper()
	list := helixsim.WrongLessons()
	if good {
		list = helixsim.CorrectLessons()
	}
	for _, s := range list {
		if strings.Contains(s.Text, tag) {
			return s.Text
		}
	}
	t.Fatalf("no lesson containing %q", tag)
	return ""
}

func TestCorrectLessonPassesWithEvidence(t *testing.T) {
	w := newWorld(t)
	id := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvUserTyped)
	rep, err := w.verifier().Test(context.Background(), w.m, id)
	if err != nil {
		t.Fatal(err)
	}
	ev := rep.Evidence
	if rep.To != types.StateTrial || !ev.Passed() || ev.Wins < 2 || ev.Regressions != 0 || len(ev.Episodes) == 0 {
		t.Fatalf("correct lesson: %s → %s, %s", rep.From, rep.To, ev.Summary)
	}
	e, _ := w.m.Get(id)
	last := e.Events[len(e.Events)-1]
	if e.Lesson.State != types.StateTrial || len(last.Evidence) != 1 || last.Evidence[0] != ev.ID {
		t.Fatalf("the move to trial does not cite its evidence: %+v", last)
	}
}

func TestWrongLessonsAreEliminated(t *testing.T) {
	cases := []struct {
		scenario, tag, why string
	}{
		{"empty-glob-loop", "YAML", "changes nothing"},
		{"edit-and-test", "(always-rerun)", "harms"},
		{"missing-package-json", "(node-first)", ""},
		{"sed-noop", "(prefer-sed)", ""},
	}
	for _, c := range cases {
		t.Run(c.tag, func(t *testing.T) {
			w := newWorld(t)
			id := w.lessonFrom(t, c.scenario, spec(t, c.tag, false), types.ProvUserTyped)
			rep, err := w.verifier().Test(context.Background(), w.m, id)
			if err != nil {
				t.Fatal(err)
			}
			if rep.To != types.StateEliminated || rep.Evidence.Result != types.ResultFail {
				t.Fatalf("wrong lesson %q survived: %s", c.tag, rep.Evidence.Summary)
			}
			if c.why != "" && !strings.Contains(rep.Evidence.Summary, c.why) {
				t.Fatalf("summary %q does not say %q", rep.Evidence.Summary, c.why)
			}
		})
	}
}

func TestQuarantinedLessonFacesTheStricterBar(t *testing.T) {
	w := newWorld(t)
	id := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvRetrievedWeb)
	if e, _ := w.m.Get(id); e.Lesson.State != types.StateQuarantined {
		t.Fatalf("setup: lesson is %s", e.Lesson.State)
	}
	rep, err := w.verifier().Test(context.Background(), w.m, id)
	if err != nil {
		t.Fatal(err)
	}
	if rep.To != types.StateTrial || rep.Evidence.Wins < 4 || rep.Evidence.Losses != 0 {
		t.Fatalf("a correct lesson with ample evidence should clear quarantine: %s", rep.Evidence.Summary)
	}

	// With the bar out of reach, the same lesson fails quarantine.
	w2 := newWorld(t)
	id2 := w2.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvRetrievedWeb)
	v := w2.verifier()
	v.Config.QuarantineMinWins = 99
	rep, err = v.Test(context.Background(), w2.m, id2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.To != types.StateEliminated || !strings.Contains(rep.Evidence.Summary, "quarantined") {
		t.Fatalf("quarantine bar not applied: %s", rep.Evidence.Summary)
	}
}

func TestInconclusiveLeavesTheLessonWhereItIs(t *testing.T) {
	w := newWorld(t)
	id := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvUserTyped)
	v := w.verifier()
	v.Config.MinCompared = 1000
	rep, err := v.Test(context.Background(), w.m, id)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := w.m.Get(id)
	if rep.Evidence.Result != types.ResultInconclusive || e.Lesson.State != types.StateCandidate || len(e.Evidence) != 1 {
		t.Fatalf("inconclusive: result %s, state %s, %d evidence", rep.Evidence.Result, e.Lesson.State, len(e.Evidence))
	}
}

func TestOnlyCandidatesAreTested(t *testing.T) {
	w := newWorld(t)
	id := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvUserTyped)
	v := w.verifier()
	if _, err := v.Test(context.Background(), w.m, id); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Test(context.Background(), w.m, id); err == nil {
		t.Fatal("a lesson already in trial was tested again")
	}
}

type countingHost struct {
	helixsim.Host
	without map[types.ID]int
}

func (c *countingHost) Replay(ctx context.Context, ep types.Episode, ls []types.Lesson) (types.ReplayResult, error) {
	if len(ls) == 0 {
		c.without[ep.ID]++
	}
	return c.Host.Replay(ctx, ep, ls)
}

func TestBaselineReplaysAreCached(t *testing.T) {
	w := newWorld(t)
	a := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvUserTyped)
	b := w.lessonFrom(t, "empty-glob-loop", spec(t, "YAML", false), types.ProvUserTyped)
	host := &countingHost{without: map[types.ID]int{}}
	v := &Verifier{Host: host, Judge: judge.Heuristic{}, History: w.h}
	for _, id := range []types.ID{a, b} {
		if _, err := v.Test(context.Background(), w.m, id); err != nil {
			t.Fatal(err)
		}
	}
	// Two baseline replays per episode (the baseline and its A/A twin), and
	// no more however many lessons are tested against it.
	for ep, n := range host.without {
		if n > 2 {
			t.Fatalf("episode %s replayed without lessons %d times", ep, n)
		}
	}
}

type countingJudge struct {
	judge.Heuristic
	calls int
}

func (c *countingJudge) Compare(ctx context.Context, ep types.Episode, outs []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	c.calls++
	return c.Heuristic.Compare(ctx, ep, outs, with, without)
}

// A judge is only asked about pairs that differ. An irrelevant lesson changes
// no plan, so it costs no judge calls and cannot fail on judge noise.
func TestIdenticalReplaysAreTiesWithoutAJudge(t *testing.T) {
	w := newWorld(t)
	id := w.lessonFrom(t, "empty-glob-loop", spec(t, "YAML", false), types.ProvUserTyped)
	j := &countingJudge{}
	rep, err := (&Verifier{Host: helixsim.Host{}, Judge: j, History: w.h}).Test(context.Background(), w.m, id)
	if err != nil {
		t.Fatal(err)
	}
	if j.calls != 0 {
		t.Fatalf("the judge was asked %d times about identical plans", j.calls)
	}
	if rep.Evidence.Wins != 0 || rep.Evidence.Losses != 0 || rep.Evidence.Regressions != 0 || rep.Evidence.Ties == 0 {
		t.Fatalf("evidence %+v", rep.Evidence)
	}
}

// noisyHost wraps the simulator the way a real planner behaves: a replay
// sometimes differs from the last for reasons that have nothing to do with
// any lesson (a different but equivalent step).
type noisyHost struct {
	helixsim.Host
	rate  float64
	calls uint64
}

func (n *noisyHost) Replay(ctx context.Context, ep types.Episode, ls []types.Lesson) (types.ReplayResult, error) {
	res, err := n.Host.Replay(ctx, ep, ls)
	if err != nil {
		return res, err
	}
	n.calls++
	if float64(mix(ep.ID, types.ID(fmt.Sprint(n.calls)))%1000)/1000 < n.rate {
		res.Steps = append(slices.Clone(res.Steps), types.Step{Tool: "file", Action: "list", Subject: fmt.Sprintf("variant-%d", n.calls%3), OK: true})
	}
	return res, nil
}

// noisyJudge is the heuristic judge, except that when two plans differ only
// by noise it still picks a side, confidently, at random: what the first live
// run showed a model judge doing.
type noisyJudge struct{ judge.Heuristic }

func (noisyJudge) Name() string { return "noisy" }
func (j noisyJudge) Compare(ctx context.Context, ep types.Episode, outs []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	v, err := j.Heuristic.Compare(ctx, ep, outs, with, without)
	if err != nil || v.Prefer != types.PreferNeither {
		return v, err
	}
	if mix(ep.ID, types.ID(fmt.Sprint(len(with.Steps), len(without.Steps), with.Steps[len(with.Steps)-1].Subject)))%2 == 0 {
		return types.Verdict{Prefer: types.PreferWith, Confidence: 0.8, Reason: "noise"}, nil
	}
	return types.Verdict{Prefer: types.PreferWithout, Confidence: 0.8, Reason: "noise"}, nil
}

// TestDecisionsHoldOnANoisyHost is D-018's acceptance test. On a host whose
// replays vary and a judge that reads that variation as preference, the
// nutrient test, over 20 independent lessons per case, must:
//   - accept a correct lesson nearly always;
//   - eliminate a harmful lesson always;
//   - almost never eliminate an irrelevant lesson on noise, which the D-015
//     rule (any decisive loss on a past success fails) did routinely.
func TestDecisionsHoldOnANoisyHost(t *testing.T) {
	const trials = 20
	cases := []struct {
		scenario, tag string
		good          bool
	}{
		{"empty-glob-loop", "(empty-glob)", true},
		{"edit-and-test", "(always-rerun)", false},
		{"empty-glob-loop", "YAML", false},
	}
	for _, c := range cases {
		t.Run(c.tag, func(t *testing.T) {
			w := newWorld(t)
			counts := map[types.TestResult]int{}
			noiseRegressions := 0
			for i := range trials {
				id := w.lessonFrom(t, c.scenario, spec(t, c.tag, c.good), types.ProvUserTyped)
				v := &Verifier{Host: &noisyHost{rate: 0.6, calls: uint64(i) * 1000}, Judge: noisyJudge{}, History: w.h}
				rep, err := v.Test(context.Background(), w.m, id)
				if err != nil {
					t.Fatal(err)
				}
				if rep.Evidence.NoiseDecisive == 0 {
					t.Fatalf("the A/A probe saw no noise on a noisy host: %+v", rep.Evidence)
				}
				counts[rep.Evidence.Result]++
				noiseRegressions += rep.Evidence.Regressions
			}
			t.Logf("%s over %d trials: %v", c.tag, trials, counts)
			switch c.tag {
			case "(empty-glob)":
				if counts[types.ResultPass] < trials*9/10 || counts[types.ResultFail] > 1 {
					t.Fatalf("a correct lesson was accepted %d/%d and eliminated %d/%d", counts[types.ResultPass], trials, counts[types.ResultFail], trials)
				}
			case "(always-rerun)":
				if counts[types.ResultFail] != trials {
					t.Fatalf("a harmful lesson survived %d/%d times", trials-counts[types.ResultFail], trials)
				}
			case "YAML":
				if noiseRegressions == 0 {
					t.Fatal("the noisy host produced no noise regressions; the test proves nothing")
				}
				if counts[types.ResultFail] > 1 || counts[types.ResultPass] > 2 {
					t.Fatalf("an irrelevant lesson was eliminated %d and accepted %d of %d times on noise",
						counts[types.ResultFail], counts[types.ResultPass], trials)
				}
			}
		})
	}
}

func TestTailAndNoiseRate(t *testing.T) {
	if tail(0, 5, 0.3) != 1 || tail(3, 0, 0.3) != 1 {
		t.Fatal("no evidence must be p=1")
	}
	if got := tail(3, 3, 0.5); math.Abs(got-0.125) > 1e-9 {
		t.Fatalf("tail(3,3,.5) = %v", got)
	}
	quiet := types.Evidence{NoiseCompared: 14, NoiseDecisive: 0}
	noisy := types.Evidence{NoiseCompared: 14, NoiseDecisive: 10}
	if noiseRate(quiet) >= noiseRate(noisy) || noiseRate(types.Evidence{}) != 0.5 {
		t.Fatal("noise rate ordering or default")
	}
	// Three clean wins are strong evidence on a quiet host and weak on a noisy one.
	if tail(3, 8, noiseRate(quiet)/2) > 0.1 || tail(3, 8, noiseRate(noisy)/2) < 0.1 {
		t.Fatal("the noise floor does not change the strength of the same evidence")
	}
}

// failingHost fails every replay of the episodes in bad, or of every episode.
type failingHost struct {
	helixsim.Host
	bad map[types.ID]bool
	all bool
}

func (f failingHost) Replay(ctx context.Context, ep types.Episode, ls []types.Lesson) (types.ReplayResult, error) {
	if f.all || f.bad[ep.ID] {
		return types.ReplayResult{}, errors.New("no valid steps after validation")
	}
	return f.Host.Replay(ctx, ep, ls)
}

// One episode's failed replay costs that episode, not the test; the evidence
// says how many were skipped. When every replay fails, the test fails loudly.
func TestFailedReplaysSkipTheEpisodeNotTheTest(t *testing.T) {
	w := newWorld(t)
	id := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvUserTyped)
	e, _ := w.m.Get(id)
	host := failingHost{bad: map[types.ID]bool{e.Lesson.Sources[0]: true}}
	v := &Verifier{Host: host, Judge: judge.Heuristic{}, History: w.h}
	rep, err := v.Test(context.Background(), w.m, id)
	if err != nil {
		t.Fatalf("one failed replay ended the test: %v", err)
	}
	if rep.Evidence.Skipped != 1 || len(rep.Skipped) != 1 || !strings.Contains(rep.Evidence.Summary, "1 episode(s) skipped") {
		t.Fatalf("skipped %d %v, summary %q", rep.Evidence.Skipped, rep.Skipped, rep.Evidence.Summary)
	}
	if rep.To != types.StateTrial {
		t.Fatalf("the other episodes still decide: %s, %s", rep.To, rep.Evidence.Summary)
	}

	other := w.lessonFrom(t, "empty-glob-loop", spec(t, "YAML", false), types.ProvUserTyped)
	all := &Verifier{Host: failingHost{all: true}, Judge: judge.Heuristic{}, History: w.h}
	if _, err := all.Test(context.Background(), w.m, other); err == nil || !strings.Contains(err.Error(), "every replay") {
		t.Fatalf("every replay failing: %v", err)
	}
}

// changingHost adds a harmless step whenever a lesson is in context: the plan
// changes, the outcome does not.
type changingHost struct{ helixsim.Host }

func (c changingHost) Replay(ctx context.Context, ep types.Episode, ls []types.Lesson) (types.ReplayResult, error) {
	r, err := c.Host.Replay(ctx, ep, nil)
	if len(ls) > 0 {
		r.Steps = append(slices.Clone(r.Steps), types.Step{Tool: "file", Action: "list", Subject: "list .", OK: true})
	}
	return r, err
}

type neitherJudge struct{}

func (neitherJudge) Name() string { return "neither" }
func (neitherJudge) Compare(context.Context, types.Episode, []types.Outcome, types.ReplayResult, types.ReplayResult) (types.Verdict, error) {
	return types.Verdict{Prefer: types.PreferNeither, Confidence: 1, Reason: "outcome: both answered; equal outcomes are a tie"}, nil
}

// Permanent elimination for "changes nothing" needs identical plans. A lesson
// that changed the plans but no outcome stays a candidate (D-024).
func TestChangedPlansWithEqualOutcomesAreNotNothing(t *testing.T) {
	w := newWorld(t)
	id := w.lessonFrom(t, "answer", spec(t, "summary line", false), types.ProvUserTyped)
	v := &Verifier{Host: changingHost{}, Judge: neitherJudge{}, History: w.h}
	rep, err := v.Test(context.Background(), w.m, id)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Evidence.Result != types.ResultInconclusive || rep.To != types.StateCandidate || rep.Evidence.Identical != 0 {
		t.Fatalf("changed plans, equal outcomes: %s → %s, %s", rep.From, rep.To, rep.Evidence.Summary)
	}
	// The same lesson on a host where it changes nothing at all is eliminated.
	other := w.lessonFrom(t, "answer", spec(t, "YAML", false), types.ProvUserTyped)
	rep, err = w.verifier().Test(context.Background(), w.m, other)
	if err != nil {
		t.Fatal(err)
	}
	if rep.To != types.StateEliminated || rep.Evidence.Identical != rep.Evidence.Compared {
		t.Fatalf("identical plans: %s, identical %d of %d", rep.Evidence.Summary, rep.Evidence.Identical, rep.Evidence.Compared)
	}
}

// D-034: wins must also beat the lesson's own losses. With a judge decisive
// on most pairs (15 of 24 baseline pairs), 8 wins against 6 losses cleared
// the noise on wins alone; it is not evidence the lesson helps.
func TestDecideWinsMustBeatLosses(t *testing.T) {
	cfg := Config{}.withDefaults()
	ev := func(w, l, ties, noiseDecisive, noiseCompared int) types.Evidence {
		e := types.Evidence{Wins: w, Losses: l, Ties: ties, Compared: w + l + ties,
			NoiseDecisive: noiseDecisive, NoiseCompared: noiseCompared}
		Calibrate(&e)
		return e
	}
	cases := []struct {
		name string
		e    types.Evidence
		want types.TestResult
	}{
		{"8 against 6, noisy judge (the case that passed)", ev(8, 6, 0, 15, 24), types.ResultInconclusive},
		{"4 against 0, quiet judge", ev(4, 0, 12, 4, 26), types.ResultPass},
		{"8 against 2, noisy judge", ev(8, 2, 4, 15, 24), types.ResultPass},
		{"6 losses against 7 wins is not worse", ev(7, 6, 3, 15, 24), types.ResultInconclusive},
	}
	for _, c := range cases {
		if got, why := decide(c.e, false, cfg); got != c.want {
			t.Errorf("%s: %s (%s)", c.name, got, why)
		}
	}
}

// With no losses there is nothing to weigh the wins against: a quiet
// judge's 2 clean wins pass as under D-018.
func TestDecideCleanWinsStillPass(t *testing.T) {
	cfg := Config{}.withDefaults()
	e := types.Evidence{Wins: 2, Compared: 14, Ties: 12, NoiseDecisive: 0, NoiseCompared: 26}
	Calibrate(&e)
	if got, why := decide(e, false, cfg); got != types.ResultPass {
		t.Fatalf("2 clean wins with a quiet judge: %s (%s)", got, why)
	}
}
