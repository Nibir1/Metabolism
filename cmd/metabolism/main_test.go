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
)

func TestSimThenCurvesThenInspect(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.db")
	var out bytes.Buffer

	if err := run(ctx, []string{"sim", "-journal", journal, "-count", "120", "-days", "21"}, &out); err != nil {
		t.Fatalf("sim: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := run(ctx, []string{"curves", "-journal", journal, "-window", "7d", "-out", filepath.Join(dir, "res")}, &out); err != nil {
		t.Fatalf("curves: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "total") || !strings.Contains(out.String(), "120") {
		t.Errorf("curves output missing the total row:\n%s", out.String())
	}
	for _, f := range []string{"curve.json", "curve.csv"} {
		if fi, err := os.Stat(filepath.Join(dir, "res", f)); err != nil || fi.Size() == 0 {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	out.Reset()
	if err := run(ctx, []string{"inspect", "-journal", journal, "-n", "3"}, &out); err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !strings.Contains(out.String(), "episodes 120") || !strings.Contains(out.String(), "last 3 matching episodes") {
		t.Errorf("inspect output:\n%s", out.String())
	}
}

func TestIngestHelixGolden(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "j.db")
	var out bytes.Buffer
	golden := filepath.Join("..", "..", "adapters", "ndjson", "testdata", "helix_wire_v1.ndjson")
	if err := run(context.Background(), []string{"ingest", "-journal", journal, "-from", golden}, &out); err != nil {
		t.Fatalf("ingest: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "episodes 1 · outcomes 2") {
		t.Fatalf("ingest output:\n%s", out.String())
	}
}

func TestRecallInspectAndEval(t *testing.T) {
	ctx := context.Background()
	journal := filepath.Join(t.TempDir(), "j.db")
	var out bytes.Buffer
	if err := run(ctx, []string{"sim", "-journal", journal, "-count", "200"}, &out); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := run(ctx, []string{"recall", "-journal", journal, "-q", "commit my changes", "-k", "2"}, &out); err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(out.String(), "2 case(s)") || !strings.Contains(out.String(), "commit") {
		t.Fatalf("recall output:\n%s", out.String())
	}
	// The last column of a case row is its id; inspect it in full.
	var id string
	for _, line := range strings.Split(out.String(), "\n") {
		if f := strings.Fields(line); len(f) > 0 && len(f[len(f)-1]) == 28 {
			id = f[len(f)-1]
			break
		}
	}
	out.Reset()
	if err := run(ctx, []string{"inspect", "-journal", journal, "-id", id}, &out); err != nil {
		t.Fatalf("inspect -id %q: %v", id, err)
	}
	for _, want := range []string{"episode " + id, "steps:", "outcomes:", "undone"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("inspect -id output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := run(ctx, []string{"inspect", "-journal", journal, "-end", "failed", "-n", "50"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, " steps ") && !strings.Contains(line, " failed ") {
			t.Errorf("-end failed listed %q", line)
		}
	}
	if err := run(ctx, []string{"inspect", "-journal", journal, "-end", "sort-of"}, &out); err == nil {
		t.Error("a bad -end was accepted")
	}

	out.Reset()
	if err := run(ctx, []string{"recall-eval", "-journal", journal, "-label", "scenario", "-out", filepath.Join(t.TempDir(), "r")}, &out); err != nil {
		t.Fatalf("recall-eval: %v", err)
	}
	if !strings.Contains(out.String(), "brier") || !strings.Contains(out.String(), "same-task precision") {
		t.Fatalf("recall-eval output:\n%s", out.String())
	}
	if err := run(ctx, []string{"recall", "-journal", journal}, &out); err == nil {
		t.Error("recall without -q was accepted")
	}
}

func TestDoctor(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.db")
	cfg := filepath.Join(dir, "config.json")
	golden := filepath.Join("..", "..", "adapters", "ndjson", "testdata", "helix_wire_v1.ndjson")
	doctor := func() string {
		t.Helper()
		var out bytes.Buffer
		if err := run(ctx, []string{"doctor", "-journal", journal, "-from", golden, "-helix-config", cfg}, &out); err != nil {
			t.Fatalf("doctor: %v", err)
		}
		return out.String()
	}

	// Nothing set up: preference unreadable, no journal. Doctor must not create one.
	got := doctor()
	for _, want := range []string{"no Helix config at", "1 file(s), 3 records (1 episodes, 2 outcomes)", "none yet at", "make helix-ingest"} {
		if !strings.Contains(got, want) {
			t.Errorf("before ingest, doctor output lacks %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("doctor created a journal")
	}

	if err := os.WriteFile(cfg, []byte(`{"user_preferences":{"metabolism_record":true,"other":"ignored"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"ingest", "-journal", journal, "-from", golden}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got = doctor()
	for _, want := range []string{"metabolism_record is on", "all 1 recorded episodes are in the journal", "helix 1", "100.0% of episodes", "make helix-report"} {
		if !strings.Contains(got, want) {
			t.Errorf("after ingest, doctor output lacks %q:\n%s", want, got)
		}
	}
}

// Uniform labels cannot measure a judge; -redo keeps the old ones.
func TestLabelScoreRefusesUniformLabelsAndRedoKeepsThem(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.db")
	saved := filepath.Join(dir, "c.ndjson")
	for _, args := range [][]string{
		{"sim", "-journal", journal, "-count", "300"},
		{"digest", "-journal", journal, "-reflector", "sim"},
		{"verify", "-journal", journal, "-save", saved},
	} {
		if err := run(ctx, args, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	comps, _ := readNDJSON[comparisonRecord](saved)
	if len(comps) < 10 {
		t.Skipf("only %d comparisons saved", len(comps))
	}
	if err := cmdLabel(ctx, []string{"-from", saved}, &bytes.Buffer{}, strings.NewReader(strings.Repeat("a\n", len(comps)))); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdLabel(ctx, []string{"-from", saved, "-score"}, &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cannot measure a judge") || strings.Contains(out.String(), "agrees on") {
		t.Fatalf("uniform labels were scored:\n%s", out.String())
	}
	out.Reset()
	if err := cmdLabel(ctx, []string{"-from", saved, "-redo"}, &out, strings.NewReader("q\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "earlier labels kept in") || !strings.Contains(out.String(), fmt.Sprintf("%d to label", len(comps))) {
		t.Fatalf("redo output:\n%s", out.String())
	}
	kept, _ := filepath.Glob(saved + ".labels.*")
	if len(kept) != 1 {
		t.Fatalf("old labels not kept: %v", kept)
	}
}

func TestParseScope(t *testing.T) {
	for _, ok := range []string{"", "global", "machine:m-1", "project:helix-ab12"} {
		if _, err := parseScope(ok); err != nil {
			t.Errorf("parseScope(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"project", "global:x", "planet:earth"} {
		if _, err := parseScope(bad); err == nil {
			t.Errorf("parseScope(%q) accepted", bad)
		}
	}
}

func TestInspectDoesNotCreateAJournal(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	if err := run(context.Background(), []string{"inspect", "-journal", missing}, &bytes.Buffer{}); err == nil {
		t.Fatal("inspect of a missing journal succeeded")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("inspect created a journal")
	}
}

func TestParseWindow(t *testing.T) {
	for in, want := range map[string]time.Duration{"7d": 7 * 24 * time.Hour, "12h": 12 * time.Hour} {
		got, err := parseWindow(in)
		if err != nil || got != want {
			t.Errorf("parseWindow(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"0d", "-1d", "xd", "0s", "soon"} {
		if _, err := parseWindow(bad); err == nil {
			t.Errorf("parseWindow(%q) accepted", bad)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	if err := run(context.Background(), []string{"digest"}, &bytes.Buffer{}); err == nil {
		t.Fatal("unknown command accepted")
	}
}
