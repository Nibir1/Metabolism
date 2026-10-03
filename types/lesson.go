package types

import (
	"errors"
	"fmt"
	"time"
)

// LessonKind is what sort of change a lesson proposes.
type LessonKind string

const (
	// KindFact: something true about the environment ("this repo uses make").
	KindFact LessonKind = "fact"
	// KindPattern: a recurring failure and how to avoid it.
	KindPattern LessonKind = "pattern"
	// KindPreference: how the user likes things done.
	KindPreference LessonKind = "preference"
	// KindProcedure: a multi-step workflow (Phase 6).
	KindProcedure LessonKind = "procedure"
	// KindGate: a compiled fast check (Phase 5). Gates may only restrict.
	KindGate LessonKind = "gate"
)

// Valid reports whether k is a known lesson kind.
func (k LessonKind) Valid() bool {
	switch k {
	case KindFact, KindPattern, KindPreference, KindProcedure, KindGate:
		return true
	}
	return false
}

// LessonState is where a lesson is in its lifecycle. See the diagram in
// docs/architecture.md.
type LessonState string

const (
	StateCandidate   LessonState = "candidate"
	StateQuarantined LessonState = "quarantined"
	StateTrial       LessonState = "trial"
	StateAccepted    LessonState = "accepted"
	StateSuperseded  LessonState = "superseded"
	StateEliminated  LessonState = "eliminated"
)

// transitions is the lifecycle. A lesson moves forward only on evidence, and
// every state can be eliminated. A trial or accepted lesson is superseded
// when another lesson covers the same turns and it conflicts with that
// lesson or adds nothing to it (D-027). Superseded is reversible, because
// "set aside" is not a verdict on the lesson alone: it comes back through
// trial, on a coin flip, and earns its place again. Eliminated is the record
// of a decision.
var transitions = map[LessonState][]LessonState{
	StateCandidate:   {StateQuarantined, StateTrial, StateEliminated},
	StateQuarantined: {StateTrial, StateEliminated},
	StateTrial:       {StateAccepted, StateSuperseded, StateEliminated},
	StateAccepted:    {StateSuperseded, StateEliminated},
	StateSuperseded:  {StateTrial, StateEliminated},
	StateEliminated:  nil,
}

// Valid reports whether s is a known state.
func (s LessonState) Valid() bool {
	_, ok := transitions[s]
	return ok
}

// CanTransition reports whether the lifecycle allows moving from s to to.
func (s LessonState) CanTransition(to LessonState) bool {
	for _, t := range transitions[s] {
		if t == to {
			return true
		}
	}
	return false
}

// Injectable reports whether a lesson in this state may reach a host's
// context. Only trial lessons (on a share of episodes) and accepted ones do.
func (s LessonState) Injectable() bool {
	return s == StateTrial || s == StateAccepted
}

// Lesson is a candidate change to future behaviour.
type Lesson struct {
	ID    ID          `json:"id"`
	Kind  LessonKind  `json:"kind"`
	Scope Scope       `json:"scope"`
	Text  string      `json:"text"`
	State LessonState `json:"state"`

	// Sources are the episodes the lesson was learned from.
	Sources []ID `json:"sources"`
	// Provenance is the least trusted provenance among the sources.
	Provenance Provenance `json:"provenance"`

	// Rationale is why the reflector proposed it, in its own words.
	Rationale string `json:"rationale,omitempty"`
	// Origin names what proposed it ("reflector:ollama/qwen3", "sim").
	Origin string `json:"origin,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate reports every problem with l at once.
func (l Lesson) Validate() error {
	var errs []error
	if !l.ID.Valid() {
		errs = append(errs, fmt.Errorf("id %q is not a valid ID", l.ID))
	}
	if !l.Kind.Valid() {
		errs = append(errs, fmt.Errorf("kind %q is unknown", l.Kind))
	}
	if err := l.Scope.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("scope: %w", err))
	}
	if l.Text == "" {
		errs = append(errs, errors.New("text is empty"))
	}
	if !l.State.Valid() {
		errs = append(errs, fmt.Errorf("state %q is unknown", l.State))
	}
	if len(l.Sources) == 0 {
		errs = append(errs, errors.New("a lesson must name the episodes it came from"))
	}
	if !l.Provenance.Valid() {
		errs = append(errs, fmt.Errorf("provenance %q is unknown", l.Provenance))
	}
	return errors.Join(errs...)
}

// LifecycleEvent records one state change of a lesson. The journal keeps every
// event, so "why did it forget that?" always has an answer.
type LifecycleEvent struct {
	ID       ID          `json:"id"`
	LessonID ID          `json:"lesson_id"`
	At       time.Time   `json:"at"`
	From     LessonState `json:"from"`
	To       LessonState `json:"to"`
	Reason   string      `json:"reason"`
	// Evidence names the evidence records that justified the change.
	Evidence []ID `json:"evidence,omitempty"`
}

// Validate reports every problem with ev at once, including a transition the
// lifecycle does not allow.
func (ev LifecycleEvent) Validate() error {
	var errs []error
	if !ev.ID.Valid() {
		errs = append(errs, fmt.Errorf("id %q is not a valid ID", ev.ID))
	}
	if !ev.LessonID.Valid() {
		errs = append(errs, fmt.Errorf("lesson_id %q is not a valid ID", ev.LessonID))
	}
	if ev.At.IsZero() {
		errs = append(errs, errors.New("at is zero"))
	}
	// From is empty only for the event that creates the lesson.
	if ev.From == "" {
		if ev.To != StateCandidate {
			errs = append(errs, fmt.Errorf("a new lesson starts as %s, not %s", StateCandidate, ev.To))
		}
	} else if !ev.From.CanTransition(ev.To) {
		errs = append(errs, fmt.Errorf("transition %s → %s is not allowed", ev.From, ev.To))
	}
	if ev.Reason == "" {
		errs = append(errs, errors.New("a state change needs a reason"))
	}
	return errors.Join(errs...)
}
