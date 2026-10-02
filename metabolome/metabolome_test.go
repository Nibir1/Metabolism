package metabolome

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*sqlite.Journal, *Metabolome, types.Episode) {
	t.Helper()
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	ep := types.Episode{
		ID: types.NewID(t0), Host: "t", Scope: types.Scope{Level: types.ScopeGlobal},
		StartedAt: t0, EndedAt: t0, Request: types.Request{Text: "x", Provenance: types.ProvUserTyped}, End: types.RunFailed,
	}
	if err := j.AppendEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	m, err := Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	return j, m, ep
}

func draft(ep types.Episode, prov types.Provenance) types.Lesson {
	return types.Lesson{
		Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
		Text: "an empty search result is an answer", Sources: []types.ID{ep.ID}, Provenance: prov,
	}
}

func evidence(id types.ID, r types.TestResult) types.Evidence {
	return types.Evidence{LessonID: id, Kind: types.EvidenceNutrientTest, Result: r,
		Summary: "test", Compared: 3, Wins: 3}
}

func TestCreateTrustedStartsAsCandidate(t *testing.T) {
	_, m, ep := setup(t)
	l, err := m.Create(context.Background(), draft(ep, types.ProvUserTyped), "reflected")
	if err != nil {
		t.Fatal(err)
	}
	if l.State != types.StateCandidate || l.ID == "" {
		t.Fatalf("created %+v", l)
	}
}

func TestCreateFromUntrustedIsQuarantined(t *testing.T) {
	_, m, ep := setup(t)
	l, err := m.Create(context.Background(), draft(ep, types.ProvRetrievedWeb), "reflected")
	if err != nil {
		t.Fatal(err)
	}
	if l.State != types.StateQuarantined {
		t.Fatalf("a lesson from web content is %s, want quarantined", l.State)
	}
}

// C6, structurally: no evidence, no trial.
func TestTrialRequiresPassingEvidenceForThatLesson(t *testing.T) {
	ctx := context.Background()
	_, m, ep := setup(t)
	a, _ := m.Create(ctx, draft(ep, types.ProvUserTyped), "reflected")
	b, _ := m.Create(ctx, draft(ep, types.ProvUserTyped), "reflected")

	if err := m.Move(ctx, a.ID, types.StateTrial, "trust me"); !errors.Is(err, ErrNoEvidence) {
		t.Fatalf("trial without evidence: err = %v", err)
	}
	fail, _ := m.RecordEvidence(ctx, evidence(a.ID, types.ResultFail))
	if err := m.Move(ctx, a.ID, types.StateTrial, "failed test", fail.ID); !errors.Is(err, ErrNoEvidence) {
		t.Fatalf("trial on failing evidence: err = %v", err)
	}
	other, _ := m.RecordEvidence(ctx, evidence(b.ID, types.ResultPass))
	if err := m.Move(ctx, a.ID, types.StateTrial, "someone else's pass", other.ID); !errors.Is(err, ErrNoEvidence) {
		t.Fatalf("trial on another lesson's evidence: err = %v", err)
	}
	pass, _ := m.RecordEvidence(ctx, evidence(a.ID, types.ResultPass))
	if err := m.Move(ctx, a.ID, types.StateTrial, "passed", pass.ID); err != nil {
		t.Fatalf("trial on passing evidence: %v", err)
	}
	if e, _ := m.Get(a.ID); e.Lesson.State != types.StateTrial {
		t.Fatalf("state = %s", e.Lesson.State)
	}
}

// Acceptance is earned on live outcomes: a passing replay test is not enough.
func TestAcceptedRequiresPassingCreditEvidence(t *testing.T) {
	ctx := context.Background()
	_, m, ep := setup(t)
	l, _ := m.Create(ctx, draft(ep, types.ProvUserTyped), "reflected")
	pass, _ := m.RecordEvidence(ctx, evidence(l.ID, types.ResultPass))
	if err := m.Move(ctx, l.ID, types.StateTrial, "passed", pass.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(ctx, l.ID, types.StateAccepted, "replay passed", pass.ID); !errors.Is(err, ErrNoEvidence) {
		t.Fatalf("accepted on nutrient-test evidence: err = %v", err)
	}
	credit := func(r types.TestResult) types.Evidence {
		return types.Evidence{LessonID: l.ID, Kind: types.EvidenceCredit, Result: r, Summary: "credit",
			Exposed: 30, ExposedSuccesses: 25, Withheld: 20, WithheldSuccesses: 10, PBetter: 0.99}
	}
	fail, _ := m.RecordEvidence(ctx, credit(types.ResultFail))
	if err := m.Move(ctx, l.ID, types.StateAccepted, "failed credit", fail.ID); !errors.Is(err, ErrNoEvidence) {
		t.Fatalf("accepted on failing credit: err = %v", err)
	}
	ok, _ := m.RecordEvidence(ctx, credit(types.ResultPass))
	if err := m.Move(ctx, l.ID, types.StateAccepted, "credit passed", ok.ID); err != nil {
		t.Fatalf("accepted on passing credit: %v", err)
	}
}

func TestIllegalMovesAreRefused(t *testing.T) {
	ctx := context.Background()
	_, m, ep := setup(t)
	l, _ := m.Create(ctx, draft(ep, types.ProvUserTyped), "reflected")
	if err := m.Move(ctx, l.ID, types.StateAccepted, "skip the test"); !errors.Is(err, ports.ErrInvalid) {
		t.Fatalf("candidate → accepted: err = %v", err)
	}
	if err := m.Move(ctx, l.ID, types.StateEliminated, "rejected"); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(ctx, l.ID, types.StateCandidate, "resurrect"); !errors.Is(err, ports.ErrInvalid) {
		t.Fatalf("eliminated → candidate: err = %v", err)
	}
}

// The picture is rebuilt identically from the journal, and folding records
// this process wrote does not double them.
func TestReloadAndRefreshAreIdempotent(t *testing.T) {
	ctx := context.Background()
	j, m, ep := setup(t)
	l, _ := m.Create(ctx, draft(ep, types.ProvUserTyped), "reflected")
	pass, _ := m.RecordEvidence(ctx, evidence(l.ID, types.ResultPass))
	if err := m.Move(ctx, l.ID, types.StateTrial, "passed", pass.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	e, _ := m.Get(l.ID)
	if len(e.Events) != 2 || len(e.Evidence) != 1 {
		t.Fatalf("after refresh: %d events, %d evidence (want 2, 1)", len(e.Events), len(e.Evidence))
	}

	fresh, err := Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := fresh.Get(l.ID)
	if !ok || f.Lesson.State != types.StateTrial || len(f.Events) != 2 || len(f.Evidence) != 1 {
		t.Fatalf("reloaded: %+v", f)
	}
	if got := fresh.Lessons(types.StateTrial); len(got) != 1 {
		t.Fatalf("Lessons(trial) = %d", len(got))
	}
}
