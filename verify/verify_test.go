package verify

import (
	"context"
	"path/filepath"
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
	for ep, n := range host.without {
		if n > 1 {
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
