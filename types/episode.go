package types

import (
	"errors"
	"fmt"
	"time"
)

// Episode is one unit of experience: a request, what the host did about it,
// and how the run ended. Episodes are immutable once journalled. Anything
// learned about an episode later (the user undid it, or asked again) arrives
// as a separate Outcome that points back to it.
//
// The host must redact an episode before handing it over. The engine stores
// what it is given, and a secret that reaches the journal has already leaked.
type Episode struct {
	ID        ID        `json:"id"`
	Host      string    `json:"host"`
	Scope     Scope     `json:"scope"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`

	Request Request `json:"request"`

	// Exposure lists the lessons that were in context for this episode. It is
	// what lets the credit ledger blame or credit a lesson for the outcome.
	// Empty is meaningful: it is the no-lesson baseline.
	Exposure []ID `json:"exposure,omitempty"`

	Steps []Step `json:"steps,omitempty"`
	Usage Usage  `json:"usage"`
	End   RunEnd `json:"end"`
	Attrs Attrs  `json:"attrs,omitempty"`
}

// Attrs carries host-specific detail the engine does not interpret.
type Attrs map[string]string

// Request is what started the episode.
type Request struct {
	Text       string     `json:"text"`
	Provenance Provenance `json:"provenance"`
}

// Step is one action the host took. Tool and Action are the host's own
// vocabulary ("file", "glob"); Subject is what it acted on ("**/*.py").
type Step struct {
	Tool     string        `json:"tool"`
	Action   string        `json:"action,omitempty"`
	Subject  string        `json:"subject,omitempty"`
	OK       bool          `json:"ok"`
	Err      string        `json:"err,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
}

// Usage is what the episode cost. Characters rather than tokens, because not
// every provider reports tokens; bench converts with a stated estimate.
type Usage struct {
	ModelCalls  int `json:"model_calls"`
	InputChars  int `json:"input_chars"`
	OutputChars int `json:"output_chars"`
}

// RunEnd is how the host says the run finished, at the moment it finished.
// It is the host's immediate verdict; later Outcomes may contradict it.
type RunEnd string

const (
	// RunDone: nothing failed and nothing was left open.
	RunDone RunEnd = "done"
	// RunFailed: stopped on an unresolved error.
	RunFailed RunEnd = "failed"
	// RunBudgetExhausted: ran out of steps with work still open.
	RunBudgetExhausted RunEnd = "budget-exhausted"
	// RunOpenWork: stopped with work open, for a reason other than budget.
	RunOpenWork RunEnd = "open-work"
)

// Valid reports whether e is a known run end.
func (e RunEnd) Valid() bool {
	switch e {
	case RunDone, RunFailed, RunBudgetExhausted, RunOpenWork:
		return true
	}
	return false
}

// Validate reports every problem with ep at once, so a host integration can
// fix them in one pass rather than one error per round trip.
func (ep Episode) Validate() error {
	var errs []error
	if !ep.ID.Valid() {
		errs = append(errs, fmt.Errorf("id %q is not a valid ID", ep.ID))
	}
	if ep.Host == "" {
		errs = append(errs, errors.New("host is empty"))
	}
	if err := ep.Scope.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("scope: %w", err))
	}
	if ep.StartedAt.IsZero() {
		errs = append(errs, errors.New("started_at is zero"))
	}
	if ep.EndedAt.Before(ep.StartedAt) {
		errs = append(errs, errors.New("ended_at is before started_at"))
	}
	if !ep.Request.Provenance.Valid() {
		errs = append(errs, fmt.Errorf("request provenance %q is unknown", ep.Request.Provenance))
	}
	if !ep.End.Valid() {
		errs = append(errs, fmt.Errorf("run end %q is unknown", ep.End))
	}
	for i, id := range ep.Exposure {
		if !id.Valid() {
			errs = append(errs, fmt.Errorf("exposure[%d] %q is not a valid ID", i, id))
		}
	}
	if ep.Usage.ModelCalls < 0 || ep.Usage.InputChars < 0 || ep.Usage.OutputChars < 0 {
		errs = append(errs, errors.New("usage has a negative count"))
	}
	return errors.Join(errs...)
}

// FailedSteps counts the steps that did not succeed.
func (ep Episode) FailedSteps() int {
	n := 0
	for _, s := range ep.Steps {
		if !s.OK {
			n++
		}
	}
	return n
}
