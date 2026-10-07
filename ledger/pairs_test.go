package ledger

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// pairWorld is a journal with past search turns and two trial lessons
// learned from them, a older than b. play adds n live shared turns cycling
// through the four combinations of the two coins; ok says whether a turn
// with that combination succeeds.
type pairWorld struct {
	ctx  context.Context
	j    *sqlite.Journal
	m    *metabolome.Metabolome
	a, b types.Lesson
	at   time.Time
}

func newPairWorld(t *testing.T) *pairWorld {
	t.Helper()
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	at := t0.Add(-300 * time.Hour)
	var sources []types.ID
	for i := range 20 {
		at = at.Add(time.Minute)
		ep := episode(i, at, types.RunFailed, nil, nil)
		ep.Request.Text = searchReq
		if len(sources) < 3 {
			sources = append(sources, ep.ID)
		}
		if err := j.AppendEpisode(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	w := &pairWorld{ctx: ctx, j: j, m: m, at: t0.Add(-200 * time.Hour)}
	trial := func(text string, at time.Time) types.Lesson {
		m.SetClock(func() time.Time { return at })
		l, err := m.Create(ctx, types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
			Text: text, Sources: sources, Provenance: types.ProvUserTyped}, "test")
		if err != nil {
			t.Fatal(err)
		}
		ev, _ := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
			Result: types.ResultPass, Summary: "replay", Compared: 3, Wins: 3})
		if err := m.Move(ctx, l.ID, types.StateTrial, "replay passed", ev.ID); err != nil {
			t.Fatal(err)
		}
		e, _ := m.Get(l.ID)
		return e.Lesson
	}
	w.a = trial("When a glob comes back empty, say so instead of searching again.", t0.Add(-250*time.Hour))
	w.b = trial("When a glob comes back empty, widen it once, then answer.", t0.Add(-249*time.Hour))
	m.SetClock(func() time.Time { return t0 })
	return w
}

func (w *pairWorld) play(t *testing.T, n int, ok func(a, b bool) bool) {
	t.Helper()
	for i := range n {
		w.at = w.at.Add(time.Minute)
		da, db := i%2 == 0, (i/2)%2 == 0
		end := types.RunFailed
		if ok(da, db) {
			end = types.RunDone
		}
		ep := episode(i, w.at, end, nil, nil)
		ep.Request.Text = searchReq
		for _, x := range []struct {
			id        types.ID
			delivered bool
		}{{w.a.ID, da}, {w.b.ID, db}} {
			if x.delivered {
				ep.Exposure = append(ep.Exposure, x.id)
			} else {
				ep.Withheld = append(ep.Withheld, x.id)
			}
		}
		if err := w.j.AppendEpisode(w.ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
}

func (w *pairWorld) settle(t *testing.T) map[types.ID]Decision {
	t.Helper()
	book, err := Credit(w.ctx, w.j, w.m, t0, Rule{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := Settle(w.ctx, w.m, book, Rule{}, false)
	if err != nil {
		t.Fatal(err)
	}
	out := map[types.ID]Decision{}
	for _, d := range ds {
		out[d.Lesson.ID] = d
	}
	return out
}

// Two lessons each repair the failure alone and fail together. Each one's
// own credit is neutral (it helps without the other and hurts with it), so
// the ledger alone would keep both and lose every turn they share; the
// pair rule sets the newer aside, and restores it when the older leaves.
func TestContradictingLessonsAreSetAsideAndRestored(t *testing.T) {
	w := newPairWorld(t)
	w.play(t, 400, func(a, b bool) bool { return a != b })
	ds := w.settle(t)
	if ds[w.a.ID].To != "" {
		t.Fatalf("the older lesson moved: %s", ds[w.a.ID].Reason)
	}
	d := ds[w.b.ID]
	if d.To != types.StateSuperseded || d.Other != w.a.ID || !strings.Contains(d.Reason, "contradict") {
		t.Fatalf("newer: to %q other %s: %s", d.To, d.Other, d.Reason)
	}
	e, _ := w.m.Get(w.b.ID)
	last := e.Evidence[len(e.Evidence)-1]
	if last.Kind != types.EvidenceContradiction || last.Other != w.a.ID || last.Result != types.ResultFail {
		t.Fatalf("evidence: %+v", last)
	}
	if by, ok := w.m.SupersededBy(w.b.ID); !ok || by != w.a.ID {
		t.Fatalf("superseded by %s, %v", by, ok)
	}

	// The older lesson is eliminated for some other reason: the newer one
	// was set aside for it, so it comes back through trial.
	ev, _ := w.m.RecordEvidence(w.ctx, types.Evidence{LessonID: w.a.ID, Kind: types.EvidenceCredit,
		Result: types.ResultFail, Summary: "test"})
	if err := w.m.Move(w.ctx, w.a.ID, types.StateEliminated, "test", ev.ID); err != nil {
		t.Fatal(err)
	}
	ds = w.settle(t)
	if d := ds[w.b.ID]; d.To != types.StateTrial || !strings.Contains(d.Reason, "restored") {
		t.Fatalf("restore: to %q: %s", d.To, d.Reason)
	}
}

// Two lessons that do the same thing: each helps alone, neither adds to the
// other. The newer is set aside as redundant.
func TestRedundantLessonIsSetAside(t *testing.T) {
	w := newPairWorld(t)
	w.play(t, 800, func(a, b bool) bool { return a || b })
	d := w.settle(t)[w.b.ID]
	if d.To != types.StateSuperseded || d.Other != w.a.ID || !strings.Contains(d.Reason, "does what the other does") {
		t.Fatalf("to %q other %s: %s", d.To, d.Other, d.Reason)
	}
}

// A lesson that changes nothing beside one that helps is neutral, not
// redundant: it helps nowhere, so the pair rule leaves it alone, as the
// ledger leaves every neutral lesson. A lesson that hurts with or without
// the other is harmful, not in a contradiction: the ledger's own rule
// eliminates it. One whose harm shows only beside the other is set aside.
func TestNeutralAndHarmfulLessonsAreNotSuperseded(t *testing.T) {
	w := newPairWorld(t)
	w.play(t, 800, func(a, b bool) bool { return a })
	if d := w.settle(t)[w.b.ID]; d.To == types.StateSuperseded {
		t.Fatalf("a neutral lesson was superseded: %s", d.Reason)
	}

	// Harmful whether the other is delivered or not: eliminated.
	w = newPairWorld(t)
	w.play(t, 800, func(a, b bool) bool { return !b })
	if d := w.settle(t)[w.b.ID]; d.To != types.StateEliminated {
		t.Fatalf("a harmful lesson: to %q: %s", d.To, d.Reason)
	}

	// Harmful beside the other, with its effect alone unmeasurable (every
	// turn without the other fails anyway): set aside, not eliminated. It
	// comes back to trial only if the other leaves, where its own credit
	// judges it alone.
	w = newPairWorld(t)
	w.play(t, 800, func(a, b bool) bool { return a && !b })
	if d := w.settle(t)[w.b.ID]; d.To != types.StateSuperseded {
		t.Fatalf("harm seen only beside the other: to %q: %s", d.To, d.Reason)
	}
}

// Pair evidence is kept per period of constant delivery probability and the
// periods' Bayes factors multiply.
func TestStratifiedMultipliesPeriods(t *testing.T) {
	var s Stratified
	for i := range 40 {
		s.add("trial", i%2 == 0, i%2)
		s.add("accepted", i%10 != 0, 1)
	}
	if got, want := s.BayesFactor(), s.Strata[0].BayesFactor()*s.Strata[1].BayesFactor(); got != want {
		t.Fatalf("BF %v, want %v", got, want)
	}
	if p := s.Pooled(); p.Exposed != 56 || p.Withheld != 24 {
		t.Fatalf("pooled %+v", p)
	}
	ps := []Period{{Start: t0, State: types.StateCandidate}, {Start: t0.Add(time.Hour), State: types.StateTrial},
		{Start: t0.Add(2 * time.Hour), State: types.StateSuperseded}}
	if _, ok := periodAt(ps, t0.Add(30*time.Minute)); ok {
		t.Fatal("a candidate is not in force")
	}
	if k, ok := periodAt(ps, t0.Add(90*time.Minute)); !ok || k == "" {
		t.Fatal("in trial at 1h30")
	}
	if _, ok := periodAt(ps, t0.Add(3*time.Hour)); ok {
		t.Fatal("a superseded lesson is not in force")
	}
}

// Two lessons that fail only together each look harmful in their own credit
// (every turn succeeds unless both are delivered). The ledger must not
// eliminate them for it: the pair rule sets the newer aside, reversibly,
// and the older stays.
func TestContradictionIsNotEliminatedAsHarm(t *testing.T) {
	w := newPairWorld(t)
	w.play(t, 600, func(a, b bool) bool { return !a || !b })
	ds := w.settle(t)
	if d := ds[w.a.ID]; d.To == types.StateEliminated {
		t.Fatalf("the older lesson was eliminated: %s", d.Reason)
	}
	if d := ds[w.b.ID]; d.To != types.StateSuperseded || d.Other != w.a.ID {
		t.Fatalf("newer: to %q other %s: %s", d.To, d.Other, d.Reason)
	}
}
