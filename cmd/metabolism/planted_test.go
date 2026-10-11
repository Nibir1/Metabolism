package main

import (
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/audit"
	"github.com/Nibir1/metabolism/immune"
	"github.com/Nibir1/metabolism/types"
)

// The planted suite is the size the scorecard expects, and every planted
// lesson is one the immune screen would let through: the nutrient test, not
// the screen, is what is being measured.
func TestPlantedSuite(t *testing.T) {
	if len(plantedWrong) != audit.PlantedSuite {
		t.Fatalf("%d planted lessons, the scorecard expects %d", len(plantedWrong), audit.PlantedSuite)
	}
	for _, text := range plantedWrong {
		l := types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal}, Text: text,
			Sources: []types.ID{"01a1140000d4e5f6a7b8c9d0e1f7"}, Provenance: types.ProvUserTyped}
		if err := immune.Screen(l, immune.DefaultPolicy()); err != nil {
			t.Errorf("the screen stops %q (%v): it would not measure the nutrient test", text, err)
		}
	}
}

// Kept out of trial counts as rejected, whether it failed or was not shown
// to help.
func TestPlantedTally(t *testing.T) {
	r := plantedResult{}
	for _, moved := range []types.LessonState{types.StateEliminated, types.StateCandidate, types.StateTrial} {
		r.Lessons = append(r.Lessons, struct {
			Text   string `json:"text"`
			Result string `json:"result"`
			Moved  string `json:"moved_to"`
			Score  string `json:"wins_losses_ties"`
		}{Moved: string(moved)})
	}
	if p := plantedTally([]plantedResult{r}); p.Planted != 3 || p.Rejected != 2 {
		t.Fatalf("%+v", p)
	}
}

// The retest shows labelled pairs swapped, stores answers in the original
// orientation, and reports agreement: a person who answers the same about
// the plans (not the letters) agrees with themselves.
func TestRetestLabels(t *testing.T) {
	dir := t.TempDir()
	from := dir + "/c.ndjson"
	c := comparisonRecord{Lesson: "01a1140000d4e5f6a7b8c9d0e1f7", Episode: "01a1140000d4e5f6a7b8c9d0e1f8",
		PlanA: []string{"good"}, PlanB: []string{"bad"}, WithIsA: true}
	var out strings.Builder
	// Earlier label "a" (the good plan). Shown swapped, the good plan is now
	// B, and answering "b" is consistent.
	err := retestLabels(&out, strings.NewReader("b\n"), from, []comparisonRecord{c},
		map[string]string{string(c.Lesson) + "/" + string(c.Episode): "a"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "agreed with yourself on 1 of 1") {
		t.Fatalf("output:\n%s", out.String())
	}
	if i, j := strings.Index(out.String(), "bad"), strings.Index(out.String(), "good"); i < 0 || j < 0 || i > j {
		t.Fatal("the plans were not shown swapped")
	}
}
