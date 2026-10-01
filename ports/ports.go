// Package ports declares the interfaces the engine depends on. Hosts and
// adapters implement them; the engine imports nothing else. That dependency
// direction is what keeps the core domain-agnostic (docs/decisions.md, D-006).
//
// Journal is implemented by adapters/journal/sqlite; Host, Judge and Reflector
// by hosts/helixsim (simulated) and, from Phase 2, real adapters. Store and
// Selector are declared for later phases.
package ports

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"

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
	// ErrUnknownLesson: an event or evidence names a lesson the journal never saw.
	ErrUnknownLesson = errors.New("record for an unknown lesson")
	// ErrCannotReplay: a host cannot replay with this lesson in any way that
	// would mean something (a simulator shown a lesson it has no model of).
	// The nutrient test is not run, rather than recording a verdict the host
	// could not support.
	ErrCannotReplay = errors.New("host cannot replay this lesson")
)

// Journal is the durable, append-only record of everything the engine
// ingests or decides. A record is on disk before Append returns, so a crash
// after Append loses nothing.
type Journal interface {
	AppendEpisode(ctx context.Context, ep types.Episode) error
	AppendOutcome(ctx context.Context, o types.Outcome) error
	// AppendLesson records a lesson's body. Its state changes are events.
	AppendLesson(ctx context.Context, l types.Lesson) error
	AppendEvent(ctx context.Context, ev types.LifecycleEvent) error
	AppendEvidence(ctx context.Context, e types.Evidence) error
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
	Name() string
	Compare(ctx context.Context, original types.Episode, outcomes []types.Outcome,
		with, without types.ReplayResult) (types.Verdict, error)
}

// Reflector turns experience into candidate lessons (Phase 2). It proposes;
// the engine validates, screens, deduplicates and records. A Reflector need
// not set IDs, state, provenance or timestamps; those are the engine's.
type Reflector interface {
	Name() string
	Reflect(ctx context.Context, batch []types.Experience, existing []types.Lesson) ([]types.Lesson, error)
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

// ModelRequest is one completion request to a language model.
type ModelRequest struct {
	System string
	User   string
	// JSON asks the model for a single JSON object.
	JSON        bool
	MaxTokens   int
	Temperature float64
	// DisableThinking asks a model with a hidden-reasoning mode to answer
	// directly. Judges set it: one small question needs no reasoning, and a
	// reasoning model can otherwise spend the whole budget thinking and
	// return nothing (DeepSeek's deepseek-flash thinks by default).
	DisableThinking bool
}

// ModelResponse is a model's reply.
type ModelResponse struct {
	Text         string
	InputTokens  int
	OutputTokens int
	// FinishReason is the provider's reason for stopping ("stop",
	// "length", ...), when it reports one.
	FinishReason string
	// Reasoned is true when the model produced hidden reasoning, which can
	// consume the token budget and leave Text empty.
	Reasoned bool
}

// Model is a language model, local or remote. The engine's reflectors and
// judges speak only to this port; which model answers, and whether anything
// leaves the machine, is decided where adapters are wired (cmd/metabolism),
// behind an explicit opt-in (docs/decisions.md, D-008).
type Model interface {
	// Name identifies provider and model, e.g. "ollama/qwen3:8b".
	Name() string
	Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

// Describe says what came back, for error messages: the stop reason, whether
// hidden reasoning was produced, and an excerpt of the text.
func (r ModelResponse) Describe() string {
	text := strings.TrimSpace(r.Text)
	if len(text) > 160 {
		text = text[:160] + "…"
	}
	if text == "" {
		text = "(empty)"
	}
	return fmt.Sprintf("finish_reason=%q reasoning=%v output_tokens=%d text=%q", r.FinishReason, r.Reasoned, r.OutputTokens, text)
}
