package audit

import "fmt"

// RubricItem is one of vision.md's nine criteria, scored from the
// scorecard: 1 only when every criterion its evidence needs is met.
type RubricItem struct {
	C       string   `json:"c"`
	Name    string   `json:"name"`
	Score   int      `json:"score"` // 0 or 1; vision.md allows no half points here
	Because string   `json:"because"`
	Needs   []string `json:"needs,omitempty"` // scorecard criteria not met yet
}

// Rubric scores C1–C9. Three rest on the code and its tests rather than on
// collected evidence, as the roadmap records: C2 (lessons reflected with no
// human, each with sources: any journal with such lessons shows it), C3 (the
// journal and the versioned store survive restarts: tested) and C6 (no
// lesson reaches trial without a with/without result: enforced by
// metabolome.Move, and tested).
func Rubric(r Report) []RubricItem {
	met := map[string]bool{}
	for _, c := range r.Criteria {
		met[c.ID] = c.Status == Met
	}
	item := func(c, name, because string, ids ...string) RubricItem {
		it := RubricItem{C: c, Name: name, Because: because, Score: 1}
		for _, id := range ids {
			if !met[id] {
				it.Score = 0
				it.Needs = append(it.Needs, id)
			}
		}
		return it
	}
	c2 := item("C2", "Break down", fmt.Sprintf("%d lessons reflected with no human, each citing its source episodes", r.Evidence.LessonsLearned))
	if r.Evidence.LessonsLearned == 0 {
		c2.Score, c2.Because = 0, "no reflected lessons yet"
	}
	return []RubricItem{
		item("C1", "Ingest", "outcome coverage on real episodes", "1.outcome-labels"),
		c2,
		item("C3", "Assimilate", "append-only journal and versioned metabolome; restart and rollback tested (D-028)"),
		item("C4", "Distribute", "held-out transfer", "6.transfer"),
		item("C5", "Eliminate", "planted wrong lessons kept out on real replays; drift on real data (the poisoning suite passes on the simulator)",
			"2.planted-real", "4.drift-real"),
		item("C6", "Verify", "no lesson reaches trial without a with/without result (metabolome.Move, tested)"),
		item("C7", "Consolidate", "adapter retention on the old-task suite", "7.retention"),
		item("C8", "Endure", "three months of real use", "8.endurance"),
		item("C9", "Compound", "real curves against the real baseline", "0.real-baseline", "5.cost-curve"),
	}
}
