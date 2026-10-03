// Package versions is the versioned metabolome (Phase 4d, D-028): named
// versions, the metabolome as of any version, what changed between two,
// rollback, and forget-with-proof.
//
// The metabolome is a fold of the append-only journal, so a version is a
// journal position and costs nothing to keep. Rollback does not rewrite
// history: it appends lifecycle events, through the metabolome (D-014),
// that put the lessons in force back to what they were. Forget is the only
// operation that removes anything, and it leaves an audit record that can
// never be removed.
package versions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Tags returns the named versions, oldest first.
func Tags(ctx context.Context, j ports.Journal) ([]types.Tag, error) {
	var out []types.Tag
	for r, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{types.RecordTag}}) {
		if err != nil {
			return nil, err
		}
		out = append(out, *r.Tag)
	}
	return out, nil
}

// Resolve turns a version reference into a journal position: "now" (or
// empty), a tag's name, or a journal Seq. It returns the tag when the
// reference named one.
func Resolve(ctx context.Context, j ports.Journal, ref string) (int64, *types.Tag, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "now" {
		seq, err := j.LastSeq(ctx)
		return seq, nil, err
	}
	tags, err := Tags(ctx, j)
	if err != nil {
		return 0, nil, err
	}
	for i := len(tags) - 1; i >= 0; i-- {
		if tags[i].Name == ref {
			return tags[i].Seq, &tags[i], nil
		}
	}
	seq, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || seq <= 0 {
		return 0, nil, fmt.Errorf("no version %q: name a tag (metabolism versions), a journal position, or now", ref)
	}
	last, err := j.LastSeq(ctx)
	if err != nil {
		return 0, nil, err
	}
	if seq > last {
		return 0, nil, fmt.Errorf("version %d is past the end of the journal (%d)", seq, last)
	}
	return seq, nil, nil
}

// At loads the metabolome as of a version reference: the live one for now
// (or empty), a read-only past version otherwise.
func At(ctx context.Context, j ports.Journal, ref string) (*metabolome.Metabolome, int64, error) {
	seq, _, err := Resolve(ctx, j, ref)
	if err != nil {
		return nil, 0, err
	}
	if r := strings.TrimSpace(ref); r == "" || r == "now" {
		m, err := metabolome.Load(ctx, j)
		return m, seq, err
	}
	m, err := metabolome.LoadAsOf(ctx, j, seq)
	return m, seq, err
}

// Fingerprint is a hash of every lesson's ID and state: two folds with the
// same fingerprint hold the same lessons in the same states.
func Fingerprint(m *metabolome.Metabolome) string {
	h := sha256.New()
	ls := m.Lessons()
	slices.SortFunc(ls, func(a, b types.Lesson) int { return strings.Compare(string(a.ID), string(b.ID)) })
	for _, l := range ls {
		_, _ = fmt.Fprintf(h, "%s %s\n", l.ID, l.State)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Snapshot names the current version.
func Snapshot(ctx context.Context, j ports.Journal, name, note string, now time.Time) (types.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "now" {
		return types.Tag{}, fmt.Errorf("%w: a version needs a name other than %q", ports.ErrInvalid, name)
	}
	if _, err := strconv.ParseInt(name, 10, 64); err == nil {
		return types.Tag{}, fmt.Errorf("%w: a name that is a number would read as a journal position", ports.ErrInvalid)
	}
	tags, err := Tags(ctx, j)
	if err != nil {
		return types.Tag{}, err
	}
	if slices.ContainsFunc(tags, func(t types.Tag) bool { return t.Name == name }) {
		return types.Tag{}, fmt.Errorf("%w: a version named %q exists", ports.ErrConflict, name)
	}
	seq, err := j.LastSeq(ctx)
	if err != nil {
		return types.Tag{}, err
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return types.Tag{}, err
	}
	t := types.Tag{ID: types.NewID(now), At: now.UTC(), Name: name, Note: note, Seq: seq, Fingerprint: Fingerprint(m)}
	return t, j.AppendTag(ctx, t)
}

// Change is one lesson that differs between two versions.
type Change struct {
	Lesson types.Lesson `json:"lesson"`
	// From and To are its states; From is empty for a lesson created
	// between the two versions.
	From types.LessonState `json:"from,omitempty"`
	To   types.LessonState `json:"to,omitempty"`
}

// Diff lists the lessons whose state differs between from and to, in
// creation order. A lesson forgotten since is missing from both folds: a
// forget deletes it from every version, and only its forget record says it
// was there.
func Diff(from, to *metabolome.Metabolome) []Change {
	var out []Change
	for _, l := range to.Lessons() {
		e, ok := from.Get(l.ID)
		switch {
		case !ok:
			out = append(out, Change{Lesson: l, To: l.State})
		case e.Lesson.State != l.State:
			out = append(out, Change{Lesson: l, From: e.Lesson.State, To: l.State})
		}
	}
	return out
}

// Step is what a rollback does to one lesson.
type Step struct {
	Lesson types.Lesson      `json:"lesson"`
	From   types.LessonState `json:"from"`
	To     types.LessonState `json:"to,omitempty"` // empty: left alone
	Why    string            `json:"why"`
}

// PlanRollback compares the lessons in force now with those in force at
// the past version, and returns what rollback does (D-028):
//
//   - in force now, not then: set aside (superseded, by rollback);
//   - in force then, now eliminated or superseded: back to trial, on a coin
//     flip, to earn acceptance again; never straight to accepted;
//   - in force then, eliminated since by the user's own forget: left alone,
//     and said so: rollback undoes the evidence's decisions, not the user's;
//   - in force both times: left alone, whatever its state, since it was
//     delivered then too;
//   - anything else (candidates, quarantined, eliminated both times): left
//     alone: rollback is about what is delivered.
//
// A lesson forgotten since is gone from every version and cannot come back.
func PlanRollback(past, now *metabolome.Metabolome) []Step {
	var out []Step
	for _, l := range now.Lessons() {
		then, existed := past.Get(l.ID)
		inThen := existed && then.Lesson.State.Injectable()
		inNow := l.State.Injectable()
		cur, _ := now.Get(l.ID)
		switch {
		case inNow && !inThen:
			why := "it was not delivered in that version"
			if !existed {
				why = "it did not exist in that version"
			}
			out = append(out, Step{Lesson: l, From: l.State, To: types.StateSuperseded, Why: why})
		case inThen && l.State == types.StateEliminated && metabolome.ByUser(cur):
			out = append(out, Step{Lesson: l, From: l.State, Why: "you forgot it since; rollback does not undo your own forget"})
		case inThen && (l.State == types.StateEliminated || l.State == types.StateSuperseded):
			out = append(out, Step{Lesson: l, From: l.State, To: types.StateTrial, Why: fmt.Sprintf(
				"it was %s in that version; it comes back to trial and earns acceptance again", then.Lesson.State)})
		}
	}
	return out
}

// Rollback applies a plan, as the metabolome's own moves: each lesson gets
// rollback evidence naming the version, then moves, citing it. A lesson
// coming back to trial also cites the nutrient test it passed to enter
// trial the first time. Before anything moves, the current version is
// tagged, so a rollback can itself be rolled back.
func Rollback(ctx context.Context, j ports.Journal, m *metabolome.Metabolome, steps []Step, seq int64, label string, now time.Time) (types.Tag, error) {
	before, err := Snapshot(ctx, j, fmt.Sprintf("before-rollback-%s", now.UTC().Format("20060102T150405Z")),
		"taken automatically before rolling back to "+label, now)
	if err != nil {
		return types.Tag{}, err
	}
	for _, s := range steps {
		if s.To == "" {
			continue
		}
		reason := fmt.Sprintf("rollback to %s: %s", label, s.Why)
		ev, err := m.RecordEvidence(ctx, types.Evidence{LessonID: s.Lesson.ID, Kind: types.EvidenceRollback,
			Result: types.ResultInconclusive, Summary: reason, Version: seq})
		if err != nil {
			return before, err
		}
		cite := []types.ID{ev.ID}
		if s.To == types.StateTrial {
			e, _ := m.Get(s.Lesson.ID)
			var passed types.ID
			for _, x := range e.Evidence {
				if x.Kind == types.EvidenceNutrientTest && x.Passed() {
					passed = x.ID
				}
			}
			if passed == "" {
				return before, fmt.Errorf("lesson %s has no passing nutrient test to return to trial on", s.Lesson.ID)
			}
			cite = append(cite, passed)
		}
		if err := m.Move(ctx, s.Lesson.ID, s.To, reason, cite...); err != nil {
			return before, fmt.Errorf("lesson %s: %w", s.Lesson.ID, err)
		}
	}
	return before, nil
}
