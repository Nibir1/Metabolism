package deliver

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

// The golden file is the contract with Helix: it decodes with unknown fields
// refused, and encoding it again gives the same bytes.
func TestGoldenFileIsTheEncoding(t *testing.T) {
	path := filepath.Join("testdata", "lessons_v1.json")
	f, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Lessons) != 2 || f.Lessons[0].P != PAccepted || f.Lessons[1].Scope.Key != "helix-1a2b3c4d" {
		t.Fatalf("decoded %+v", f)
	}
	again, _ := json.MarshalIndent(f, "", "  ")
	want, _ := os.ReadFile(path)
	if !bytes.Equal(append(again, '\n'), want) {
		t.Fatalf("encoding drifted:\n%s", again)
	}
}

func TestReadRefusesOtherVersionsAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"v2.json":      `{"v":2,"generated_at":"2026-10-03T09:00:00Z","lessons":[]}`,
		"unknown.json": `{"v":1,"generated_at":"2026-10-03T09:00:00Z","lessons":[],"authority":"admin"}`,
	} {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(body), 0o600)
		if _, err := Read(p); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Build lists trial and accepted lessons only, accepted first, and Write
// leaves a private file a host can read.
func TestBuildAndWrite(t *testing.T) {
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	src := types.Episode{ID: types.NewID(t0), Host: "t", Scope: types.Scope{Level: types.ScopeGlobal},
		StartedAt: t0, EndedAt: t0, Request: types.Request{Text: "x", Provenance: types.ProvUserTyped}, End: types.RunFailed}
	_ = j.AppendEpisode(ctx, src)
	m, _ := metabolome.Load(ctx, j)
	n := 0
	mk := func(to types.LessonState) types.Lesson {
		n++
		m.SetClock(func() time.Time { return t0.Add(time.Duration(n) * time.Minute) })
		l, err := m.Create(ctx, types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
			Text: "lesson " + string(rune('a'+n)), Sources: []types.ID{src.ID}, Provenance: types.ProvUserTyped}, "t")
		if err != nil {
			t.Fatal(err)
		}
		if to == types.StateCandidate {
			return l
		}
		ev, _ := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
			Result: types.ResultPass, Summary: "replay", Compared: 3, Wins: 3})
		_ = m.Move(ctx, l.ID, types.StateTrial, "passed", ev.ID)
		if to == types.StateAccepted {
			cr, _ := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceCredit,
				Result: types.ResultPass, Summary: "credit", Exposed: 20, ExposedSuccesses: 18, Withheld: 10, PBetter: 0.99})
			if err := m.Move(ctx, l.ID, types.StateAccepted, "credit", cr.ID); err != nil {
				t.Fatal(err)
			}
		}
		return l
	}
	trial, _, accepted := mk(types.StateTrial), mk(types.StateCandidate), mk(types.StateAccepted)

	f := Build(m, map[types.ID]*ledger.Tally{trial.ID: {Exposed: 3, ExposedSuccesses: 3}}, t0)
	if len(f.Lessons) != 2 || f.Lessons[0].ID != string(accepted.ID) || f.Lessons[1].ID != string(trial.ID) {
		t.Fatalf("built %+v", f.Lessons)
	}
	if f.Lessons[0].P != PAccepted || f.Lessons[1].P != PTrial || f.Lessons[0].Evidence == "" || f.Lessons[1].Credit == "" {
		t.Fatalf("built %+v", f.Lessons)
	}

	path := filepath.Join(t.TempDir(), "metabolism", "lessons.json")
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if got, err := Read(path); err != nil || len(got.Lessons) != 2 {
		t.Fatalf("read back: %v %+v", err, got)
	}
}

func TestSelectRespectsScopeBudgetAndTheCoin(t *testing.T) {
	ep := types.Episode{Scope: types.Scope{Level: types.ScopeProject, Key: "p"}, Attrs: types.Attrs{"machine": "m"}}
	f := File{V: 1, Lessons: []Lesson{
		{ID: "always", Text: "a", P: 1, State: "accepted", Scope: Scope{Level: "global"}},
		{ID: "never", Text: "b", P: 0, State: "accepted", Scope: Scope{Level: "machine", Key: "m"}},
		{ID: "elsewhere", Text: "c", P: 1, Scope: Scope{Level: "project", Key: "other"}},
		{ID: "over-budget", Text: "d", P: 1, Scope: Scope{Level: "global"}},
	}}
	s := Select(f, ep, types.Budget{MaxLessons: 2}, rand.New(rand.NewPCG(1, 2)))
	if len(s.Delivered) != 1 || s.Delivered[0] != "always" || len(s.Withheld) != 1 || s.Withheld[0] != "never" {
		t.Fatalf("selection %+v", s)
	}

	// Trial lessons past the budget still reach turns: the budget is shared.
	f = File{V: 1, Lessons: []Lesson{
		{ID: "t1", Text: "a", P: 1, State: "trial", Scope: Scope{Level: "global"}},
		{ID: "t2", Text: "b", P: 1, State: "trial", Scope: Scope{Level: "global"}},
		{ID: "t3", Text: "c", P: 1, State: "trial", Scope: Scope{Level: "global"}},
	}}
	seen := map[types.ID]int{}
	r0 := rand.New(rand.NewPCG(5, 6))
	for range 300 {
		for _, id := range Select(f, ep, types.Budget{MaxLessons: 1}, r0).Delivered {
			seen[id]++
		}
	}
	if seen["t1"] < 50 || seen["t2"] < 50 || seen["t3"] < 50 {
		t.Fatalf("budget share over 300 turns: %v", seen)
	}

	// Over many turns a trial lesson is delivered about half the time.
	r := rand.New(rand.NewPCG(3, 4))
	f = File{V: 1, Lessons: []Lesson{{ID: "x", Text: "x", P: PTrial, Scope: Scope{Level: "global"}}}}
	d := 0
	for range 2000 {
		d += len(Select(f, ep, types.Budget{}, r).Delivered)
	}
	if d < 900 || d > 1100 {
		t.Fatalf("delivered %d of 2000 at p=0.5", d)
	}
}
