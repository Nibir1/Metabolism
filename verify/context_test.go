package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/types"
)

// accept moves a lesson to accepted on planted evidence, as replays and
// live credit would.
func (w world) accept(t *testing.T, id types.ID) {
	t.Helper()
	ctx := context.Background()
	ev, err := w.m.RecordEvidence(ctx, types.Evidence{LessonID: id, Kind: types.EvidenceNutrientTest,
		Result: types.ResultPass, Summary: "planted", Compared: 3, Wins: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.m.Move(ctx, id, types.StateTrial, "planted", ev.ID); err != nil {
		t.Fatal(err)
	}
	cr, err := w.m.RecordEvidence(ctx, types.Evidence{LessonID: id, Kind: types.EvidenceCredit,
		Result: types.ResultPass, Summary: "planted", Exposed: 40, ExposedSuccesses: 36, Withheld: 20, WithheldSuccesses: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.m.Move(ctx, id, types.StateAccepted, "planted", cr.ID); err != nil {
		t.Fatal(err)
	}
}

func pair(t *testing.T, tag string) helixsim.PairSpec {
	t.Helper()
	for _, p := range helixsim.PairLessons() {
		if strings.Contains(p.Text, "("+tag+")") {
			return p
		}
	}
	t.Fatalf("no pair lesson tagged %s", tag)
	return helixsim.PairSpec{}
}

// A candidate is judged on what it adds to the accepted lessons in force
// (D-027). With the empty-search lesson accepted, its paraphrase changes
// nothing and its rival, good alone, makes turns fail; both would have
// passed alone. A lesson about something else passes as before.
func TestCandidatesAreJudgedOnWhatTheyAddToTheLessonsInForce(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, scenario, text string
		inContext, alone     types.LessonState
	}{
		{"paraphrase", "empty-glob-loop", pair(t, "empty-glob").Text, types.StateEliminated, types.StateTrial},
		{"rival", "empty-glob-loop", pair(t, "widen-once").Text, types.StateEliminated, types.StateTrial},
		{"unrelated", "missing-package-json", spec(t, "(go-module)", true), types.StateTrial, types.StateTrial},
	} {
		for _, withoutContext := range []bool{false, true} {
			w := newWorld(t)
			incumbent := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvUserTyped)
			w.accept(t, incumbent)
			id := w.lessonFrom(t, c.scenario, c.text, types.ProvUserTyped)
			v := w.verifier()
			v.Config.WithoutContext = withoutContext
			rep, err := v.Test(ctx, w.m, id)
			if err != nil {
				t.Fatal(err)
			}
			want := c.inContext
			if withoutContext {
				want = c.alone
			}
			if rep.To != want {
				t.Errorf("%s (without context %v): → %s, want %s: %s", c.name, withoutContext, rep.To, want, rep.Evidence.Summary)
			}
			if !withoutContext && c.scenario == "empty-glob-loop" && !strings.Contains(rep.Evidence.Summary, "in force") {
				t.Errorf("%s: the summary does not say what was in force: %s", c.name, rep.Evidence.Summary)
			}
		}
	}
}
