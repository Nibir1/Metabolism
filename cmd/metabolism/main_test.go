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
	if !strings.Contains(out.String(), "episodes 120") || !strings.Contains(out.String(), "last 3 episodes") {
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
