package types

import (
	"errors"
	"fmt"
	"time"
)

// Evidence is a recorded test result about a lesson. Lessons advance only on
// evidence (docs/architecture.md, principle 3), and the journal keeps every
// piece of it, so "why is this lesson trusted?" always has an answer.
type Evidence struct {
	ID       ID           `json:"id"`
	LessonID ID           `json:"lesson_id"`
	At       time.Time    `json:"at"`
	Kind     EvidenceKind `json:"kind"`
	Result   TestResult   `json:"result"`
	Summary  string       `json:"summary"`

	// Judge names what decided the comparisons ("heuristic", "jev", ...).
	Judge string `json:"judge,omitempty"`

	// Counts from a nutrient test. Relevant episodes are those the lesson is
	// about; regression episodes are past successes it should leave alone.
	Compared    int `json:"compared,omitempty"`
	Wins        int `json:"wins,omitempty"`
	Losses      int `json:"losses,omitempty"`
	Ties        int `json:"ties,omitempty"`
	Regressions int `json:"regressions,omitempty"`
	// RegressionWins: past successes where the with-lesson plan was judged
	// better. Counted so harm is judged symmetrically: with a planner that
	// varies run to run, noise produces both (D-018).
	RegressionWins int `json:"regression_wins,omitempty"`

	// The A/A noise floor: episodes whose baseline was replayed twice and
	// judged against itself, and how many of those the judge called
	// decisively. A judge that confidently prefers one of two no-lesson plans
	// is reading noise; this is how often it does.
	NoiseCompared int `json:"noise_compared,omitempty"`
	NoiseDecisive int `json:"noise_decisive,omitempty"`

	// Sign-test p-values: that the lesson helps on its relevant episodes,
	// and that it harms past successes, beyond what symmetric noise explains.
	PHelp float64 `json:"p_help,omitempty"`
	PHarm float64 `json:"p_harm,omitempty"`

	// Episodes lists every episode replayed, relevant and regression alike.
	Episodes []ID `json:"episodes,omitempty"`
	// Skipped counts episodes left out because a replay or judge call failed.
	Skipped int `json:"skipped,omitempty"`
	// Identical counts relevant episodes where the plans with and without
	// the lesson were the same: the lesson changed nothing there at all.
	Identical int `json:"identical,omitempty"`

	// Credit counts (D-020), from live turns the lesson applied to: those
	// where it was delivered, those where the host's coin flip withheld it,
	// and how many of each succeeded. PBetter is the posterior probability
	// that the lesson's success rate is higher when delivered; BayesFactor
	// weighs "delivery changes the rate" against "it does not".
	Exposed           int     `json:"exposed,omitempty"`
	ExposedSuccesses  int     `json:"exposed_successes,omitempty"`
	Withheld          int     `json:"withheld,omitempty"`
	WithheldSuccesses int     `json:"withheld_successes,omitempty"`
	PBetter           float64 `json:"p_better,omitempty"`
	BayesFactor       float64 `json:"bayes_factor,omitempty"`

	// Context lists the lessons in force (accepted) that both sides of a
	// nutrient test's replays carried: the lesson is judged on what it adds
	// to them (D-027).
	Context []ID `json:"context,omitempty"`
	// Other is the lesson a contradiction or redundancy is with. For those
	// kinds the credit counts above are this lesson's turns with Other
	// delivered: this one delivered against withheld.
	Other ID `json:"other,omitempty"`
}

// Passed reports whether the evidence supports the lesson.
func (e Evidence) Passed() bool { return e.Result == ResultPass }

// Validate reports every problem with e at once.
func (e Evidence) Validate() error {
	var errs []error
	if !e.ID.Valid() {
		errs = append(errs, fmt.Errorf("id %q is not a valid ID", e.ID))
	}
	if !e.LessonID.Valid() {
		errs = append(errs, fmt.Errorf("lesson_id %q is not a valid ID", e.LessonID))
	}
	if e.At.IsZero() {
		errs = append(errs, errors.New("at is zero"))
	}
	if !e.Kind.Valid() {
		errs = append(errs, fmt.Errorf("kind %q is unknown", e.Kind))
	}
	if !e.Result.Valid() {
		errs = append(errs, fmt.Errorf("result %q is unknown", e.Result))
	}
	if e.Summary == "" {
		errs = append(errs, errors.New("summary is empty"))
	}
	if e.Compared < 0 || e.Wins < 0 || e.Losses < 0 || e.Ties < 0 || e.Regressions < 0 ||
		e.RegressionWins < 0 || e.NoiseCompared < 0 || e.NoiseDecisive < 0 || e.Skipped < 0 || e.Identical < 0 {
		errs = append(errs, errors.New("negative count"))
	}
	if e.Exposed < 0 || e.ExposedSuccesses < 0 || e.Withheld < 0 || e.WithheldSuccesses < 0 {
		errs = append(errs, errors.New("negative credit count"))
	}
	if e.ExposedSuccesses > e.Exposed || e.WithheldSuccesses > e.Withheld {
		errs = append(errs, errors.New("more successes than turns"))
	}
	if e.BayesFactor < 0 {
		errs = append(errs, errors.New("negative bayes_factor"))
	}
	if e.PBetter < 0 || e.PBetter > 1 {
		errs = append(errs, errors.New("p_better outside [0, 1]"))
	}
	if e.NoiseDecisive > e.NoiseCompared {
		errs = append(errs, errors.New("noise_decisive exceeds noise_compared"))
	}
	if e.PHelp < 0 || e.PHelp > 1 || e.PHarm < 0 || e.PHarm > 1 {
		errs = append(errs, errors.New("p-value outside [0, 1]"))
	}
	if (e.Kind == EvidenceContradiction || e.Kind == EvidenceRedundancy) && (!e.Other.Valid() || e.Other == e.LessonID) {
		errs = append(errs, fmt.Errorf("%s evidence must name the other lesson", e.Kind))
	}
	if e.Wins+e.Losses+e.Ties > e.Compared {
		errs = append(errs, errors.New("wins + losses + ties exceeds compared"))
	}
	return errors.Join(errs...)
}

// EvidenceKind names the test that produced the evidence.
type EvidenceKind string

const (
	// EvidenceNutrientTest: replay with vs without the lesson (Phase 2).
	EvidenceNutrientTest EvidenceKind = "nutrient-test"
	// EvidenceCredit: outcome credit from live turns, delivered against
	// withheld (Phase 3, D-020).
	EvidenceCredit EvidenceKind = "credit"
	// EvidenceContradiction: on the turns two lessons share, delivering
	// this one hurts when the other is delivered too (D-027). Other names
	// the other lesson.
	EvidenceContradiction EvidenceKind = "contradiction"
	// EvidenceRedundancy: on the turns two lessons share, neither adds
	// anything to the other (D-027). Other names the lesson kept.
	EvidenceRedundancy EvidenceKind = "redundancy"
)

// Valid reports whether k is a known evidence kind.
func (k EvidenceKind) Valid() bool {
	switch k {
	case EvidenceNutrientTest, EvidenceCredit, EvidenceContradiction, EvidenceRedundancy:
		return true
	}
	return false
}

// TestResult is what a test concluded.
type TestResult string

const (
	// ResultPass: the evidence supports the lesson.
	ResultPass TestResult = "pass"
	// ResultFail: the evidence is against it.
	ResultFail TestResult = "fail"
	// ResultInconclusive: not enough evidence either way. The lesson stays
	// where it is.
	ResultInconclusive TestResult = "inconclusive"
)

// Valid reports whether r is a known result.
func (r TestResult) Valid() bool {
	switch r {
	case ResultPass, ResultFail, ResultInconclusive:
		return true
	}
	return false
}

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

// Experience is an episode with what is known about how it turned out. It is
// what a Reflector reads.
type Experience struct {
	Episode   Episode   `json:"episode"`
	Outcomes  []Outcome `json:"outcomes"`
	Succeeded bool      `json:"succeeded"`
}
