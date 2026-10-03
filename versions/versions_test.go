package versions

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

type world struct {
	ctx context.Context
	j   *sqlite.Journal
	m   *metabolome.Metabolome
	eps []types.Episode
	at  time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	w := &world{ctx: ctx, j: j, at: t0}
	for i, text := range []string{"find the config loader", "deploy with token sk-secret-42", "commit my changes"} {
		w.at = w.at.Add(time.Minute)
		ep := types.Episode{ID: types.NewID(w.at), Host: "t", Scope: types.Scope{Level: types.ScopeGlobal},
			StartedAt: w.at, EndedAt: w.at.Add(time.Second), Request: types.Request{Text: text, Provenance: types.ProvUserTyped},
			End: types.RunFailed}
		if err := j.AppendEpisode(ctx, ep); err != nil {
			t.Fatal(err)
		}
		o := types.Outcome{ID: types.NewID(w.at.Add(2 * time.Second)), EpisodeID: ep.ID, At: w.at.Add(2 * time.Second),
			Kind: types.OutcomeFailure, Source: types.SourceHost, Note: "turn " + string(rune('a'+i))}
		if err := j.AppendOutcome(ctx, o); err != nil {
			t.Fatal(err)
		}
		w.eps = append(w.eps, ep)
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	m.SetClock(func() time.Time { w.at = w.at.Add(time.Second); return w.at })
	w.m = m
	return w
}

// lesson creates a lesson from source episode i and moves it to state.
func (w *world) lesson(t *testing.T, text string, i int, state types.LessonState) types.Lesson {
	t.Helper()
	l, err := w.m.Create(w.ctx, types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
		Text: text, Sources: []types.ID{w.eps[i].ID}, Provenance: types.ProvUserTyped}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if state == types.StateCandidate {
		return l
	}
	pass, _ := w.m.RecordEvidence(w.ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
		Result: types.ResultPass, Summary: "replay", Compared: 3, Wins: 3})
	if err := w.m.Move(w.ctx, l.ID, types.StateTrial, "passed", pass.ID); err != nil {
		t.Fatal(err)
	}
	switch state {
	case types.StateAccepted:
		cr, _ := w.m.RecordEvidence(w.ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceCredit,
			Result: types.ResultPass, Summary: "credit", Exposed: 30, ExposedSuccesses: 28, Withheld: 10, WithheldSuccesses: 3})
		if err := w.m.Move(w.ctx, l.ID, types.StateAccepted, "credit", cr.ID); err != nil {
			t.Fatal(err)
		}
	case types.StateEliminated:
		if err := w.m.Move(w.ctx, l.ID, types.StateEliminated, "credit: worse"); err != nil {
			t.Fatal(err)
		}
	}
	e, _ := w.m.Get(l.ID)
	return e.Lesson
}

func (w *world) state(id types.ID) types.LessonState {
	e, _ := w.m.Get(id)
	return e.Lesson.State
}

// A version is the fold up to its position; diff shows what moved since;
// rollback sets aside what was not delivered then and brings back, to
// trial, what was, and can itself be rolled back.
func TestSnapshotDiffAndRollback(t *testing.T) {
	w := newWorld(t)
	good := w.lesson(t, "an empty search is the answer", 0, types.StateAccepted)
	tried := w.lesson(t, "review what is staged before committing", 2, types.StateTrial)
	tag, err := Snapshot(w.ctx, w.j, "good", "before the bad day", w.at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(w.ctx, w.j, "good", "", w.at); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}

	// The bad day: the good lesson is eliminated, a new one enters trial.
	if err := w.m.Move(w.ctx, good.ID, types.StateEliminated, "credit: worse when delivered (a bad settle)"); err != nil {
		t.Fatal(err)
	}
	newer := w.lesson(t, "always widen an empty search", 0, types.StateTrial)

	past, seq, err := At(w.ctx, w.j, "good")
	if err != nil || seq != tag.Seq {
		t.Fatalf("at good: seq %d, %v", seq, err)
	}
	if Fingerprint(past) != tag.Fingerprint {
		t.Fatal("the fold at the tag does not reproduce the tag's fingerprint")
	}
	if _, err := past.Create(w.ctx, types.Lesson{}, "x"); !errors.Is(err, metabolome.ErrPastVersion) {
		t.Fatalf("a past version accepted a write: %v", err)
	}
	changes := Diff(past, w.m)
	if len(changes) != 2 || changes[0].Lesson.ID != good.ID || changes[0].To != types.StateEliminated ||
		changes[1].Lesson.ID != newer.ID || changes[1].From != "" {
		t.Fatalf("diff: %+v", changes)
	}

	steps := PlanRollback(past, w.m)
	if len(steps) != 2 {
		t.Fatalf("plan: %+v", steps)
	}
	before, err := Rollback(w.ctx, w.j, w.m, steps, seq, "good", w.at)
	if err != nil {
		t.Fatal(err)
	}
	if w.state(good.ID) != types.StateTrial || w.state(newer.ID) != types.StateSuperseded || w.state(tried.ID) != types.StateTrial {
		t.Fatalf("after rollback: good %s, newer %s, tried %s", w.state(good.ID), w.state(newer.ID), w.state(tried.ID))
	}
	e, _ := w.m.Get(newer.ID)
	if last := e.Evidence[len(e.Evidence)-1]; last.Kind != types.EvidenceRollback || last.Version != seq {
		t.Fatalf("rollback evidence: %+v", last)
	}
	if _, ok := w.m.SupersededBy(newer.ID); ok {
		t.Fatal("a lesson set aside by rollback names a winner, so credit would restore it")
	}

	// Rolling back the rollback.
	prior, pseq, err := At(w.ctx, w.j, before.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Rollback(w.ctx, w.j, w.m, PlanRollback(prior, w.m), pseq, before.Name, w.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if w.state(newer.ID) != types.StateTrial {
		t.Fatalf("undo: newer is %s", w.state(newer.ID))
	}
}

// An eliminated lesson comes back only through rollback, and only to trial.
func TestEliminationIsFinalExceptByRollback(t *testing.T) {
	w := newWorld(t)
	l := w.lesson(t, "an empty search is the answer", 0, types.StateEliminated)
	e, _ := w.m.Get(l.ID)
	if err := w.m.Move(w.ctx, l.ID, types.StateTrial, "trust me", e.Evidence[0].ID); !errors.Is(err, metabolome.ErrEliminatedIsFinal) {
		t.Fatalf("eliminated → trial without rollback: %v", err)
	}
}

// Forgetting an episode takes its outcomes and the lessons learned from it,
// leaves an audit record, and the lesson cannot be proposed back.
func TestForgetCascadesToLessons(t *testing.T) {
	w := newWorld(t)
	learned := w.lesson(t, "deploy with the token in the request", 1, types.StateTrial)
	other := w.lesson(t, "an empty search is the answer", 0, types.StateAccepted)
	plan, err := PlanForget(w.ctx, w.j, w.m, ForgetRequest{Match: "sk-secret-42"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Episodes) != 1 || plan.Episodes[0] != w.eps[1].ID || len(plan.Learned) != 1 || plan.Learned[0] != learned.ID {
		t.Fatalf("plan: %+v", plan)
	}
	f, err := Forget(w.ctx, w.j, w.m, plan, "pasted a token", "user", w.at)
	if err != nil {
		t.Fatal(err)
	}
	// episode + outcome + lesson + its events (candidate, trial) + evidence
	if len(f.Targets) != 6 {
		t.Fatalf("targets: %d %+v", len(f.Targets), f.Targets)
	}
	if _, ok := w.m.Get(learned.ID); ok {
		t.Fatal("the learned lesson is still in the metabolome")
	}
	if w.state(other.ID) != types.StateAccepted {
		t.Fatal("an unrelated lesson changed")
	}
	if !w.m.ForgottenText("Deploy with the token, in the request!") {
		t.Fatal("the forgotten lesson's text is not recognised")
	}
	fresh, err := metabolome.Load(w.ctx, w.j)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(fresh) != Fingerprint(w.m) || len(fresh.Forgets()) != 1 {
		t.Fatal("a fresh load disagrees with the live metabolome after the forget")
	}
	for r, err := range w.j.Scan(w.ctx, types.JournalQuery{}) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Forget == nil && strings.Contains(string(mustJSON(t, r)), "sk-secret-42") {
			t.Fatalf("a %s record still holds the text", r.Kind)
		}
	}
	if _, err := PlanForget(w.ctx, w.j, w.m, ForgetRequest{Match: "sk-secret-42"}); !ErrNothingToForget(err) {
		t.Fatalf("second forget: %v", err)
	}
}

// With KeepLessons, lessons learned from a forgotten episode are eliminated
// with the reason instead of deleted.
func TestForgetKeepLessons(t *testing.T) {
	w := newWorld(t)
	learned := w.lesson(t, "deploy carefully", 1, types.StateTrial)
	plan, err := PlanForget(w.ctx, w.j, w.m, ForgetRequest{Episodes: []types.ID{w.eps[1].ID}, KeepLessons: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Eliminate) != 1 || len(plan.Learned) != 0 {
		t.Fatalf("plan: %+v", plan)
	}
	if _, err := Forget(w.ctx, w.j, w.m, plan, "private", "user", w.at); err != nil {
		t.Fatal(err)
	}
	e, ok := w.m.Get(learned.ID)
	if !ok || e.Lesson.State != types.StateEliminated || !strings.Contains(e.Events[len(e.Events)-1].Reason, "private") {
		t.Fatalf("kept lesson: %+v", e)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Rollback undoes the evidence's decisions, not the user's: a lesson the
// user forgot since stays eliminated.
func TestRollbackLeavesTheUsersForgetAlone(t *testing.T) {
	w := newWorld(t)
	l := w.lesson(t, "an empty search is the answer", 0, types.StateTrial)
	if _, err := Snapshot(w.ctx, w.j, "v", "", w.at); err != nil {
		t.Fatal(err)
	}
	if err := w.m.Move(w.ctx, l.ID, types.StateEliminated, metabolome.ReasonUserForget+" in Helix: wrong"); err != nil {
		t.Fatal(err)
	}
	past, _, err := At(w.ctx, w.j, "v")
	if err != nil {
		t.Fatal(err)
	}
	steps := PlanRollback(past, w.m)
	if len(steps) != 1 || steps[0].To != "" || !strings.Contains(steps[0].Why, "your own forget") {
		t.Fatalf("plan: %+v", steps)
	}
}
