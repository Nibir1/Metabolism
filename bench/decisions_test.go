package bench

import (
	"testing"

	"github.com/Nibir1/metabolism/types"
)

// The measurement splits calls by kind and planner cause, and a candidate
// passes the share bar only when it touches enough of them.
func TestMeasureDecisions(t *testing.T) {
	eps := []types.Episode{
		{Usage: types.Usage{ModelCalls: 9}}, // before wire v3: left out
		{Usage: types.Usage{ModelCalls: 4, ByKind: map[string]int{"planner": 4}},
			Plans: []types.PlanRecord{
				{Round: 1, Prompt: "full", Calls: 2, Result: "planned", FirstTool: "file"},
				{Round: 2, Prompt: "full", Calls: 2, Result: "planned", FirstTool: "response", Answered: true},
			}},
		{Usage: types.Usage{ModelCalls: 3, ByKind: map[string]int{"planner": 1, "critic": 1, "chat": 1}},
			Plans:    []types.PlanRecord{{Round: 1, Prompt: "full", Calls: 1, Result: "quarantined"}},
			Critic:   &types.CriticRecord{Verdict: "no", Calls: 1},
			Fallback: "critic-quarantine"},
		{Usage: types.Usage{ModelCalls: 3, ByKind: map[string]int{"planner": 3}},
			Plans: []types.PlanRecord{{Round: 1, Prompt: "compact", Calls: 3, Result: "planned", FirstTool: "shell"}}},
	}
	d := MeasureDecisions(eps)
	if d.Episodes != 3 || d.Older != 1 || d.Calls != 10 || d.ByKind["planner"] != 8 || d.ByKind["critic"] != 1 {
		t.Fatalf("totals %+v", d)
	}
	if d.FirstAttempts != 3 || d.Retries != 4 || d.FollowUpCalls != 2 || d.SmallerPrompts != 1 || d.FailedPlannings != 1 {
		t.Fatalf("planner causes %+v", d)
	}
	if d.CriticRuns != 1 || d.CriticNo != 1 || d.Fallbacks["critic-quarantine"] != 1 || d.AnsweredRound != 1 {
		t.Fatalf("critic and fallback %+v", d)
	}
	byName := map[string]Candidate{}
	for _, c := range d.Candidates {
		byName[c.Name] = c
	}
	// Retries: 1 + 1 + 2 = 4 of 10 calls; answered follow-up rounds: 2.
	if c := byName["retry level"]; c.Calls != 4 || !c.Passes {
		t.Errorf("retry level %+v", c)
	}
	if c := byName["answer now or read more"]; c.Calls != 2 || !c.Passes {
		t.Errorf("answer now %+v", c)
	}
}
