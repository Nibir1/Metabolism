package main

import (
	"bytes"
	"context"
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
