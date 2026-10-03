package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// secret is a marker that appears only in the episode the test forgets.
const secret = "sk-test-0123456789abcdef-PRIVATE"

// A forget deletes what it names, keeps an audit record with each record's
// SHA-256, takes the bytes off the disk, and the forgotten ID never comes
// back (D-028).
func TestForgetDeletesWithProof(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	keep := episodeAt(t0)
	gone := episodeAt(t0.Add(time.Minute))
	gone.Request.Text = "use my key " + secret + " to call the API"
	for _, ep := range []types.Episode{keep, gone} {
		if err := j.AppendEpisode(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	out := types.Outcome{ID: types.NewID(t0.Add(2 * time.Minute)), EpisodeID: gone.ID, At: t0.Add(2 * time.Minute),
		Kind: types.OutcomeSuccess, Source: types.SourceHost}
	if err := j.AppendOutcome(ctx, out); err != nil {
		t.Fatal(err)
	}
	onDisk := func() bool {
		for _, name := range []string{path, path + "-wal"} {
			if data, err := os.ReadFile(name); err == nil && bytes.Contains(data, []byte(secret)) {
				return true
			}
		}
		return false
	}
	if !onDisk() {
		t.Fatal("setup: the text should be on disk before the forget")
	}
	var stored []byte
	if err := j.db.QueryRowContext(ctx, `SELECT body FROM records WHERE id = ?`, gone.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}

	f, err := j.Forget(ctx, types.Forget{ID: types.NewID(t0.Add(time.Hour)), At: t0.Add(time.Hour), Reason: "pasted a key",
		By: "user", Targets: []types.ForgetTarget{{ID: gone.ID}, {ID: out.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(stored)
	if f.Targets[0].Kind != types.RecordEpisode || f.Targets[0].SHA256 != hex.EncodeToString(sum[:]) || f.Targets[1].Kind != types.RecordOutcome {
		t.Fatalf("audit targets: %+v", f.Targets)
	}
	if err := j.Compact(ctx); err != nil {
		t.Fatal(err)
	}

	recs := collect(t, j, types.JournalQuery{})
	if len(recs) != 2 || recs[0].Episode == nil || recs[0].Episode.ID != keep.ID || recs[1].Forget == nil || recs[1].Forget.ID != f.ID {
		t.Fatalf("after forget: %d records, %+v", len(recs), recs)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		data, err := os.ReadFile(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("%s still holds the forgotten text", filepath.Base(name))
		}
	}
	// A host re-sending the episode cannot bring it back.
	if err := j.AppendEpisode(ctx, gone); !errors.Is(err, ports.ErrForgotten) {
		t.Fatalf("re-append of a forgotten episode: err = %v", err)
	}
}

// Without a forget record naming it, nothing is deleted; a forget record is
// never deleted; and the forgotten table is append-only too.
func TestDeleteStillNeedsAForgetRecord(t *testing.T) {
	ctx := context.Background()
	j := openTemp(t)
	ep := episodeAt(t0)
	if err := j.AppendEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.ExecContext(ctx, `DELETE FROM records WHERE id = ?`, ep.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete without a forget record: err = %v", err)
	}
	if _, err := j.db.ExecContext(ctx, `INSERT INTO forgotten (id, forget_id) VALUES (?, 'nope')`, ep.ID); err == nil {
		t.Fatal("a forgotten row without its forget record was accepted")
	}
	f, err := j.Forget(ctx, types.Forget{ID: types.NewID(t0.Add(time.Hour)), At: t0.Add(time.Hour), Reason: "r", By: "user",
		Targets: []types.ForgetTarget{{ID: ep.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DELETE FROM records WHERE id = '` + string(f.ID) + `'`,
		`DELETE FROM forgotten`,
		`UPDATE forgotten SET forget_id = 'x'`,
		`UPDATE records SET body = '{}'`,
	} {
		if _, err := j.db.ExecContext(ctx, stmt); err == nil {
			t.Errorf("%s was allowed", stmt)
		}
	}
	if _, err := j.Forget(ctx, types.Forget{ID: types.NewID(t0.Add(2 * time.Hour)), At: t0.Add(2 * time.Hour), Reason: "r", By: "user",
		Targets: []types.ForgetTarget{{ID: f.ID}}}); err == nil {
		t.Fatal("a forget record was forgotten")
	}
	if _, err := j.Forget(ctx, types.Forget{ID: types.NewID(t0.Add(3 * time.Hour)), At: t0.Add(3 * time.Hour), Reason: "r", By: "user",
		Targets: []types.ForgetTarget{{ID: types.NewID(t0)}}}); !errors.Is(err, ports.ErrInvalid) {
		t.Fatalf("forget of a missing record: err = %v", err)
	}
}

// A version is a journal position: UpToSeq reads the journal as it was.
func TestTagsAndUpToSeq(t *testing.T) {
	ctx := context.Background()
	j := openTemp(t)
	for i := range 3 {
		if err := j.AppendEpisode(ctx, episodeAt(t0.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	seq, err := j.LastSeq(ctx)
	if err != nil || seq != 3 {
		t.Fatalf("last seq %d, %v", seq, err)
	}
	tag := types.Tag{ID: types.NewID(t0.Add(time.Hour)), At: t0.Add(time.Hour), Name: "v1", Seq: seq, Fingerprint: "x"}
	if err := j.AppendTag(ctx, tag); err != nil {
		t.Fatal(err)
	}
	if err := j.AppendEpisode(ctx, episodeAt(t0.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got := collect(t, j, types.JournalQuery{UpToSeq: tag.Seq}); len(got) != 3 {
		t.Fatalf("as of seq %d: %d records", tag.Seq, len(got))
	}
	tags := collect(t, j, types.JournalQuery{Kinds: []types.RecordKind{types.RecordTag}})
	if len(tags) != 1 || tags[0].Tag.Name != "v1" {
		t.Fatalf("tags: %+v", tags)
	}
}
