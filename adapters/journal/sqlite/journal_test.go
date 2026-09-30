package sqlite

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) *Journal {
	t.Helper()
	j, err := Open(context.Background(), filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

func episodeAt(at time.Time) types.Episode {
	return types.Episode{
		ID:        types.NewID(at),
		Host:      "test",
		Scope:     types.Scope{Level: types.ScopeGlobal},
		StartedAt: at,
		EndedAt:   at.Add(time.Second),
		Request:   types.Request{Text: "do the thing", Provenance: types.ProvUserTyped},
		Steps:     []types.Step{{Tool: "shell", Subject: "true", OK: true}},
		Usage:     types.Usage{ModelCalls: 1, InputChars: 400, OutputChars: 80},
		End:       types.RunDone,
	}
}

func collect(t *testing.T, j *Journal, q types.JournalQuery) []types.Record {
	t.Helper()
	var out []types.Record
	for r, err := range j.Scan(context.Background(), q) {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	j := openTemp(t)

	// A non-UTC time must come back as the same instant.
	ep := episodeAt(t0.In(time.FixedZone("CEST", 2*3600)))
	if err := j.AppendEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	o := types.Outcome{ID: types.NewID(t0), EpisodeID: ep.ID, At: t0.Add(time.Hour),
		Kind: types.OutcomeUndone, Source: types.SourceDerived, Note: "/undo"}
	if err := j.AppendOutcome(ctx, o); err != nil {
		t.Fatal(err)
	}
	ev := types.LifecycleEvent{ID: types.NewID(t0), LessonID: types.NewID(t0), At: t0,
		To: types.StateCandidate, Reason: "reflected"}
	if err := j.AppendEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}

	recs := collect(t, j, types.JournalQuery{})
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	if recs[0].Seq >= recs[1].Seq || recs[1].Seq >= recs[2].Seq {
		t.Fatal("seq is not strictly increasing")
	}
	got := recs[0].Episode
	if got == nil || got.ID != ep.ID || !got.StartedAt.Equal(ep.StartedAt) || got.Steps[0].Subject != "true" {
		t.Fatalf("episode did not round-trip: %+v", got)
	}
	if recs[1].Outcome == nil || recs[1].Outcome.Kind != types.OutcomeUndone {
		t.Fatalf("outcome did not round-trip: %+v", recs[1])
	}
	if recs[2].Event == nil || recs[2].Event.Reason != "reflected" {
		t.Fatalf("event did not round-trip: %+v", recs[2])
	}
}

func TestRetryIsIdempotentButConflictIsNot(t *testing.T) {
	ctx := context.Background()
	j := openTemp(t)
	ep := episodeAt(t0)
	if err := j.AppendEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	if err := j.AppendEpisode(ctx, ep); err != nil {
		t.Fatalf("identical retry should succeed: %v", err)
	}
	if n := len(collect(t, j, types.JournalQuery{})); n != 1 {
		t.Fatalf("retry wrote a second record: %d", n)
	}
	changed := ep
	changed.End = types.RunFailed
	if err := j.AppendEpisode(ctx, changed); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("rewrite under the same id: err = %v, want ErrConflict", err)
	}
}

func TestRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	j := openTemp(t)

	bad := episodeAt(t0)
	bad.Host = ""
	if err := j.AppendEpisode(ctx, bad); !errors.Is(err, ports.ErrInvalid) {
		t.Fatalf("invalid episode: err = %v, want ErrInvalid", err)
	}
	orphan := types.Outcome{ID: types.NewID(t0), EpisodeID: types.NewID(t0), At: t0,
		Kind: types.OutcomeSuccess, Source: types.SourceHost}
	if err := j.AppendOutcome(ctx, orphan); !errors.Is(err, ports.ErrUnknownEpisode) {
		t.Fatalf("orphan outcome: err = %v, want ErrUnknownEpisode", err)
	}
	if n := len(collect(t, j, types.JournalQuery{})); n != 0 {
		t.Fatalf("rejected input was written: %d records", n)
	}
}

func TestAppendOnlyIsEnforcedByTheDatabase(t *testing.T) {
	ctx := context.Background()
	j := openTemp(t)
	if err := j.AppendEpisode(ctx, episodeAt(t0)); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE records SET body = '{}'`,
		`DELETE FROM records`,
	} {
		_, err := j.db.ExecContext(ctx, stmt)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: err = %v, want append-only refusal", stmt, err)
		}
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	j := openTemp(t)
	fi, err := os.Stat(j.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("journal mode = %o, want 600", perm)
	}
	// t.TempDir makes its own directory, so check one Open creates.
	nested := filepath.Join(t.TempDir(), "state", "journal.db")
	j2, err := Open(context.Background(), nested)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j2.Close() }()
	ni, _ := os.Stat(filepath.Dir(nested))
	if perm := ni.Mode().Perm(); perm != 0o700 {
		t.Fatalf("journal dir mode = %o, want 700", perm)
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.ExecContext(ctx, `UPDATE schema_version SET version = ?`, SchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	_ = j.Close()
	if _, err := Open(ctx, path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("opening a newer journal: err = %v, want a refusal", err)
	}
}

func TestReopenKeepsRecordsAndSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.AppendEpisode(ctx, episodeAt(t0)); err != nil {
		t.Fatal(err)
	}
	_ = j.Close()
	j, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = j.Close() }()
	if n := len(collect(t, j, types.JournalQuery{})); n != 1 {
		t.Fatalf("after reopen: %d records, want 1", n)
	}
}

func TestQueryFilters(t *testing.T) {
	ctx := context.Background()
	j := openTemp(t)
	var eps []types.Episode
	for i := range 5 {
		ep := episodeAt(t0.Add(time.Duration(i) * time.Hour))
		eps = append(eps, ep)
		if err := j.AppendEpisode(ctx, ep); err != nil {
			t.Fatal(err)
		}
		o := types.Outcome{ID: types.NewID(ep.StartedAt), EpisodeID: ep.ID, At: ep.EndedAt,
			Kind: types.OutcomeSuccess, Source: types.SourceHost}
		if err := j.AppendOutcome(ctx, o); err != nil {
			t.Fatal(err)
		}
	}

	if n := len(collect(t, j, types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode}})); n != 5 {
		t.Errorf("kind filter: %d, want 5", n)
	}
	window := collect(t, j, types.JournalQuery{
		Kinds: []types.RecordKind{types.RecordEpisode},
		Since: t0.Add(time.Hour), Until: t0.Add(3 * time.Hour),
	})
	if len(window) != 2 || window[0].Episode.ID != eps[1].ID {
		t.Errorf("time window: got %d records", len(window))
	}
	one := collect(t, j, types.JournalQuery{EpisodeID: eps[2].ID})
	if len(one) != 2 || one[0].Kind != types.RecordEpisode || one[1].Kind != types.RecordOutcome {
		t.Errorf("episode filter: %+v", one)
	}
	all := collect(t, j, types.JournalQuery{})
	after := collect(t, j, types.JournalQuery{AfterSeq: all[3].Seq})
	if len(after) != len(all)-4 {
		t.Errorf("after seq: %d, want %d", len(after), len(all)-4)
	}
	if n := len(collect(t, j, types.JournalQuery{Limit: 3})); n != 3 {
		t.Errorf("limit: %d, want 3", n)
	}

	// Stopping early must release the query so the journal stays usable.
	for range j.Scan(ctx, types.JournalQuery{}) {
		break
	}
	if err := j.AppendEpisode(ctx, episodeAt(t0.Add(10*time.Hour))); err != nil {
		t.Fatalf("append after early break: %v", err)
	}
}

// TestCrashLosesNothingConfirmed kills a writer process mid-stream, with no
// chance to close the journal, then reopens it. Every append the writer
// reported as done must be there, and the file must pass SQLite's integrity
// check.
func TestCrashLosesNothingConfirmed(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a process")
	}
	path := filepath.Join(t.TempDir(), "journal.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), "METABOLISM_CRASH_HELPER="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	const target = 150
	confirmed := 0
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if n, ok := strings.CutPrefix(sc.Text(), "appended "); ok {
			confirmed, _ = strconv.Atoi(n)
			if confirmed >= target {
				break
			}
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if confirmed < target {
		t.Fatalf("writer confirmed only %d appends before exiting", confirmed)
	}

	j, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer func() { _ = j.Close() }()
	if err := j.IntegrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := len(collect(t, j, types.JournalQuery{}))
	if got < confirmed {
		t.Fatalf("lost confirmed appends: %d on disk, %d confirmed", got, confirmed)
	}
}

// TestCrashHelper is the writer for TestCrashLosesNothingConfirmed. It does
// nothing unless that test launched it.
func TestCrashHelper(t *testing.T) {
	path := os.Getenv("METABOLISM_CRASH_HELPER")
	if path == "" {
		t.Skip("only runs as a subprocess")
	}
	j, err := Open(context.Background(), path)
	if err != nil {
		fmt.Println("open failed:", err)
		os.Exit(2)
	}
	for i := 1; ; i++ {
		if err := j.AppendEpisode(context.Background(), episodeAt(t0.Add(time.Duration(i)*time.Second))); err != nil {
			fmt.Println("append failed:", err)
			os.Exit(2)
		}
		fmt.Printf("appended %d\n", i)
	}
}
