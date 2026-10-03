package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// The versioned metabolome from the command line (D-028): name a version,
// lose a lesson to a /lessons forget --hard from Helix, see it in versions,
// roll back what can be, forget by text, and verify.
func TestVersionsFromTheCommandLine(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.db")
	rec := filepath.Join(dir, "episodes.ndjson")
	var out bytes.Buffer
	step := func(args ...string) string {
		t.Helper()
		out.Reset()
		if err := run(ctx, args, &out); err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out.String())
		}
		return out.String()
	}
	step("sim", "-journal", journal, "-count", "300")
	step("digest", "-journal", journal, "-reflector", "sim")
	step("verify", "-journal", journal)
	step("snapshot", "-journal", journal, "-note", "after verify", "good")

	j, err := sqlite.Open(ctx, journal)
	if err != nil {
		t.Fatal(err)
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	trial := m.Lessons(types.StateTrial)
	_ = j.Close()
	if len(trial) < 2 {
		t.Fatalf("setup: %d trial lessons", len(trial))
	}
	hard, soft := trial[0], trial[1]
	line := func(id, lesson, action string) string {
		return fmt.Sprintf(`{"v":2,"kind":"feedback","feedback":{"id":%q,"lesson_id":%q,"at":"2026-10-03T12:00:00Z","action":%q,"reason":"test"}}`+"\n",
			id, lesson, action)
	}
	if err := os.WriteFile(rec, []byte(line("01a10239ffff00000000000000a1", string(hard.ID), "forget-hard")+
		line("01a10239ffff00000000000000a2", string(soft.ID), "forget")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := step("ingest", "-journal", journal, "-from", rec); !strings.Contains(got, "1 lesson(s) forgotten in Helix eliminated, 1 deleted with proof") {
		t.Fatalf("ingest:\n%s", got)
	}
	got := step("versions", "-journal", journal)
	if !strings.Contains(got, "by helix: ") || !strings.Contains(got, "no: a forget since removed records") {
		t.Fatalf("versions:\n%s", got)
	}
	// The lesson forgotten by the user stays eliminated; nothing else moved.
	if got := step("rollback", "-journal", journal, "good"); !strings.Contains(got, "your own forget") || !strings.Contains(got, "nothing to roll back") {
		t.Fatalf("rollback:\n%s", got)
	}
	if got := step("lessons", "-journal", journal, "-as-of", "good"); strings.Contains(got, string(hard.ID)) {
		t.Fatalf("a forgotten lesson is still in a past version:\n%s", got)
	}

	if got := step("forget", "-journal", journal, "-match", "settings.py", "-recording", "none"); !strings.Contains(got, "nothing was deleted") {
		t.Fatalf("forget dry run:\n%s", got)
	}
	got = step("forget", "-journal", journal, "-match", "settings.py", "-recording", rec, "-lessons-file", "none", "-apply", "-reason", "test")
	if !strings.Contains(got, "audit record") {
		t.Fatalf("forget:\n%s", got)
	}
	if got := step("forget", "-journal", journal, "-verify", "-recording", rec); !strings.Contains(got, "0 forgotten record(s) still present") {
		t.Fatalf("verify:\n%s", got)
	}
	data, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("settings.py")) {
		t.Fatal("the journal file still holds the forgotten text")
	}
}
