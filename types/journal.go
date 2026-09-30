package types

import "time"

// RecordKind names what a journal record holds.
type RecordKind string

const (
	RecordEpisode RecordKind = "episode"
	RecordOutcome RecordKind = "outcome"
	RecordEvent   RecordKind = "event"
)

// Record is one journal entry. Exactly one of Episode, Outcome and Event is
// set, matching Kind.
type Record struct {
	// Seq is the journal's own position for the record: strictly increasing,
	// assigned at append time. Stages resume from the last Seq they processed.
	Seq  int64      `json:"seq"`
	Kind RecordKind `json:"kind"`
	At   time.Time  `json:"at"`

	Episode *Episode        `json:"episode,omitempty"`
	Outcome *Outcome        `json:"outcome,omitempty"`
	Event   *LifecycleEvent `json:"event,omitempty"`
}

// JournalQuery selects journal records. Zero fields don't filter.
type JournalQuery struct {
	Kinds     []RecordKind
	AfterSeq  int64
	Since     time.Time // inclusive
	Until     time.Time // exclusive
	EpisodeID ID        // outcomes for one episode, or the episode itself
	Limit     int
}
