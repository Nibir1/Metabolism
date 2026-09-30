// Package ports declares the interfaces the engine depends on. Hosts and
// adapters implement them; the engine imports nothing else. That dependency
// direction is what keeps the core domain-agnostic (docs/decisions.md, D-006).
//
// Phase 0 implements Journal (adapters/journal/sqlite) and a simulated Host
// and Judge (hosts/helixsim). The rest are declared now so later phases fill
// them in without reshaping the core.
package ports

import (
	"context"
	"errors"
	"iter"

	"github.com/Nibir1/metabolism/types"
)

// Errors a Journal returns, so callers can tell a caller mistake from a fault.
var (
	// ErrInvalid: the record failed validation. Nothing was written.
	ErrInvalid = errors.New("invalid record")
	// ErrConflict: a record with the same ID but different content exists.
	// Appending the identical record twice is not an error; hosts retry.
	ErrConflict = errors.New("conflicting record with the same id")
	// ErrUnknownEpisode: an outcome names an episode the journal never saw.
	ErrUnknownEpisode = errors.New("outcome for an unknown episode")
)

// Journal is the durable, append-only record of everything the engine
// ingests or decides. A record is on disk before Append returns, so a crash
// after Append loses nothing.
type Journal interface {
	AppendEpisode(ctx context.Context, ep types.Episode) error
	AppendOutcome(ctx context.Context, o types.Outcome) error
	AppendEvent(ctx context.Context, ev types.LifecycleEvent) error
	// Scan yields matching records in Seq order. Stopping the iteration early
	// releases its resources.
	Scan(ctx context.Context, q types.JournalQuery) iter.Seq2[types.Record, error]
	Close() error
}

// Host is what a system implements to become digestive.
type Host interface {
	Describe() types.HostProfile
	// Replay re-runs a past episode with the given lessons in context and
	// with NO side effects. The nutrient test depends on this being safe to
	// call any number of times.
	Replay(ctx context.Context, ep types.Episode, lessons []types.Lesson) (types.ReplayResult, error)
}

// Judge compares a replay with lessons against one without, knowing how the
// original episode actually turned out.
type Judge interface {
	Compare(ctx context.Context, original types.Episode, outcomes []types.Outcome,
		with, without types.ReplayResult) (types.Verdict, error)
}

// Reflector turns episodes into candidate lessons (Phase 2).
type Reflector interface {
	Reflect(ctx context.Context, eps []types.Episode, existing []types.Lesson) ([]types.Lesson, error)
}

// LessonQuery selects lessons from a Store. Zero fields don't filter.
type LessonQuery struct {
	States []types.LessonState
	Scopes []types.Scope
	Limit  int
}

// Store is the queryable, versioned metabolome (Phases 2–4).
type Store interface {
	PutLesson(ctx context.Context, l types.Lesson) error
	Lessons(ctx context.Context, q LessonQuery) ([]types.Lesson, error)
	RecordEvidence(ctx context.Context, e types.Evidence) error
	Credit(ctx context.Context, lessonID types.ID) (types.Credit, error)
	Snapshot(ctx context.Context, label string) (string, error)
	Restore(ctx context.Context, snapshot string) error
}

// Selector chooses which lessons to deliver for a request within the host's
// budget (Phase 3).
type Selector interface {
	Select(ctx context.Context, req types.Request, scope types.Scope, budget types.Budget) (types.Selection, error)
}
