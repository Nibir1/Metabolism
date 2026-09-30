package types

import "time"

// Evidence is a recorded test result about a lesson. Lessons advance only on
// evidence (docs/architecture.md, principle 3).
type Evidence struct {
	ID       ID           `json:"id"`
	LessonID ID           `json:"lesson_id"`
	At       time.Time    `json:"at"`
	Kind     EvidenceKind `json:"kind"`
	Passed   bool         `json:"passed"`
	Summary  string       `json:"summary"`
}

// EvidenceKind names the test that produced the evidence.
type EvidenceKind string

const (
	// EvidenceNutrientTest: replay with vs without the lesson (Phase 2).
	EvidenceNutrientTest EvidenceKind = "nutrient-test"
	// EvidenceCredit: accumulated outcome credit (Phase 3).
	EvidenceCredit EvidenceKind = "credit"
	// EvidenceContradiction: conflict with another lesson (Phase 4).
	EvidenceContradiction EvidenceKind = "contradiction"
)

// Credit is the running estimate of a lesson's contribution to outcomes, as
// Beta-Bernoulli counts starting from a uniform prior.
type Credit struct {
	LessonID  ID      `json:"lesson_id"`
	Successes float64 `json:"successes"`
	Failures  float64 `json:"failures"`
}

// Mean is the posterior mean success rate under a Beta(1,1) prior.
func (c Credit) Mean() float64 {
	return (c.Successes + 1) / (c.Successes + c.Failures + 2)
}
