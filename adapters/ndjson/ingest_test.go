package ndjson

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/types"
)

func openJournal(t *testing.T) *sqlite.Journal {
	t.Helper()
	j, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

func readGolden(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "helix_wire_v1.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHelixGoldenDecodesIntoEngineTypes is the contract test. The golden file
// is a copy of Helix's internal/metabolism/testdata/wire_v1.ndjson.
func TestHelixGoldenDecodesIntoEngineTypes(t *testing.T) {
	ctx := context.Background()
	j := openJournal(t)
	st, err := Ingest(ctx, j, filepath.Join("testdata", "helix_wire_v1.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Episodes != 1 || st.Outcomes != 2 || st.Invalid+st.Malformed+st.Orphans != 0 {
		t.Fatalf("stats %+v, problems %v", st, st.Problems)
	}
	var ep *types.Episode
	var kinds []types.OutcomeKind
	for r, err := range j.Scan(ctx, types.JournalQuery{}) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Episode != nil {
			ep = r.Episode
		}
		if r.Outcome != nil {
			kinds = append(kinds, r.Outcome.Kind)
		}
	}
	if ep == nil || ep.Host != "helix" || ep.End != types.RunBudgetExhausted ||
		ep.Scope.Level != types.ScopeProject || len(ep.Steps) != 2 || ep.Steps[1].OK ||
		ep.Usage.ModelCalls != 2 || ep.Attrs["machine"] == "" {
		t.Fatalf("episode did not survive the contract: %+v", ep)
	}
	if ep.Steps[0].Duration.Milliseconds() != 12 {
		t.Fatalf("step duration = %v, want 12ms", ep.Steps[0].Duration)
	}
	if len(kinds) != 2 || kinds[0] != types.OutcomeFailure || kinds[1] != types.OutcomeUndone {
		t.Fatalf("outcomes = %v", kinds)
	}
}

func TestReingestIsHarmless(t *testing.T) {
	ctx := context.Background()
	j := openJournal(t)
	golden := filepath.Join("testdata", "helix_wire_v1.ndjson")
	for range 2 {
		if _, err := Ingest(ctx, j, golden); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for range j.Scan(ctx, types.JournalQuery{}) {
		n++
	}
	if n != 3 {
		t.Fatalf("journal holds %d records after two ingests, want 3", n)
	}
}

func TestBadLinesAreCountedNotFatal(t *testing.T) {
	g := readGolden(t)
	path := filepath.Join(t.TempDir(), "episodes.ndjson")
	writeLines(t, path,
		g[1], // outcome before its episode: orphan
		g[0],
		strings.Replace(g[2], `"note"`, `"surprise":1,"note"`, 1), // unknown field
		strings.Replace(g[0], `"v":1`, `"v":3`, 1),                // future version
		strings.Replace(g[0], `"host":"helix"`, `"host":""`, 1),   // fails validation
		`{"v":1,"kind":"episode","episode":{"id":"01`,             // cut off mid-write
	)
	st, err := Ingest(context.Background(), openJournal(t), path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Lines != 6 || st.Episodes != 1 || st.Orphans != 1 || st.Malformed != 3 || st.Invalid != 1 {
		t.Fatalf("stats = %+v", st)
	}
	if len(st.Problems) != 4 {
		t.Fatalf("problems = %v", st.Problems)
	}
}

func TestRotatedGenerationsOldestFirst(t *testing.T) {
	g := readGolden(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "episodes.ndjson")
	// The episode rotated into .2, its outcomes into .1 and the live file. If
	// ingest read the live file first, the outcomes would be orphans.
	writeLines(t, path+".2", g[0])
	writeLines(t, path+".1", g[1])
	writeLines(t, path, g[2])
	writeLines(t, path+".bak", "not a generation")

	files, err := Generations(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{path + ".2", path + ".1", path}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Fatalf("generations = %v, want %v", files, want)
	}
	st, err := Ingest(context.Background(), openJournal(t), path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Episodes != 1 || st.Outcomes != 2 || st.Orphans != 0 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestMissingFileIsAnError(t *testing.T) {
	if _, err := Ingest(context.Background(), openJournal(t), filepath.Join(t.TempDir(), "none.ndjson")); err == nil {
		t.Fatal("ingesting nothing succeeded")
	}
}

// Wire v2 (Phase 3): an episode carries the lessons delivered and withheld,
// and a feedback record carries a /lessons forget. The golden file is a copy
// of Helix's internal/metabolism/testdata/wire_v2.ndjson.
func TestHelixGoldenV2(t *testing.T) {
	ctx := context.Background()
	j := openJournal(t)
	st, err := Ingest(ctx, j, filepath.Join("testdata", "helix_wire_v2.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Episodes != 1 || st.Outcomes != 1 || len(st.Feedback) != 2 || st.Invalid+st.Malformed+st.Orphans != 0 {
		t.Fatalf("stats %+v, problems %v", st, st.Problems)
	}
	fb := st.Feedback[0]
	if fb.Action != FeedbackForget || fb.LessonID != "0199a1b2c3d4e5f6a7b8c9d0e1f4" || fb.Reason == "" {
		t.Fatalf("feedback %+v", fb)
	}
	if hard := st.Feedback[1]; hard.Action != FeedbackForgetHard || hard.LessonID != "0199a1b2c3d4e5f6a7b8c9d0e1f3" {
		t.Fatalf("hard feedback %+v", hard)
	}
	for r, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode}}) {
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Episode.Exposure) != 1 || len(r.Episode.Withheld) != 1 {
			t.Fatalf("episode %+v", r.Episode)
		}
	}
}

// v2 fields in a v1 line, an unknown action and a version from the future
// are refused, not half-read.
func TestWireVersionsAreNotMixed(t *testing.T) {
	ctx := context.Background()
	j := openJournal(t)
	path := filepath.Join(t.TempDir(), "episodes.ndjson")
	writeLines(t, path,
		`{"v":1,"kind":"feedback","feedback":{"id":"0199a1b2c3d6a7b8c9d0e1f2a3b6","lesson_id":"0199a1b2c3d4e5f6a7b8c9d0e1f4","at":"2026-10-03T10:05:00Z","action":"forget","reason":"x"}}`,
		`{"v":2,"kind":"feedback","feedback":{"id":"0199a1b2c3d6a7b8c9d0e1f2a3b7","lesson_id":"0199a1b2c3d4e5f6a7b8c9d0e1f4","at":"2026-10-03T10:05:00Z","action":"promote","reason":"x"}}`,
		`{"v":3,"kind":"outcome","outcome":{}}`,
	)
	st, err := Ingest(ctx, j, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Feedback) != 0 || st.Malformed != 2 || st.Invalid != 1 {
		t.Fatalf("stats %+v, problems %v", st, st.Problems)
	}
}
