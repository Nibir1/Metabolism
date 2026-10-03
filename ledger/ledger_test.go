package ledger

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

func TestPGreaterAgainstExactValues(t *testing.T) {
	cases := []struct {
		s1, f1, s2, f2 int
		want           float64
	}{
		{0, 0, 0, 0, 0.5},
		{7, 3, 7, 3, 0.5},
		{1, 0, 0, 1, 5.0 / 6}, // Beta(2,1) > Beta(1,2): ∫ 2x(2x−x²) dx
		{0, 1, 1, 0, 1.0 / 6},
	}
	for _, c := range cases {
		if got := pGreater(c.s1, c.f1, c.s2, c.f2); math.Abs(got-c.want) > 1e-4 {
			t.Errorf("pGreater(%d,%d,%d,%d) = %.5f, want %.5f", c.s1, c.f1, c.s2, c.f2, got, c.want)
		}
	}
	// Large, clearly different arms.
	if p := pGreater(90, 10, 60, 40); p < 0.999 {
		t.Errorf("90%% vs 60%% over 100 turns each: P(better) = %.4f", p)
	}
}

func TestBayesFactorFavoursWhatTheTurnsShow(t *testing.T) {
	same := Tally{Exposed: 200, ExposedSuccesses: 150, Withheld: 200, WithheldSuccesses: 150}
	if bf := same.BayesFactor(); bf >= 1 {
		t.Errorf("identical arms: BF = %.3f, want < 1", bf)
	}
	apart := Tally{Exposed: 200, ExposedSuccesses: 180, Withheld: 200, WithheldSuccesses: 120}
	if bf := apart.BayesFactor(); bf < 1000 {
		t.Errorf("90%% vs 60%%: BF = %.3g", bf)
	}
	if bf := (Tally{}).BayesFactor(); math.Abs(bf-1) > 1e-9 {
		t.Errorf("no turns: BF = %v, want 1", bf)
	}
}

func episode(i int, ended time.Time, end types.RunEnd, exposed, withheld []types.ID) types.Episode {
	return types.Episode{
		ID: types.NewID(ended), Host: "t", Scope: types.Scope{Level: types.ScopeGlobal},
		StartedAt: ended.Add(-time.Second), EndedAt: ended,
		Request:  types.Request{Text: "x", Provenance: types.ProvUserTyped},
		Exposure: exposed, Withheld: withheld, End: end, Usage: types.Usage{ModelCalls: 1 + i%2},
	}
}

// Turns before a lesson entered its current state do not count for it.
func TestTalliesStartAtTheLessonsEra(t *testing.T) {
	a := types.NewID(t0)
	old := t0.Add(-5 * time.Hour)
	eps := []types.Episode{
		episode(0, old, types.RunFailed, nil, []types.ID{a}),
		episode(1, t0.Add(-2*time.Hour), types.RunDone, []types.ID{a}, nil),
	}
	got := Tallies(eps, nil, t0, time.Hour, map[types.ID]time.Time{a: t0.Add(-3 * time.Hour)}, nil)[a]
	if got.Exposed != 1 || got.Withheld != 0 {
		t.Fatalf("tally %+v", got)
	}
}

func TestTalliesCountBothArmsAndHoldBackRecentTurns(t *testing.T) {
	a := types.NewID(t0)
	old := t0.Add(-2 * time.Hour)
	eps := []types.Episode{
		episode(0, old, types.RunDone, []types.ID{a}, nil),
		episode(1, old, types.RunFailed, []types.ID{a}, nil),
		episode(2, old, types.RunDone, nil, []types.ID{a}),
		episode(3, old, types.RunDone, nil, nil),                            // the lesson did not apply
		episode(4, t0.Add(-time.Minute), types.RunDone, []types.ID{a}, nil), // too recent
	}
	undone := eps[0]
	outs := map[types.ID][]types.Outcome{undone.ID: {{Kind: types.OutcomeUndone}}}
	got := *Tallies(eps, outs, t0, time.Hour, nil, nil)[a]
	want := Tally{LessonID: a, Exposed: 2, ExposedSuccesses: 0, Withheld: 1, WithheldSuccesses: 1,
		ExposedCalls: 3, WithheldCalls: 1, Immature: 1,
		RecentExposed: 2, RecentExposedSuccesses: 0, RecentWithheld: 1, RecentWithheldSuccesses: 1}
	if got != want {
		t.Fatalf("tally %+v\nwant  %+v", got, want)
	}
}

// Settle on real journal records: a lesson that helps is accepted, one that
// harms is eliminated, one with too few turns stays.
func TestSettleAcceptsEliminatesAndWaits(t *testing.T) {
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	src := episode(0, t0.Add(-48*time.Hour), types.RunFailed, nil, nil)
	if err := j.AppendEpisode(ctx, src); err != nil {
		t.Fatal(err)
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	// The lessons enter trial before the turns below: only turns since a
	// lesson's last state change count (Eras).
	m.SetClock(func() time.Time { return t0.Add(-30 * time.Hour) })
	trial := func(text string) types.Lesson {
		l, err := m.Create(ctx, types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
			Text: text, Sources: []types.ID{src.ID}, Provenance: types.ProvUserTyped}, "test")
		if err != nil {
			t.Fatal(err)
		}
		ev, _ := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
			Result: types.ResultPass, Summary: "replay", Compared: 3, Wins: 3})
		if err := m.Move(ctx, l.ID, types.StateTrial, "replay passed", ev.ID); err != nil {
			t.Fatal(err)
		}
		return l
	}
	good, bad, young := trial("helps"), trial("harms"), trial("new")

	// 40 delivered and 20 withheld turns each. good: 90% vs 50%. bad: 40% vs 90%.
	at := t0.Add(-24 * time.Hour)
	add := func(id types.ID, n, wins int, delivered bool) {
		for i := range n {
			at = at.Add(time.Minute)
			end := types.RunFailed
			if i < wins {
				end = types.RunDone
			}
			var exp, wh []types.ID
			if delivered {
				exp = []types.ID{id}
			} else {
				wh = []types.ID{id}
			}
			if err := j.AppendEpisode(ctx, episode(i, at, end, exp, wh)); err != nil {
				t.Fatal(err)
			}
		}
	}
	add(good.ID, 40, 36, true)
	add(good.ID, 20, 10, false)
	add(bad.ID, 40, 16, true)
	add(bad.ID, 20, 18, false)
	add(young.ID, 5, 5, true)

	tallies, err := Count(ctx, j, t0, time.Hour, Eras(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := Settle(ctx, m, tallies, Rule{}, false)
	if err != nil {
		t.Fatal(err)
	}
	moved := map[types.ID]types.LessonState{}
	for _, d := range ds {
		moved[d.Lesson.ID] = d.To
	}
	if moved[good.ID] != types.StateAccepted || moved[bad.ID] != types.StateEliminated || moved[young.ID] != "" {
		t.Fatalf("decisions: good→%q bad→%q young→%q", moved[good.ID], moved[bad.ID], moved[young.ID])
	}
	e, _ := m.Get(good.ID)
	if e.Lesson.State != types.StateAccepted {
		t.Fatalf("good is %s", e.Lesson.State)
	}
	last := e.Evidence[len(e.Evidence)-1]
	if last.Kind != types.EvidenceCredit || last.Exposed != 40 || last.WithheldSuccesses != 10 || last.PBetter < 0.95 {
		t.Fatalf("credit evidence: %+v", last)
	}

	// A dry run decides without writing.
	before := len(e.Evidence)
	if _, err := Settle(ctx, m, tallies, Rule{}, true); err != nil {
		t.Fatal(err)
	}
	if e, _ := m.Get(good.ID); len(e.Evidence) != before {
		t.Fatal("a dry run recorded evidence")
	}
}

// An accepted lesson with a long good record that starts doing harm is
// retired on its recent turns, though its whole record is still positive
// (D-026); the same lesson without the change stays.
func TestDriftRetiresAnAcceptedLessonThatStoppedHelping(t *testing.T) {
	lesson := types.Lesson{ID: types.NewID(t0), State: types.StateAccepted}
	var eps []types.Episode
	at := t0.Add(-200 * time.Hour)
	add := func(n int, deliveredWins, withheldWins float64) {
		for i := range n {
			at = at.Add(10 * time.Minute)
			end := types.RunFailed
			ep := episode(i, at, end, nil, nil)
			if i%10 == 0 { // the 10% holdout
				ep.Withheld = []types.ID{lesson.ID}
				if float64(i%100)/100 < withheldWins {
					ep.End = types.RunDone
				}
			} else {
				ep.Exposure = []types.ID{lesson.ID}
				if float64(i%100)/100 < deliveredWins {
					ep.End = types.RunDone
				}
			}
			eps = append(eps, ep)
		}
	}
	add(1000, 0.9, 0.5) // a long, good record: 90% vs 50%
	healthy := Tallies(eps, nil, t0, time.Hour, nil, nil)[lesson.ID]
	if d := decide(lesson, *healthy, Rule{}.Defaults()); d.To != "" {
		t.Fatalf("a healthy accepted lesson moved: %s", d.Reason)
	}
	add(200, 0.1, 0.6) // the world changed: now 10% vs 60%
	drifted := Tallies(eps, nil, t0, time.Hour, nil, nil)[lesson.ID]
	if p := drifted.PBetter(); p < 0.5 {
		t.Fatalf("setup: the whole record should still look positive, P(better) %.3f", p)
	}
	d := decide(lesson, *drifted, Rule{}.Defaults())
	if d.To != types.StateEliminated || !strings.Contains(d.Reason, "drift") || d.Tally.Exposed+d.Tally.Withheld != DriftWindow {
		t.Fatalf("drift: to %q, %s, window %d", d.To, d.Reason, d.Tally.Exposed+d.Tally.Withheld)
	}
}
