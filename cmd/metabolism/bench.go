package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/deliver"
	"github.com/Nibir1/metabolism/hosts/coder"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// defaultExercises is where make bench-setup puts the benchmark.
var defaultExercises = filepath.Join(".runs", "bench", "polyglot-benchmark", "go", "exercises", "practice")

// cmdBench runs the second host (Phase 8, D-032): the coding agent on the
// public benchmark's Go exercises, recording each run as an episode in its
// own journal. With lessons off it is the public benchmark's baseline
// (Phase 0); with lessons on they are delivered by the same coin flip Helix
// uses, so the same ledger credits them. The engine's own commands (digest,
// verify -host coder, credit, transfer) then run on that journal unchanged.
func cmdBench(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", filepath.Join(".runs", "bench", "journal.db"), "the second host's journal")
	dir := fs.String("exercises", defaultExercises, "the benchmark's Go exercises (make bench-setup)")
	n := fs.Int("n", 10, "exercises this run: the least-run first")
	lessonsMode := fs.String("lessons", "auto", "deliver the journal's tested lessons by coin flip: on, off (the learning-off baseline), "+
		"or auto: off until every exercise has a baseline run, then on")
	model := fs.String("model", "", "DeepSeek model (default: deepseek-flash)")
	allowRemote := fs.Bool("allow-remote", false, "allow the agent's model calls (each exercise's instructions and code go to DeepSeek)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exs, err := coder.Load(*dir)
	if err != nil {
		return fmt.Errorf("%w (make bench-setup downloads the benchmark)", err)
	}
	rem := remote{allowed: *allowRemote, out: out}
	m0, err := pickModel("deepseek", *model, rem)
	if err != nil {
		return err
	}
	rem.notice("each exercise's instructions, stub and test output go to %s; its code runs only in a sandboxed copy", m0.Name())
	if err := os.MkdirAll(filepath.Dir(*path), 0o700); err != nil {
		return err
	}
	j, err := sqlite.Open(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	ds, err := bench.Load(ctx, j)
	if err != nil {
		return err
	}
	runs := map[string]int{}
	baseline := map[string]bool{}
	for _, ep := range ds.Episodes {
		runs[ep.Attrs["exercise"]]++
		if len(ep.Exposure) == 0 && len(ep.Withheld) == 0 {
			baseline[ep.Attrs["exercise"]] = true
		}
	}
	// Least-run first, then a fixed hash order: every exercise is reached,
	// and a rerun picks the same ones.
	slices.SortStableFunc(exs, func(a, b coder.Exercise) int {
		if d := runs[a.Name] - runs[b.Name]; d != 0 {
			return d
		}
		return cmp.Compare(hashName(a.Name), hashName(b.Name))
	})
	var lessonsOn bool
	switch *lessonsMode {
	case "on":
		lessonsOn = true
	case "off":
	case "auto":
		lessonsOn = !slices.ContainsFunc(exs, func(ex coder.Exercise) bool { return !baseline[ex.Name] })
	default:
		return fmt.Errorf("-lessons %q: want auto, on or off", *lessonsMode)
	}
	if !lessonsOn {
		// The baseline first: exercises without a lessons-off run lead.
		slices.SortStableFunc(exs, func(a, b coder.Exercise) int {
			switch {
			case baseline[a.Name] == baseline[b.Name]:
				return 0
			case !baseline[a.Name]:
				return -1
			}
			return 1
		})
	}
	if *n > 0 && len(exs) > *n {
		exs = exs[:*n]
	}

	var file deliver.File
	if lessonsOn {
		m, err := metabolome.Load(ctx, j)
		if err != nil {
			return err
		}
		book, err := ledger.Credit(ctx, j, m, time.Now(), ledger.Rule{}, nil)
		if err != nil {
			return err
		}
		file = deliver.Build(m, book.Lessons, time.Now())
	}
	host := coder.Host{}.Describe()
	agent := coder.Agent{Model: m0}
	passed := 0
	say(out, "second host: %d exercise(s) · lessons %s (%d deliverable)\n", len(exs), map[bool]string{true: "on", false: "off (the baseline)"}[lessonsOn], len(file.Lessons))
	for i, ex := range exs {
		started := time.Now()
		probe := types.Episode{Scope: types.Scope{Level: types.ScopeProject, Key: coder.Benchmark}}
		sel := deliver.Select(file, probe, host.Budget, rand.New(rand.NewPCG(uint64(started.UnixNano()), hashName(ex.Name))))
		var lessons []types.Lesson
		for _, l := range file.Lessons {
			if slices.Contains(sel.Delivered, types.ID(l.ID)) {
				lessons = append(lessons, types.Lesson{ID: types.ID(l.ID), Text: l.Text})
			}
		}
		res, err := agent.Solve(ctx, ex, lessons)
		if err != nil {
			return fmt.Errorf("%s: %w", ex.Name, err)
		}
		ep, o := coder.Episode(ex, res, sel.Delivered, sel.Withheld, started, time.Now())
		if err := j.AppendEpisode(ctx, ep); err != nil {
			return err
		}
		if err := j.AppendOutcome(ctx, o); err != nil {
			return err
		}
		mark := "✘"
		if res.Passed {
			mark = "✔"
			passed++
		}
		say(out, "  %2d/%d %s %-28s attempts %d · lessons %d delivered, %d withheld\n",
			i+1, len(exs), mark, ex.Name, res.Attempts, len(sel.Delivered), len(sel.Withheld))
	}
	say(out, "passed %d of %d (%.0f%%) · journal %s\n", passed, len(exs), 100*float64(passed)/float64(max(len(exs), 1)), *path)
	return nil
}

func hashName(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
