package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const userConfig = `{
  "provider": "deepseek",
  "user_preferences": {"typing_effect": true},
  "speech": {"wake_word": {"phrase": "hey helix"}}
}
`

func newSetup(t *testing.T, withLessons bool) *helixSetup {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".helix")
	_ = os.MkdirAll(filepath.Join(home, "metabolism"), 0o700)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if withLessons {
		_ = os.WriteFile(filepath.Join(home, "metabolism", "lessons.json"), []byte("the user's own"), 0o600)
	}
	return &helixSetup{home: home, state: filepath.Join(t.TempDir(), "state")}
}

// The user's Helix setup comes back byte for byte, whatever the run did.
func TestHelixSetupAppliesAndRestoresExactly(t *testing.T) {
	for _, withLessons := range []bool{true, false} {
		h := newSetup(t, withLessons)
		delivery := filepath.Join(t.TempDir(), "lessons.json")
		_ = os.WriteFile(delivery, []byte("today's lessons"), 0o600)
		if err := h.apply(true, true, delivery); err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		data, _ := os.ReadFile(h.configPath())
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		prefs := doc["user_preferences"].(map[string]any)
		wake := doc["speech"].(map[string]any)["wake_word"].(map[string]any)
		if prefs["metabolism_record"] != true || prefs["metabolism_lessons"] != true || wake["enabled"] != false ||
			prefs["metabolism_thinking_flip"] != true ||
			prefs["typing_effect"] != true || wake["phrase"] != "hey helix" || doc["provider"] != "deepseek" {
			t.Fatalf("applied config: %s", data)
		}
		if l, _ := os.ReadFile(h.lessonsPath()); string(l) != "today's lessons" {
			t.Fatalf("delivery file: %q", l)
		}

		if err := h.restore(); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(h.configPath()); string(data) != userConfig {
			t.Fatalf("config not restored exactly:\n%s", data)
		}
		l, err := os.ReadFile(h.lessonsPath())
		switch {
		case withLessons && string(l) != "the user's own":
			t.Fatalf("the user's delivery file was not restored: %q", l)
		case !withLessons && err == nil:
			t.Fatal("a delivery file the user did not have was left behind")
		}
		if entries, _ := os.ReadDir(h.state); len(entries) != 0 {
			t.Fatalf("backups left after restore: %v", entries)
		}
	}
}

// A run killed between apply and restore is put right by the next run.
func TestHelixSetupRecoversAKilledRun(t *testing.T) {
	h := newSetup(t, true)
	if ok, err := h.recover(); ok || err != nil {
		t.Fatalf("nothing to recover: %v %v", ok, err)
	}
	if err := h.apply(false, false, ""); err != nil {
		t.Fatal(err)
	}
	// ...the process dies here...
	again := &helixSetup{home: h.home, state: h.state}
	if ok, err := again.recover(); !ok || err != nil {
		t.Fatalf("recover: %v %v", ok, err)
	}
	if data, _ := os.ReadFile(h.configPath()); string(data) != userConfig {
		t.Fatalf("config after recovery:\n%s", data)
	}
}

// Without -thinking-flip the run leaves no flip preference behind, even one
// a config somehow already had.
func TestHelixSetupWithoutThinkingFlip(t *testing.T) {
	h := newSetup(t, false)
	if err := h.apply(false, false, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(h.configPath())
	if strings.Contains(string(data), "metabolism_thinking_flip") {
		t.Fatalf("flip preference set without -thinking-flip:\n%s", data)
	}
	if err := h.restore(); err != nil {
		t.Fatal(err)
	}
}

// The run refuses to share Helix with anything the user is doing.
func TestPreflightRefusals(t *testing.T) {
	ctx := context.Background()
	h := newSetup(t, false)
	missing := filepath.Join(t.TempDir(), "no-helix") // status fails: no daemon
	if err := preflight(ctx, h.home, missing, false); err != nil {
		t.Fatalf("a quiet Helix was refused: %v", err)
	}
	lock := fmt.Sprintf(`{"kind":"tty","pid":1,"ts":%d}`, time.Now().Unix())
	_ = os.WriteFile(filepath.Join(h.home, "active.lock"), []byte(lock), 0o600)
	if err := preflight(ctx, h.home, missing, false); err == nil || !strings.Contains(err.Error(), "interactive") {
		t.Fatalf("fresh session lock: %v", err)
	}
	_ = os.WriteFile(filepath.Join(h.home, "active.lock"), []byte(`{"kind":"tty","pid":1,"ts":1}`), 0o600)
	_ = os.WriteFile(filepath.Join(h.home, "metabolism", "episodes.ndjson"), []byte("{}\n"), 0o600)
	if err := preflight(ctx, h.home, missing, false); err == nil || !strings.Contains(err.Error(), "recording") {
		t.Fatalf("the user's own recording: %v", err)
	}
	if err := preflight(ctx, h.home, missing, true); err != nil {
		t.Fatalf("with -keep-own-recording the user's recording is set aside, not refused: %v", err)
	}
}

// The user's own recording is set aside for the run and comes back
// untouched; the run's recording never mixes with it.
func TestOwnRecordingIsSetAsideAndPutBack(t *testing.T) {
	h := newSetup(t, false)
	own := filepath.Join(h.home, "metabolism", "episodes.ndjson")
	_ = os.WriteFile(own, []byte("the user's turn\n"), 0o600)
	_ = os.WriteFile(own+".1", []byte("an older generation\n"), 0o600)
	stashed, err := h.stashRecording()
	if err != nil || !stashed {
		t.Fatalf("stash: %v %v", stashed, err)
	}
	if _, err := os.Stat(own); err == nil {
		t.Fatal("the user's recording is still where the run will record")
	}
	_ = os.WriteFile(own, []byte("the run's turn\n"), 0o600) // the run records
	if err := moveRecording(filepath.Join(h.home, "metabolism"), filepath.Join(t.TempDir(), "day")); err != nil {
		t.Fatal(err)
	}
	if err := h.unstashRecording(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{own: "the user's turn\n", own + ".1": "an older generation\n"} {
		if got, _ := os.ReadFile(path); string(got) != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
	if _, err := os.Stat(h.stashDir()); err == nil {
		t.Fatal("stash directory left behind")
	}
}

// A run killed with the user's recording set aside is put right by the
// next: the interrupted run's recording is kept apart, the user's restored.
func TestOwnRecordingRecoveredAfterAKilledRun(t *testing.T) {
	h := newSetup(t, false)
	own := filepath.Join(h.home, "metabolism", "episodes.ndjson")
	_ = os.WriteFile(own, []byte("the user's turn\n"), 0o600)
	if _, err := h.stashRecording(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(own, []byte("the killed run's turn\n"), 0o600)
	// ...the process dies here...
	kept, err := h.recoverRecording()
	if err != nil || kept == "" {
		t.Fatalf("recover: %q %v", kept, err)
	}
	if got, _ := os.ReadFile(own); string(got) != "the user's turn\n" {
		t.Fatalf("own recording = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(kept, "episodes.ndjson")); string(got) != "the killed run's turn\n" {
		t.Fatalf("interrupted run's recording = %q", got)
	}
	if again, err := h.recoverRecording(); again != "" || err != nil {
		t.Fatalf("nothing left to recover: %q %v", again, err)
	}
}

// The synthetic user works in a clone, never in the real checkout, and a new
// run replaces the clone (yesterday's edits are gone).
func TestCloneProjectUsesAFreshClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	home := t.TempDir()
	src := filepath.Join(home, "Dev", "proj")
	_ = os.MkdirAll(src, 0o755)
	_ = os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	ws := filepath.Join(home, "runs", "workspace")
	rel, err := cloneProject(context.Background(), home, "Dev/proj", ws)
	if err != nil || rel != "runs/workspace/proj" {
		t.Fatalf("clone: %q %v", rel, err)
	}
	_ = os.WriteFile(filepath.Join(ws, "proj", "scratch.txt"), []byte("an edit"), 0o644)
	if _, err := cloneProject(context.Background(), home, "Dev/proj", ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "proj", "scratch.txt")); !os.IsNotExist(err) {
		t.Fatal("yesterday's edit survived the new clone")
	}
	if _, err := os.Stat(filepath.Join(src, "scratch.txt")); !os.IsNotExist(err) {
		t.Fatal("the real checkout was touched")
	}
	if _, err := cloneProject(context.Background(), home, "Dev/proj", filepath.Join(t.TempDir(), "elsewhere")); err == nil {
		t.Fatal("a workspace outside home was accepted (the daemon resolves requests from home)")
	}
}

func TestAutoNeedsRemoteOptIn(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"auto", "-dir", t.TempDir(), "-helix-home", t.TempDir()}, &out)
	if err == nil || !strings.Contains(err.Error(), "-allow-remote") {
		t.Fatalf("auto without opt-in: %v", err)
	}
}

// A day interrupted during verify, from before day.json existed, is
// reconstructed from its folder: ingest and digest done, verify and credit
// still to run.
func TestLoadDayMetaReconstructsAnInterruptedDay(t *testing.T) {
	dayDir := filepath.Join(t.TempDir(), "2026-10-03T115702")
	_ = os.MkdirAll(dayDir, 0o700)
	_ = os.WriteFile(filepath.Join(dayDir, "drive.ndjson"), []byte("{}\n{}\n{}\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dayDir, "log.txt"), []byte("auto run … · baseline (lessons off) · 14 episodes\n"+
		"━━ metabolism ingest\nok\n━━ metabolism digest\nok\n━━ metabolism verify\ncontext canceled\n"), 0o600)
	m, err := loadDayMeta(dayDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Sent != 3 || m.Phase != "baseline (lessons off)" || strings.Join(m.Done, ",") != "ingest,digest" ||
		m.Started.Format("15:04:05") != "11:57:02" {
		t.Fatalf("meta %+v", m)
	}
	// A day with day.json is read as written.
	want := dayMeta{Started: m.Started, Phase: "lessons (delivered by coin flip)", Sent: 90, Done: []string{"ingest"}}
	if err := want.save(dayDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := loadDayMeta(dayDir); got.Sent != 90 || got.Phase != want.Phase || len(got.Done) != 1 {
		t.Fatalf("day.json read as %+v", got)
	}
	if _, err := loadDayMeta(t.TempDir()); err == nil {
		t.Fatal("a folder that is not a day was accepted")
	}
}
