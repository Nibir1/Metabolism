package types

import (
	"errors"
	"fmt"
	"time"
)

// OutcomeKind is what an outcome says about its episode.
type OutcomeKind string

const (
	// OutcomeSuccess: the episode achieved what was asked.
	OutcomeSuccess OutcomeKind = "success"
	// OutcomeFailure: it did not.
	OutcomeFailure OutcomeKind = "failure"
	// OutcomePartial: some of it.
	OutcomePartial OutcomeKind = "partial"
	// OutcomeUndone: the user reversed what the episode did. The strongest
	// negative signal a host can give, and it usually arrives late.
	OutcomeUndone OutcomeKind = "undone"
	// OutcomeDeclined: the user refused a step the episode proposed.
	OutcomeDeclined OutcomeKind = "declined"
	// OutcomeRepeated: the user asked for the same thing again soon after,
	// which says the first attempt did not land whatever the run end claimed.
	OutcomeRepeated OutcomeKind = "repeated"
)

// Valid reports whether k is a known outcome kind.
func (k OutcomeKind) Valid() bool {
	switch k {
	case OutcomeSuccess, OutcomeFailure, OutcomePartial,
		OutcomeUndone, OutcomeDeclined, OutcomeRepeated:
		return true
	}
	return false
}

// Negative reports whether k counts against the episode.
func (k OutcomeKind) Negative() bool {
	switch k {
	case OutcomeFailure, OutcomeUndone, OutcomeDeclined, OutcomeRepeated:
		return true
	}
	return false
}

// OutcomeSource is who produced the label.
type OutcomeSource string

const (
	// SourceHost: the host labelled the episode when it ended.
	SourceHost OutcomeSource = "host"
	// SourceUser: the user said so explicitly.
	SourceUser OutcomeSource = "user"
	// SourceDerived: inferred later from behaviour (an undo, a repeat).
	SourceDerived OutcomeSource = "derived"
)

// Valid reports whether s is a known source.
func (s OutcomeSource) Valid() bool {
	switch s {
	case SourceHost, SourceUser, SourceDerived:
		return true
	}
	return false
}

// Succeeded reports whether an episode counts as a good outcome: it ended
// done and collected no negative outcome. This is the one definition of
// success. Benchmarks divide cost by it, and episodic memory labels cases
// with it.
func Succeeded(ep Episode, outs []Outcome) bool {
	if ep.End != RunDone {
		return false
	}
	for _, o := range outs {
		if o.Kind.Negative() {
			return false
		}
	}
	return true
}

// Outcome labels an episode. An episode may collect several: the host's
// immediate verdict, then an undo an hour later. Nothing overwrites; readers
// decide how to combine them.
type Outcome struct {
	ID        ID            `json:"id"`
	EpisodeID ID            `json:"episode_id"`
	At        time.Time     `json:"at"`
	Kind      OutcomeKind   `json:"kind"`
	Source    OutcomeSource `json:"source"`
	Note      string        `json:"note,omitempty"`
}

// Validate reports every problem with o at once.
func (o Outcome) Validate() error {
	var errs []error
	if !o.ID.Valid() {
		errs = append(errs, fmt.Errorf("id %q is not a valid ID", o.ID))
	}
	if !o.EpisodeID.Valid() {
		errs = append(errs, fmt.Errorf("episode_id %q is not a valid ID", o.EpisodeID))
	}
	if o.At.IsZero() {
		errs = append(errs, errors.New("at is zero"))
	}
	if !o.Kind.Valid() {
		errs = append(errs, fmt.Errorf("kind %q is unknown", o.Kind))
	}
	if !o.Source.Valid() {
		errs = append(errs, fmt.Errorf("source %q is unknown", o.Source))
	}
	return errors.Join(errs...)
}
