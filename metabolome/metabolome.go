// Package metabolome is the engine's learned state: every lesson, where it is
// in its lifecycle, how it got there, and the evidence behind it.
//
// It is event-sourced. The journal holds immutable lesson bodies, lifecycle
// events and evidence; this package folds them into the current picture and
// is the only code allowed to append new ones. Nothing is updated in place, so
// "why is this lesson trusted?" and "when did it stop being?" are always
// answerable from the journal alone (docs/decisions.md, D-014).
//
// Two rules are enforced here rather than trusted to callers:
//
//   - Lifecycle moves follow types.LessonState's transition table.
//   - A lesson enters trial only on evidence: the move must cite a passing
//     nutrient test for that same lesson. This is rubric C6 made structural,
//     so no code path can promote a lesson that was never tested.
package metabolome

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Entry is one lesson with its history.
type Entry struct {
	// Lesson carries the current State; its other fields never change.
	Lesson   types.Lesson
	Events   []types.LifecycleEvent
	Evidence []types.Evidence
}

// Metabolome is the folded view. It is not safe for concurrent use.
type Metabolome struct {
	j       ports.Journal
	now     func() time.Time
	entries map[types.ID]*Entry
	order   []types.ID
	lastSeq int64
	// applied holds every record ID folded so far. Records this process
	// appends are folded at once and then seen again by the next Refresh;
	// folding must be idempotent or a lesson's history doubles.
	applied map[types.ID]bool
}

// ErrNoEvidence: a move to trial did not cite passing nutrient-test evidence,
// or a move to accepted did not cite passing credit evidence.
var ErrNoEvidence = errors.New("a lesson enters trial only on passing nutrient-test evidence, " +
	"and is accepted only on passing credit evidence")

// Load folds everything in the journal.
func Load(ctx context.Context, j ports.Journal) (*Metabolome, error) {
	m := &Metabolome{j: j, now: time.Now, entries: map[types.ID]*Entry{}, applied: map[types.ID]bool{}}
	return m, m.Refresh(ctx)
}

// SetClock replaces the clock (tests, simulations).
func (m *Metabolome) SetClock(now func() time.Time) { m.now = now }

// Refresh folds records appended since the last Refresh, including any
// appended by another process.
func (m *Metabolome) Refresh(ctx context.Context) error {
	q := types.JournalQuery{
		Kinds:    []types.RecordKind{types.RecordLesson, types.RecordEvent, types.RecordEvidence},
		AfterSeq: m.lastSeq,
	}
	for r, err := range m.j.Scan(ctx, q) {
		if err != nil {
			return err
		}
		m.apply(r)
		m.lastSeq = r.Seq
	}
	return nil
}

func (m *Metabolome) apply(r types.Record) {
	var id types.ID
	switch {
	case r.Lesson != nil:
		id = r.Lesson.ID
	case r.Event != nil:
		id = r.Event.ID
	case r.Evidence != nil:
		id = r.Evidence.ID
	}
	if id == "" || m.applied[id] {
		return
	}
	m.applied[id] = true
	switch {
	case r.Lesson != nil:
		if _, ok := m.entries[r.Lesson.ID]; !ok {
			l := *r.Lesson
			// The body records the state it was created in; the events that
			// follow are the truth about where it is now.
			l.State = ""
			m.entries[l.ID] = &Entry{Lesson: l}
			m.order = append(m.order, l.ID)
		}
	case r.Event != nil:
		if e, ok := m.entries[r.Event.LessonID]; ok {
			e.Events = append(e.Events, *r.Event)
			e.Lesson.State = r.Event.To
			e.Lesson.UpdatedAt = r.Event.At
		}
	case r.Evidence != nil:
		if e, ok := m.entries[r.Evidence.LessonID]; ok {
			e.Evidence = append(e.Evidence, *r.Evidence)
		}
	}
}

// Get returns a lesson's entry.
func (m *Metabolome) Get(id types.ID) (Entry, bool) {
	e, ok := m.entries[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// Lessons returns lessons in creation order, filtered to the given states
// (all states when none are given).
func (m *Metabolome) Lessons(states ...types.LessonState) []types.Lesson {
	var out []types.Lesson
	for _, id := range m.order {
		l := m.entries[id].Lesson
		if len(states) == 0 || slices.Contains(states, l.State) {
			out = append(out, l)
		}
	}
	return out
}

// Create records a new lesson and its creation event. A lesson learned from
// untrusted provenance is quarantined at once: it may never be injected until
// it passes the stricter test quarantine requires.
func (m *Metabolome) Create(ctx context.Context, l types.Lesson, reason string) (types.Lesson, error) {
	now := m.now()
	if l.ID == "" {
		l.ID = types.NewID(now)
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	l.UpdatedAt = l.CreatedAt
	l.State = types.StateCandidate
	if err := l.Validate(); err != nil {
		return types.Lesson{}, fmt.Errorf("%w: %w", ports.ErrInvalid, err)
	}
	if err := m.j.AppendLesson(ctx, l); err != nil {
		return types.Lesson{}, err
	}
	m.apply(types.Record{Lesson: &l})
	if err := m.appendEvent(ctx, l.ID, "", types.StateCandidate, reason, nil); err != nil {
		return types.Lesson{}, err
	}
	if !l.Provenance.Trusted() {
		if err := m.appendEvent(ctx, l.ID, types.StateCandidate, types.StateQuarantined,
			"learned from "+string(l.Provenance)+" content", nil); err != nil {
			return types.Lesson{}, err
		}
	}
	e, _ := m.Get(l.ID)
	return e.Lesson, nil
}

// RecordEvidence journals a test result about a lesson.
func (m *Metabolome) RecordEvidence(ctx context.Context, ev types.Evidence) (types.Evidence, error) {
	if _, ok := m.entries[ev.LessonID]; !ok {
		return types.Evidence{}, fmt.Errorf("%w: %s", ports.ErrUnknownLesson, ev.LessonID)
	}
	if ev.ID == "" {
		ev.ID = types.NewID(m.now())
	}
	if ev.At.IsZero() {
		ev.At = m.now()
	}
	if err := m.j.AppendEvidence(ctx, ev); err != nil {
		return types.Evidence{}, err
	}
	m.apply(types.Record{Evidence: &ev})
	return ev, nil
}

// Move changes a lesson's state, citing the evidence that justifies it.
func (m *Metabolome) Move(ctx context.Context, id types.ID, to types.LessonState, reason string, evidence ...types.ID) error {
	e, ok := m.entries[id]
	if !ok {
		return fmt.Errorf("%w: %s", ports.ErrUnknownLesson, id)
	}
	from := e.Lesson.State
	if !from.CanTransition(to) {
		return fmt.Errorf("%w: lesson %s cannot move %s → %s", ports.ErrInvalid, id, from, to)
	}
	if to == types.StateTrial && !cites(e, evidence, types.EvidenceNutrientTest) {
		return fmt.Errorf("%w (lesson %s)", ErrNoEvidence, id)
	}
	// Accepted is earned on live outcomes, not on replays: a lesson that
	// helped plans in replay must also help real turns (D-020).
	if to == types.StateAccepted && from == types.StateTrial && !cites(e, evidence, types.EvidenceCredit) {
		return fmt.Errorf("%w (lesson %s)", ErrNoEvidence, id)
	}
	return m.appendEvent(ctx, id, from, to, reason, evidence)
}

// cites reports whether cited names passing evidence of kind for e.
func cites(e *Entry, cited []types.ID, kind types.EvidenceKind) bool {
	for _, ev := range e.Evidence {
		if ev.Kind == kind && ev.Passed() && slices.Contains(cited, ev.ID) {
			return true
		}
	}
	return false
}

func (m *Metabolome) appendEvent(ctx context.Context, id types.ID, from, to types.LessonState, reason string, evidence []types.ID) error {
	ev := types.LifecycleEvent{
		ID: types.NewID(m.now()), LessonID: id, At: m.now(),
		From: from, To: to, Reason: reason, Evidence: evidence,
	}
	if err := m.j.AppendEvent(ctx, ev); err != nil {
		return err
	}
	m.apply(types.Record{Event: &ev})
	return nil
}
