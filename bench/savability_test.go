package bench

import (
	"testing"
	"time"

	"github.com/Nibir1/metabolism/types"
)

// lookup is a two-round turn: a read, then a round that answers or reads on.
func lookup(i int, readOK, answers bool, chars int) types.Episode {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
	second := types.PlanRecord{Round: 2, Calls: 1, Result: "planned", Answered: answers, InputChars: chars}
	return types.Episode{StartedAt: at,
		Steps: []types.Step{{Tool: "file", Action: "read", OK: readOK, Round: 1}},
		Plans: []types.PlanRecord{{Round: 1, Calls: 1, Result: "planned", FirstTool: "file", InputChars: 17000}, second}}
}

// After a read that worked the planner answers, after one that failed it
// reads on: a table that learns this saves most of the answer-only input
// (the full prompt, 17k of 24k characters per round) and cuts nothing short.
func TestSavabilityLearnsWhenTheNextRoundAnswers(t *testing.T) {
	var eps []types.Episode
	for i := range 40 {
		ok := i%2 == 0
		eps = append(eps, lookup(i, ok, ok, 24000))
	}
	s := MeasureSavability(eps, 0.5)
	if !s.Passes || s.FalseAlarms != 0 || s.Recall != 1 || s.FullPrompt != 17000 {
		t.Fatalf("%+v", s)
	}
	if s.SavedShare < 0.7 || s.SavedShare > 0.71 {
		t.Fatalf("saved share %.3f, want 17/24", s.SavedShare)
	}
}

// When the situation before the round does not tell, nothing fires and the
// test says so.
func TestSavabilityFailsWhenNothingPredicts(t *testing.T) {
	var eps []types.Episode
	for i := range 40 {
		eps = append(eps, lookup(i, true, i%2 == 0, 24000))
	}
	if s := MeasureSavability(eps, 0.5); s.Passes || s.Fired != 0 {
		t.Fatalf("%+v", s)
	}
}
