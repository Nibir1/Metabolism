// Package sqlite is the durable Journal: a single SQLite file, written through
// modernc.org/sqlite (pure Go, no CGO, the same driver Helix uses).
//
// Three properties matter and each is enforced here rather than hoped for:
//
//   - Durable: WAL with synchronous=FULL, so a record is on disk before
//     Append returns. The crash test kills a writer mid-stream and checks that
//     nothing it confirmed is lost.
//   - Append-only: triggers refuse UPDATE and DELETE at the database level,
//     so no code path, including a future bug, can rewrite history.
//     Forget-with-proof (Phase 4) will add a separate, audited redaction path
//     through its own migration.
//   - Private: the file and its directory are created 0600 and 0700.
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// migrations are applied in order; the journal records how many have run.
// Never edit a migration that has shipped: add a new one.
var migrations = []string{
	// 1: the record table and its append-only guarantee.
	`CREATE TABLE records (
		seq  INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT    NOT NULL,
		id   TEXT    NOT NULL UNIQUE,
		ref  TEXT,             -- outcome: its episode; event: its lesson
		at   INTEGER NOT NULL, -- Unix nanoseconds, UTC
		body BLOB    NOT NULL  -- the record as JSON
	);
	CREATE INDEX records_kind_at ON records(kind, at);
	CREATE INDEX records_ref ON records(ref);
	CREATE TRIGGER records_no_update BEFORE UPDATE ON records
		BEGIN SELECT RAISE(ABORT, 'journal is append-only'); END;
	CREATE TRIGGER records_no_delete BEFORE DELETE ON records
		BEGIN SELECT RAISE(ABORT, 'journal is append-only'); END;`,
}

// SchemaVersion is the schema this build writes.
var SchemaVersion = len(migrations)

// Journal implements ports.Journal on SQLite.
type Journal struct {
	db   *sql.DB
	path string
}

var _ ports.Journal = (*Journal)(nil)

// Open opens or creates the journal at path and brings its schema up to date.
// It refuses a journal written by a newer build rather than guessing at a
// schema it does not know.
func Open(ctx context.Context, path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("journal directory: %w", err)
	}
	// Create the file ourselves so it is 0600 from the first byte; SQLite
	// would otherwise create it with the process umask.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("journal file: %w", err)
	}
	_ = f.Close()

	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(FULL)" +
		"&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	j := &Journal{db: db, path: path}
	if err := j.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return j, nil
}

// Path is where the journal lives.
func (j *Journal) Path() string { return j.path }

// Close releases the database.
func (j *Journal) Close() error { return j.db.Close() }

func (j *Journal) migrate(ctx context.Context) error {
	if _, err := j.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("schema_version: %w", err)
	}
	var have int
	err := j.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&have)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := j.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return fmt.Errorf("schema_version: %w", err)
		}
	case err != nil:
		return fmt.Errorf("schema_version: %w", err)
	}
	if have > SchemaVersion {
		return fmt.Errorf("journal schema v%d is newer than this build understands (v%d); upgrade Metabolism",
			have, SchemaVersion)
	}
	for v := have; v < SchemaVersion; v++ {
		tx, err := j.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, v+1); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
	}
	return nil
}

// AppendEpisode journals an episode.
func (j *Journal) AppendEpisode(ctx context.Context, ep types.Episode) error {
	if err := ep.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrInvalid, err)
	}
	ep.StartedAt, ep.EndedAt = ep.StartedAt.UTC(), ep.EndedAt.UTC()
	return j.append(ctx, types.RecordEpisode, ep.ID, "", ep.StartedAt, ep, nil)
}

// AppendOutcome journals an outcome. The episode it labels must already be in
// the journal: an outcome for nothing is a host bug worth surfacing.
func (j *Journal) AppendOutcome(ctx context.Context, o types.Outcome) error {
	if err := o.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrInvalid, err)
	}
	o.At = o.At.UTC()
	return j.append(ctx, types.RecordOutcome, o.ID, o.EpisodeID, o.At, o, func(tx *sql.Tx) error {
		var one int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM records WHERE kind = ? AND id = ?`, types.RecordEpisode, o.EpisodeID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ports.ErrUnknownEpisode, o.EpisodeID)
		}
		return err
	})
}

// AppendEvent journals a lesson lifecycle event.
func (j *Journal) AppendEvent(ctx context.Context, ev types.LifecycleEvent) error {
	if err := ev.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrInvalid, err)
	}
	ev.At = ev.At.UTC()
	return j.append(ctx, types.RecordEvent, ev.ID, ev.LessonID, ev.At, ev, nil)
}

// append writes one record in a transaction. Re-appending an identical record
// is a no-op, because hosts retry and a retry must not fail; a different
// record under the same ID is a conflict.
func (j *Journal) append(ctx context.Context, kind types.RecordKind, id, ref types.ID,
	at time.Time, v any, check func(*sql.Tx) error) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", kind, err)
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if check != nil {
		if err := check(tx); err != nil {
			return err
		}
	}
	var refArg any
	if ref != "" {
		refArg = string(ref)
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO records (kind, id, ref, at, body) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		kind, id, refArg, at.UnixNano(), body)
	if err != nil {
		return fmt.Errorf("append %s: %w", kind, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var existing []byte
		var existingKind string
		if err := tx.QueryRowContext(ctx, `SELECT kind, body FROM records WHERE id = ?`, id).
			Scan(&existingKind, &existing); err != nil {
			return err
		}
		if existingKind != string(kind) || !bytes.Equal(existing, body) {
			return fmt.Errorf("%w: %s", ports.ErrConflict, id)
		}
		return nil
	}
	return tx.Commit()
}

// Scan yields matching records in Seq order.
func (j *Journal) Scan(ctx context.Context, q types.JournalQuery) iter.Seq2[types.Record, error] {
	return func(yield func(types.Record, error) bool) {
		query, args := buildQuery(q)
		rows, err := j.db.QueryContext(ctx, query, args...)
		if err != nil {
			yield(types.Record{}, fmt.Errorf("scan journal: %w", err))
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var (
				r    types.Record
				kind string
				at   int64
				body []byte
			)
			if err := rows.Scan(&r.Seq, &kind, &at, &body); err != nil {
				yield(types.Record{}, err)
				return
			}
			r.Kind, r.At = types.RecordKind(kind), time.Unix(0, at).UTC()
			if err := decode(&r, body); err != nil {
				if !yield(types.Record{}, fmt.Errorf("record seq %d: %w", r.Seq, err)) {
					return
				}
				continue
			}
			if !yield(r, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(types.Record{}, err)
		}
	}
}

func buildQuery(q types.JournalQuery) (string, []any) {
	var (
		where []string
		args  []any
	)
	if len(q.Kinds) > 0 {
		marks := make([]string, len(q.Kinds))
		for i, k := range q.Kinds {
			marks[i] = "?"
			args = append(args, string(k))
		}
		where = append(where, "kind IN ("+strings.Join(marks, ",")+")")
	}
	if q.AfterSeq > 0 {
		where = append(where, "seq > ?")
		args = append(args, q.AfterSeq)
	}
	if !q.Since.IsZero() {
		where = append(where, "at >= ?")
		args = append(args, q.Since.UnixNano())
	}
	if !q.Until.IsZero() {
		where = append(where, "at < ?")
		args = append(args, q.Until.UnixNano())
	}
	if q.EpisodeID != "" {
		where = append(where, "(id = ? OR (kind = ? AND ref = ?))")
		args = append(args, string(q.EpisodeID), string(types.RecordOutcome), string(q.EpisodeID))
	}
	query := "SELECT seq, kind, at, body FROM records"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY seq"
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}
	return query, args
}

func decode(r *types.Record, body []byte) error {
	switch r.Kind {
	case types.RecordEpisode:
		r.Episode = new(types.Episode)
		return json.Unmarshal(body, r.Episode)
	case types.RecordOutcome:
		r.Outcome = new(types.Outcome)
		return json.Unmarshal(body, r.Outcome)
	case types.RecordEvent:
		r.Event = new(types.LifecycleEvent)
		return json.Unmarshal(body, r.Event)
	}
	return fmt.Errorf("unknown record kind %q", r.Kind)
}

// IntegrityCheck runs SQLite's own consistency check. It returns nil only for
// a journal SQLite considers sound.
func (j *Journal) IntegrityCheck(ctx context.Context) error {
	var res string
	if err := j.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity check: %s", res)
	}
	return nil
}
