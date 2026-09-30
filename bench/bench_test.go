package bench

import (
	"bytes"
	"context"
	"encoding/csv"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC) // a Monday

func ep(at time.Time, end types.RunEnd, calls, chars int, steps ...bool) types.Episode {
	e := types.Episode{
		ID: types.NewID(at), Host: "t", Scope: types.Scope{Level: types.ScopeGlobal},
		StartedAt: at, EndedAt: at, Request: types.Request{Provenance: types.ProvUserTyped},
		Usage: types.Usage{ModelCalls: calls, InputChars: chars}, End: end,
	}
	for _, ok := range steps {
		e.Steps = append(e.Steps, types.Step{Tool: "shell", OK: ok})
	}
	return e
}

func outcome(e types.Episode, k types.OutcomeKind) types.Outcome {
	return types.Outcome{ID: types.NewID(e.StartedAt), EpisodeID: e.ID, At: e.StartedAt, Kind: k, Source: types.SourceDerived}
}

func TestComputeExactNumbers(t *testing.T) {
	a := ep(t0, types.RunDone, 1, 400, true)                          // finished
	b := ep(t0, types.RunDone, 2, 800, true, true)                    // done, but undone later
	c := ep(t0, types.RunBudgetExhausted, 5, 2000, true, false, true) // exhausted
	d := ep(t0, types.RunFailed, 2, 800, false)                       // failed
	outs := map[types.ID][]types.Outcome{
		b.ID: {outcome(b, types.OutcomeSuccess), outcome(b, types.OutcomeUndone)},
		d.ID: {outcome(d, types.OutcomeDeclined), outcome(d, types.OutcomeRepeated)},
	}
	m := Compute([]types.Episode{a, b, c, d}, outs)

	check := func(name string, got, want float64) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if m.Episodes != 4 || m.Steps != 7 || m.Finished != 1 {
		t.Fatalf("counts: %+v", m)
	}
	check("finish rate", m.FinishRate, 0.25)
	check("outcome coverage", m.OutcomeCoverage, 0.5)
	// All ten calls are paid for by the one finished episode.
	check("calls per finished", m.ModelCallsPerFinished, 10)
	check("tokens per finished", m.EstTokensPerFinished, 4000.0/CharsPerToken)
	check("budget exhaustion", m.BudgetExhaustionRate, 0.25)
	check("failed steps", m.FailedStepRate, 2.0/7)
	check("undo", m.UndoRate, 0.25)
	check("decline", m.DeclineRate, 0.25)
	check("repeat", m.RepeatRate, 0.25)
}

func TestComputeEmptyIsZeroNotNaN(t *testing.T) {
	m := Compute(nil, nil)
	if m.FinishRate != 0 || m.ModelCallsPerFinished != 0 || m.FailedStepRate != 0 {
		t.Fatalf("empty metrics should be zero: %+v", m)
	}
}

func TestCurveKeepsEmptyWindows(t *testing.T) {
	day := 24 * time.Hour
	ds := Dataset{
		Episodes: []types.Episode{
			ep(t0.Add(15*day), types.RunDone, 1, 0), // out of order on purpose
			ep(t0, types.RunDone, 1, 0),
			ep(t0.Add(day), types.RunFailed, 1, 0),
		},
		Outcomes: map[types.ID][]types.Outcome{},
	}
	c, err := BuildCurve("t", ds, 7*day)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Points) != 3 {
		t.Fatalf("got %d windows, want 3 (week 2 empty)", len(c.Points))
	}
	got := []int{c.Points[0].Metrics.Episodes, c.Points[1].Metrics.Episodes, c.Points[2].Metrics.Episodes}
	if got[0] != 2 || got[1] != 0 || got[2] != 1 {
		t.Fatalf("episodes per window = %v, want [2 0 1]", got)
	}
	if c.Total.Episodes != 3 {
		t.Fatalf("total = %d", c.Total.Episodes)
	}

	var buf bytes.Buffer
	if err := c.WriteCSV(&buf); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || strings.Join(rows[0], ",") != strings.Join(csvHeader, ",") {
		t.Fatalf("csv shape: %v", rows)
	}
	if _, err := BuildCurve("t", ds, 0); err == nil {
		t.Fatal("zero window accepted")
	}
}

func TestLoadAndReplayOnSimulatedHelix(t *testing.T) {
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	eps, outs := helixsim.Generate(helixsim.Config{Seed: 1, Count: 200, Start: t0, Span: 14 * 24 * time.Hour})
	for _, e := range eps {
		if err := j.AppendEpisode(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range outs {
		if err := j.AppendOutcome(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Episodes) != len(eps) {
		t.Fatalf("loaded %d episodes, want %d", len(ds.Episodes), len(eps))
	}
	n := 0
	for _, o := range ds.Outcomes {
		n += len(o)
	}
	if n != len(outs) {
		t.Fatalf("loaded %d outcomes, want %d", n, len(outs))
	}

	// One lesson that fixes one scenario: replay must prefer it exactly on
	// that scenario's episodes and nowhere else.
	lesson := types.Lesson{ID: types.NewID(t0), Text: "an empty-glob result is an answer; stop searching"}
	s, err := Replay(ctx, helixsim.Host{}, helixsim.Judge{}, ds, ds.Episodes, []types.Lesson{lesson})
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, e := range ds.Episodes {
		if e.Attrs["scenario"] == "empty-glob-loop" {
			want++
		}
	}
	if want == 0 || s.PreferWith != want || s.PreferOld != 0 || s.Compared != len(ds.Episodes) {
		t.Fatalf("replay summary %+v, want prefer_with=%d prefer_without=0", s, want)
	}
}
