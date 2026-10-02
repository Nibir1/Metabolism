package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/deliver"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// The Phase 3 loop through the CLI: live turns credit a trial lesson,
// `credit -apply` accepts it, `export` delivers it, and a /lessons forget
// recorded by Helix eliminates it at the next ingest.
func TestCreditExportAndForget(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.db")
	now := time.Now().UTC()

	j, err := sqlite.Open(ctx, journal)
	if err != nil {
		t.Fatal(err)
	}
	ep := func(at time.Time, end types.RunEnd, exposed, withheld []types.ID) types.Episode {
		return types.Episode{ID: types.NewID(at), Host: "helix", Scope: types.Scope{Level: types.ScopeGlobal},
			StartedAt: at, EndedAt: at.Add(time.Second), Request: types.Request{Text: "x", Provenance: types.ProvUserTyped},
			End: end, Exposure: exposed, Withheld: withheld}
	}
	src := ep(now.Add(-72*time.Hour), types.RunFailed, nil, nil)
	if err := j.AppendEpisode(ctx, src); err != nil {
		t.Fatal(err)
	}
	m, _ := metabolome.Load(ctx, j)
	m.SetClock(func() time.Time { return now.Add(-60 * time.Hour) })
	l, err := m.Create(ctx, types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
		Text: "Do not call todo add before a task list is loaded.", Sources: []types.ID{src.ID}, Provenance: types.ProvUserTyped}, "t")
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
		Result: types.ResultPass, Summary: "replay", Compared: 3, Wins: 3})
	if err := m.Move(ctx, l.ID, types.StateTrial, "passed", ev.ID); err != nil {
		t.Fatal(err)
	}
	at := now.Add(-48 * time.Hour)
	for i := range 60 {
		at = at.Add(10 * time.Minute)
		end := types.RunFailed
		var exp, wh []types.ID
		if i%2 == 0 {
			exp = []types.ID{l.ID}
			if i%10 != 0 {
				end = types.RunDone // 80% of delivered turns succeed
			}
		} else {
			wh = []types.ID{l.ID}
			if i%6 == 1 {
				end = types.RunDone // 33% of withheld turns succeed
			}
		}
		if err := j.AppendEpisode(ctx, ep(at, end, exp, wh)); err != nil {
			t.Fatal(err)
		}
	}
	_ = j.Close()

	var out bytes.Buffer
	if err := run(ctx, []string{"credit", "-journal", journal}, &out); err != nil {
		t.Fatalf("credit: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "→ accepted") || !strings.Contains(out.String(), "nothing was recorded") {
		t.Fatalf("credit (dry run):\n%s", out.String())
	}
	out.Reset()
	if err := run(ctx, []string{"credit", "-journal", journal, "-apply"}, &out); err != nil {
		t.Fatalf("credit -apply: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "moved 1 lesson") {
		t.Fatalf("credit -apply:\n%s", out.String())
	}

	lessonsFile := filepath.Join(dir, "helix", "lessons.json")
	out.Reset()
	if err := run(ctx, []string{"export", "-journal", journal, "-to", lessonsFile}, &out); err != nil {
		t.Fatalf("export: %v\n%s", err, out.String())
	}
	f, err := deliver.Read(lessonsFile)
	if err != nil || len(f.Lessons) != 1 || f.Lessons[0].State != "accepted" || f.Lessons[0].P != deliver.PAccepted {
		t.Fatalf("delivery file: %v %+v", err, f)
	}

	feedback := filepath.Join(dir, "episodes.ndjson")
	line := fmt.Sprintf(`{"v":2,"kind":"feedback","feedback":{"id":"%s","lesson_id":"%s","at":"%s","action":"forget","reason":"no longer true here"}}`,
		types.NewID(now), l.ID, now.Format(time.RFC3339))
	if err := os.WriteFile(feedback, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 { // twice: a re-ingest must be harmless
		out.Reset()
		if err := run(ctx, []string{"ingest", "-journal", journal, "-from", feedback}, &out); err != nil {
			t.Fatalf("ingest: %v\n%s", err, out.String())
		}
	}
	if !strings.Contains(out.String(), "1 already applied") {
		t.Fatalf("second ingest:\n%s", out.String())
	}
	out.Reset()
	if err := run(ctx, []string{"lessons", "-journal", journal, "-id", string(l.ID)}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "eliminated") || !strings.Contains(out.String(), "no longer true here") {
		t.Fatalf("lesson after forget:\n%s", out.String())
	}
}

func TestCreditEvalCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"credit-eval", "-seeds", "1", "-turns", "600"}, &out); err != nil {
		t.Fatalf("credit-eval: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "lessons that harm, eliminated") {
		t.Fatalf("credit-eval:\n%s", out.String())
	}
}
