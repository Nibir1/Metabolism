package ndjson

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/types"
)

// Forgetting an episode removes it and its outcome from every generation of
// the recording, and only them; Mentions then finds nothing (D-028).
func TestPurgeRemovesForgottenLines(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "helix_wire_v2.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "episodes.ndjson")
	other := `{"v":2,"kind":"note","text":"my token is sk-secret-42"}` + "\n"
	if err := os.WriteFile(path, golden, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", append(append([]byte{}, golden...), other...), 0o600); err != nil {
		t.Fatal(err)
	}
	episode := types.ID("0199a1b2c3d4e5f6a7b8c9d0e1f5")
	ids := map[types.ID]bool{episode: true}
	if got, err := Mentions(path, ids); err != nil || len(got) != 2 {
		t.Fatalf("before: mentions %v, %v", got, err)
	}
	st, err := Purge(path, ids, "sk-secret-42")
	if err != nil {
		t.Fatal(err)
	}
	// Each file loses the episode and its outcome; the older one also the
	// line holding the text.
	if st.Removed != 5 || len(st.Files) != 2 {
		t.Fatalf("purge: %+v", st)
	}
	for _, f := range []string{path, path + ".1"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), string(episode)) || strings.Contains(string(data), "sk-secret-42") {
			t.Errorf("%s still mentions what was forgotten", f)
		}
		if !strings.Contains(string(data), `"kind":"feedback"`) {
			t.Errorf("%s lost an unrelated line", f)
		}
		if info, _ := os.Stat(f); info.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v, want 0600", f, info.Mode().Perm())
		}
	}
	if got, err := Mentions(path, ids); err != nil || len(got) != 0 {
		t.Fatalf("after: mentions %v, %v", got, err)
	}
}
