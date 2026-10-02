// Package ndjson ingests episodes that a host recorded as NDJSON, the
// file-based contract that keeps a host's build independent of the engine.
//
// Helix writes version 2 of the format (Helix: internal/metabolism/wire.go);
// files recorded before Phase 3 hold version 1, which is still read.
// Version 2 adds an episode's withheld lessons and the feedback record.
// testdata/helix_wire_v1.ndjson and helix_wire_v2.ndjson are byte-for-byte
// copies of Helix's golden files; the tests here decode them into the
// engine's types with unknown fields refused, so a format drift on either
// side fails a test instead of silently dropping data.
package ndjson

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// WireVersion is the newest host format version this build reads. Every
// version from 1 up is read.
const WireVersion = 2

// maxProblems bounds how many problems Stats keeps verbatim.
const maxProblems = 10

// envelope is one NDJSON line.
type envelope struct {
	V        int             `json:"v"`
	Kind     string          `json:"kind"`
	Episode  json.RawMessage `json:"episode,omitempty"`
	Outcome  json.RawMessage `json:"outcome,omitempty"`
	Feedback json.RawMessage `json:"feedback,omitempty"`
}

// Feedback is what the user told the host about a lesson (wire v2). The
// only action is "forget": the user eliminated the lesson, with a reason.
// Ingest does not apply it, because lessons change only through the
// metabolome (D-014); it returns it in Stats for the caller to apply.
type Feedback struct {
	ID       types.ID  `json:"id"`
	LessonID types.ID  `json:"lesson_id"`
	At       time.Time `json:"at"`
	Action   string    `json:"action"`
	Reason   string    `json:"reason"`
}

// FeedbackForget is the action that eliminates a lesson.
const FeedbackForget = "forget"

// Stats says what an ingest did. Episodes and Outcomes count records that are
// now in the journal, whether this run appended them or an earlier one did:
// ingest is idempotent, so running it twice is safe.
type Stats struct {
	Files     []string `json:"files"`
	Lines     int      `json:"lines"`
	Episodes  int      `json:"episodes"`
	Outcomes  int      `json:"outcomes"`
	Orphans   int      `json:"orphans"`   // outcomes whose episode was never ingested (rotated out first)
	Invalid   int      `json:"invalid"`   // well-formed records that failed validation or conflicted
	Malformed int      `json:"malformed"` // lines that are not a v1 record (e.g. a line cut off by a crash)
	Problems  []string `json:"problems,omitempty"`
	// Feedback lists every feedback record read, in file order, for the
	// caller to apply. Applying it twice is harmless: an eliminated lesson
	// stays eliminated.
	Feedback []Feedback `json:"feedback,omitempty"`
}

func (s *Stats) problem(file string, line int, err error) {
	if len(s.Problems) < maxProblems {
		s.Problems = append(s.Problems, fmt.Sprintf("%s:%d: %v", filepath.Base(file), line, err))
	}
}

// Generations returns path's rotated generations and path itself, oldest
// first: path.3, path.2, path.1, path. Only files that exist are returned.
func Generations(path string) ([]string, error) {
	matches, err := filepath.Glob(path + ".*")
	if err != nil {
		return nil, err
	}
	type gen struct {
		n    int
		path string
	}
	var gens []gen
	for _, m := range matches {
		n, err := strconv.Atoi(strings.TrimPrefix(m, path+"."))
		if err == nil && n > 0 {
			gens = append(gens, gen{n, m})
		}
	}
	slices.SortFunc(gens, func(a, b gen) int { return b.n - a.n })
	var out []string
	for _, g := range gens {
		out = append(out, g.path)
	}
	if _, err := os.Stat(path); err == nil {
		out = append(out, path)
	}
	return out, nil
}

// Ingest appends every record in path and its rotated generations to j.
// Record-level problems are counted and reported, never fatal: one bad line
// must not cost the rest of the baseline. Only an unreadable file or a
// journal failure stops it.
func Ingest(ctx context.Context, j ports.Journal, path string) (Stats, error) {
	files, err := Generations(path)
	if err != nil {
		return Stats{}, err
	}
	if len(files) == 0 {
		return Stats{}, fmt.Errorf("no episode file at %s", path)
	}
	st := Stats{Files: files}
	for _, f := range files {
		if err := ingestFile(ctx, j, f, &st); err != nil {
			return st, err
		}
	}
	return st, nil
}

func ingestFile(ctx context.Context, j ports.Journal, path string, st *Stats) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		st.Lines++
		if err := ingestLine(ctx, j, line, st); err != nil {
			switch {
			case errors.Is(err, ports.ErrUnknownEpisode):
				st.Orphans++
			case errors.Is(err, ports.ErrInvalid), errors.Is(err, ports.ErrConflict):
				st.Invalid++
				st.problem(path, n, err)
			case errors.Is(err, errMalformed):
				st.Malformed++
				st.problem(path, n, err)
			default:
				return fmt.Errorf("%s:%d: %w", filepath.Base(path), n, err)
			}
		}
	}
	return sc.Err()
}

var errMalformed = errors.New("malformed record")

func ingestLine(ctx context.Context, j ports.Journal, line []byte, st *Stats) error {
	var env envelope
	if err := strictDecode(line, &env); err != nil {
		return fmt.Errorf("%w: %v", errMalformed, err)
	}
	if env.V < 1 || env.V > WireVersion {
		return fmt.Errorf("%w: wire version %d, this build reads 1 to %d", errMalformed, env.V, WireVersion)
	}
	switch env.Kind {
	case "episode":
		var ep types.Episode
		if err := strictDecode(env.Episode, &ep); err != nil {
			return fmt.Errorf("%w: episode: %v", errMalformed, err)
		}
		if env.V < 2 && len(ep.Withheld) > 0 {
			return fmt.Errorf("%w: withheld lessons in a v1 record", errMalformed)
		}
		if err := j.AppendEpisode(ctx, ep); err != nil {
			return err
		}
		st.Episodes++
	case "outcome":
		var o types.Outcome
		if err := strictDecode(env.Outcome, &o); err != nil {
			return fmt.Errorf("%w: outcome: %v", errMalformed, err)
		}
		if err := j.AppendOutcome(ctx, o); err != nil {
			return err
		}
		st.Outcomes++
	case "feedback":
		if env.V < 2 {
			return fmt.Errorf("%w: feedback in a v1 record", errMalformed)
		}
		var fb Feedback
		if err := strictDecode(env.Feedback, &fb); err != nil {
			return fmt.Errorf("%w: feedback: %v", errMalformed, err)
		}
		if !fb.ID.Valid() || !fb.LessonID.Valid() || fb.At.IsZero() || fb.Action != FeedbackForget || strings.TrimSpace(fb.Reason) == "" {
			return fmt.Errorf("%w: feedback %+v", ports.ErrInvalid, fb)
		}
		st.Feedback = append(st.Feedback, fb)
	default:
		return fmt.Errorf("%w: unknown kind %q", errMalformed, env.Kind)
	}
	return nil
}

// strictDecode refuses unknown fields: a field the engine does not know is a
// field it would silently drop, which is how a format drifts unnoticed.
func strictDecode(data []byte, v any) error {
	if len(data) == 0 {
		return errors.New("empty")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
