package types

import (
	"errors"
	"fmt"
	"slices"
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

	// Withheld lists the lessons that applied to this episode but were left
	// out by the host's coin flip (trial randomisation, D-020). Together with
	// Exposure it is a randomised experiment per lesson: the withheld turns
	// are the matched no-lesson baseline the credit ledger compares against.
	// A lesson left out for another reason (the budget) is in neither list.
	Withheld []ID `json:"withheld,omitempty"`

	Steps []Step `json:"steps,omitempty"`
	Usage Usage  `json:"usage"`
	End   RunEnd `json:"end"`
	Attrs Attrs  `json:"attrs,omitempty"`

	// What the turn decided (recording wire v3, D-031): one record per
	// planner round, the firewall critic's verdict when it ran, and why a
	// chat fallback answered instead of a plan. Empty in older recordings.
	Plans    []PlanRecord  `json:"plans,omitempty"`
	Critic   *CriticRecord `json:"critic,omitempty"`
	Fallback string        `json:"fallback,omitempty"`

	// Thinking is the turn's arm of the host's planning-mode coin flip:
	// "on" or "off" (wire v4, D-031). Empty when no coin was flipped.
	Thinking string `json:"thinking,omitempty"`

	// Consumed is the provenance of content the turn brought in from outside
	// the user's own work (wire v5, D-038): retrieved-web, file-content. A
	// lesson learned from it takes the least trusted of these and the
	// request's provenance.
	Consumed []Provenance `json:"consumed,omitempty"`
}

// The arms of the planning-mode coin flip (wire v4).
const (
	ThinkingOn  = "on"
	ThinkingOff = "off"
)

// Tokens is what the provider itself reported model calls cost (wire v4).
// Calls is how many calls came with a report; calls without one are not in
// these counts. Reasoning is part of Completion; CacheHit is part of Prompt.
type Tokens struct {
	Prompt     int `json:"prompt"`
	CacheHit   int `json:"cache_hit,omitempty"`
	Completion int `json:"completion"`
	Reasoning  int `json:"reasoning,omitempty"`
	Calls      int `json:"calls"`
}

// Add sums two token counts.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{Prompt: t.Prompt + o.Prompt, CacheHit: t.CacheHit + o.CacheHit,
		Completion: t.Completion + o.Completion, Reasoning: t.Reasoning + o.Reasoning, Calls: t.Calls + o.Calls}
}

func (t Tokens) valid() bool {
	return t.Prompt >= 0 && t.CacheHit >= 0 && t.Completion >= 0 && t.Reasoning >= 0 && t.Calls >= 0 &&
		t.CacheHit <= t.Prompt && t.Reasoning <= t.Completion
}

// PlanRecord is one planner round of a recorded turn (wire v3, D-031).
type PlanRecord struct {
	Round int `json:"round"`
	// Prompt is the planner prompt that produced it: full, compact or
	// minimal.
	Prompt string `json:"prompt"`
	// Calls is how many model calls the planning cost, retries included.
	Calls int `json:"calls"`
	// Result: planned, error, parse-error, canary or quarantined.
	Result string `json:"result"`
	// The plan's own routing labels, and whether it only answered.
	Intent    string `json:"intent,omitempty"`
	FirstTool string `json:"first_tool,omitempty"`
	Answered  bool   `json:"answered,omitempty"`
	Steps     int    `json:"steps,omitempty"`
	// What the planning sent and received, in characters, retries
	// included: a habit that changes the prompt saves these, not calls.
	InputChars  int `json:"input_chars,omitempty"`
	OutputChars int `json:"output_chars,omitempty"`
	// Tokens is what the provider reported the planning cost (wire v4).
	Tokens *Tokens `json:"tokens,omitempty"`
}

// CriticRecord is the host's safety critic's review of a plan (wire v3).
type CriticRecord struct {
	Verdict string `json:"verdict"`
	Calls   int    `json:"calls"`
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

	// The planner round that planned the step (1-based): in a replay
	// (D-021), and in a recorded episode from wire v3 (D-031); 0 in older
	// recordings. Ran is replay only: whether the replay ran it. A replay runs
	// only read-only file steps, between rounds; every other step is planned
	// and never run, and its OK means only "not known to fail".
	Round int  `json:"round,omitempty"`
	Ran   bool `json:"ran,omitempty"`
}

// Usage is what the episode cost. Characters always, because not every
// provider reports tokens; bench converts with a stated estimate. Tokens too
// where the provider reported them.
type Usage struct {
	ModelCalls  int `json:"model_calls"`
	InputChars  int `json:"input_chars"`
	OutputChars int `json:"output_chars"`
	// ByKind splits ModelCalls by what each call was for, as the host names
	// it (Helix: planner, critic, chat, tool, vision). Wire v3, D-031.
	ByKind map[string]int `json:"by_kind,omitempty"`
	// Tokens is the provider's reported token count for the episode, over
	// the calls that reported one (wire v4, D-031).
	Tokens *Tokens `json:"tokens,omitempty"`
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
	for i, id := range ep.Withheld {
		if !id.Valid() {
			errs = append(errs, fmt.Errorf("withheld[%d] %q is not a valid ID", i, id))
		}
		if slices.Contains(ep.Exposure, id) {
			errs = append(errs, fmt.Errorf("lesson %s is both exposed and withheld", id))
		}
	}
	if ep.Usage.ModelCalls < 0 || ep.Usage.InputChars < 0 || ep.Usage.OutputChars < 0 {
		errs = append(errs, errors.New("usage has a negative count"))
	}
	if t := ep.Usage.Tokens; t != nil && !t.valid() {
		errs = append(errs, errors.New("usage tokens are negative or inconsistent"))
	}
	for i, p := range ep.Plans {
		if p.Tokens != nil && !p.Tokens.valid() {
			errs = append(errs, fmt.Errorf("plan %d tokens are negative or inconsistent", i))
		}
	}
	for _, p := range ep.Consumed {
		if !p.Valid() {
			errs = append(errs, fmt.Errorf("consumed provenance %q is unknown", p))
		}
	}
	if ep.Thinking != "" && ep.Thinking != ThinkingOn && ep.Thinking != ThinkingOff {
		errs = append(errs, fmt.Errorf("thinking %q is neither on nor off", ep.Thinking))
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
