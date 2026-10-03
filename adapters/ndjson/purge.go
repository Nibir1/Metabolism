package ndjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Nibir1/metabolism/types"
)

// PurgeStats says what Purge removed from a host's recording.
type PurgeStats struct {
	Files   []string `json:"files"`
	Removed int      `json:"removed_lines"`
}

// Purge removes from path and its rotated generations every line about a
// forgotten record (D-028): an episode or outcome whose ID is in ids, an
// outcome of a forgotten episode, feedback about a forgotten lesson, and
// any line containing match (when set). The host keeps appending while this
// runs, so each file is rewritten to a temporary file and renamed over the
// original only if the original did not grow meanwhile; otherwise it is
// read again. Lines it cannot parse are kept unless they contain match.
func Purge(path string, ids map[types.ID]bool, match string) (PurgeStats, error) {
	files, err := Generations(path)
	if err != nil {
		return PurgeStats{}, err
	}
	var st PurgeStats
	for _, f := range files {
		n, err := purgeFile(f, ids, match)
		if err != nil {
			return st, err
		}
		if n > 0 {
			st.Files = append(st.Files, f)
			st.Removed += n
		}
	}
	return st, nil
}

// Mentions reports whether path or its generations still hold a line about
// any of ids: what forget -verify checks.
func Mentions(path string, ids map[types.ID]bool) ([]string, error) {
	files, err := Generations(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			if forgottenLine(line, ids, "") {
				out = append(out, f)
				break
			}
		}
	}
	return out, nil
}

func purgeFile(path string, ids map[types.ID]bool, match string) (int, error) {
	for attempt := 0; attempt < 5; attempt++ {
		before, err := os.Stat(path)
		if err != nil {
			return 0, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		var kept bytes.Buffer
		removed := 0
		for _, line := range bytes.SplitAfter(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 && forgottenLine(line, ids, match) {
				removed++
				continue
			}
			kept.Write(line)
		}
		if removed == 0 {
			return 0, nil
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".purge-*.ndjson")
		if err != nil {
			return 0, err
		}
		cleanup := func() { _ = os.Remove(tmp.Name()) }
		if _, err := tmp.Write(kept.Bytes()); err != nil {
			_ = tmp.Close()
			cleanup()
			return 0, err
		}
		if err := tmp.Chmod(before.Mode().Perm()); err != nil {
			_ = tmp.Close()
			cleanup()
			return 0, err
		}
		if err := tmp.Close(); err != nil {
			cleanup()
			return 0, err
		}
		after, err := os.Stat(path)
		if err != nil {
			cleanup()
			return 0, err
		}
		if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			cleanup() // the host appended meanwhile: read it again
			continue
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			cleanup()
			return 0, err
		}
		return removed, nil
	}
	return 0, fmt.Errorf("%s kept changing while it was purged; stop the host and run forget again", path)
}

// forgottenLine reports whether a recording line is about a forgotten
// record, or contains match.
func forgottenLine(line []byte, ids map[types.ID]bool, match string) bool {
	if match != "" && (bytes.Contains(line, []byte(match)) || bytes.Contains(line, []byte(jsonEscaped(match)))) {
		return true
	}
	var env struct {
		Episode *struct {
			ID types.ID `json:"id"`
		} `json:"episode"`
		Outcome *struct {
			ID        types.ID `json:"id"`
			EpisodeID types.ID `json:"episode_id"`
		} `json:"outcome"`
		Feedback *struct {
			ID       types.ID `json:"id"`
			LessonID types.ID `json:"lesson_id"`
		} `json:"feedback"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &env) != nil {
		return false
	}
	switch {
	case env.Episode != nil:
		return ids[env.Episode.ID]
	case env.Outcome != nil:
		return ids[env.Outcome.ID] || ids[env.Outcome.EpisodeID]
	case env.Feedback != nil:
		return ids[env.Feedback.ID] || ids[env.Feedback.LessonID]
	}
	return false
}

func jsonEscaped(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
