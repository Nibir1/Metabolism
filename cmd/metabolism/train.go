package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/adapters/remote/llm"
	"github.com/Nibir1/metabolism/adapters/remote/mlx"
	"github.com/Nibir1/metabolism/hosts/coder"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/parametric"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

var defaultTrainDir = filepath.Join(".runs", "train")

// cmdTrain is Phase 7 (C7, D-032), on the second host where every answer is
// checked by tests. A small local model runs every exercise three ways:
// alone (the old-task suite: what it already solves), with the promoted
// lessons as text, and with a LoRA adapter trained on its own passing
// text-lesson runs from the training half, without the lessons. The adapter
// is kept in use only if it retains ≥ 95% of the old tasks and does at least
// as well as the text lessons on the held-out half at a smaller prompt; the
// rollback is exercised either way. Everything runs on this machine.
func cmdTrain(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("train", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", filepath.Join(".runs", "bench", "journal.db"), "the second host's journal (where the lessons were credited)")
	dir := fs.String("exercises", defaultExercises, "the benchmark's exercises")
	base := fs.String("base", mlx.DefaultBase, "the small local model to train (an MLX model on Hugging Face)")
	py := fs.String("python", mlx.DefaultPython, "the trainer environment's Python (make trainer-setup)")
	iters := fs.Int("iters", 200, "training iterations")
	trainDir := fs.String("out", defaultTrainDir, "where adapters, datasets and evaluations go")
	minTurns := fs.Int("min-turns", 50, "promotion: delivered turns a lesson's credit needs")
	minAge := fs.Duration("min-age", 7*24*time.Hour, "promotion: how long a lesson must have been accepted")
	only := fs.String("lessons", "", "comma-separated lesson IDs to consolidate instead of the promotion rule (an experiment; the evaluation says so)")
	allowRemote := fs.Bool("allow-remote", false, "allow the first download of the base model from Hugging Face (training data never leaves the machine)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rem := remote{allowed: *allowRemote, out: out}
	if err := rem.require("the base model's first download", "huggingface.co"); err != nil {
		return err
	}
	exs, err := coder.Load(*dir)
	if err != nil {
		return fmt.Errorf("%w (make bench-setup)", err)
	}
	lessons, err := promoted(ctx, *path, *only, parametric.PromoteRule{MinTurns: *minTurns, MinAge: *minAge})
	if err != nil {
		return err
	}
	if len(lessons) == 0 {
		say(out, "nothing to consolidate: no lesson is accepted, credited on ≥ %d turns and %s old on the second host yet\n", *minTurns, *minAge)
		return nil
	}
	say(out, "consolidating %d lesson(s) into %s\n", len(lessons), *base)
	for _, l := range lessons {
		say(out, "  %s %s\n", l.ID, truncate(l.Text, 80))
	}
	version := time.Now().Format("20060102T150405")
	vdir, err := filepath.Abs(filepath.Join(*trainDir, version))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(vdir, 0o700); err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(vdir, "mlx.log"))
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()

	// 1. The base model alone, then with the text lessons.
	var baseRuns, textRuns []parametric.Run
	var examples []parametric.Example
	err = withModel(ctx, mlx.Server{Python: *py, Base: *base, Log: log}, func(m ports.Model) error {
		say(out, "\nbase model, no lessons (the old-task suite):\n")
		baseRuns, _, err = runAll(ctx, out, m, exs, nil)
		if err != nil {
			return err
		}
		say(out, "\nbase model, with the lessons as text:\n")
		textRuns, examples, err = runAll(ctx, out, m, exs, lessons)
		return err
	})
	if err != nil {
		return err
	}
	var train []parametric.Example
	for _, ex := range examples {
		if !parametric.HeldOut(ex.Task) {
			train = append(train, ex)
		}
	}
	if len(train) < 4 {
		say(out, "\ntoo few training examples: %d passing first attempts on the training half (need 4)\n", len(train))
		return nil
	}
	nTrain, nValid, err := parametric.WriteDataset(filepath.Join(vdir, "data"), train)
	if err != nil {
		return err
	}

	// 2. Train.
	say(out, "\ntraining on %d examples (%d for validation), %d iterations…\n", nTrain, nValid, *iters)
	adapter := filepath.Join(vdir, "adapter")
	if err := (mlx.Trainer{Python: *py, Log: log}).Train(ctx, ports.TrainRequest{
		Base: *base, DataDir: filepath.Join(vdir, "data"), OutDir: adapter, Iters: *iters}); err != nil {
		return err
	}

	// 3. The adapter, with no lessons.
	var adapterRuns []parametric.Run
	err = withModel(ctx, mlx.Server{Python: *py, Base: *base, Adapter: adapter, Log: log}, func(m ports.Model) error {
		say(out, "\nadapter, no lessons:\n")
		adapterRuns, _, err = runAll(ctx, out, m, exs, nil)
		return err
	})
	if err != nil {
		return err
	}

	// 4. Judge, register, and exercise the rollback.
	ev := parametric.Evaluate(baseRuns, textRuns, adapterRuns)
	reg := parametric.Registry{Dir: *trainDir}
	ids := make([]string, len(lessons))
	for i, l := range lessons {
		ids[i] = string(l.ID)
	}
	if err := reg.Add(parametric.Version{Name: version, Path: adapter, Base: *base, At: time.Now(),
		Lessons: ids, Examples: nTrain + nValid, Eval: &ev}); err != nil {
		return err
	}
	before, err := reg.Load()
	if err != nil {
		return err
	}
	if err := reg.Use(version, "trained", time.Now()); err != nil {
		return err
	}
	prev, err := reg.Rollback("rollback check", time.Now())
	if err != nil {
		return err
	}
	after, err := reg.Load()
	if err != nil {
		return err
	}
	ev.RolledBack = prev == before.Active && after.Active == before.Active
	if ev.Passes {
		if err := reg.Use(version, "passes C7: "+ev.Why, time.Now()); err != nil {
			return err
		}
	}
	if *only != "" {
		ev.Why += " (lessons chosen by hand, not by the promotion rule)"
	}
	if err := writeFile(filepath.Join(vdir, "eval.json"), func(w io.Writer) error { return writeJSON(w, ev) }); err != nil {
		return err
	}
	say(out, "\nretained %.0f%% of %d old tasks · held-out: adapter %.0f%%, text lessons %.0f%% (%+.0f points) · prompt %.0f%% of the text lessons' · rollback %s\n",
		100*ev.Retained, ev.OldTasks, 100*ev.AdapterPass, 100*ev.TextPass, 100*ev.Gain, 100*ev.TokensRatio,
		map[bool]string{true: "works", false: "FAILED"}[ev.RolledBack])
	state := "kept rolled back: the base model stays in use"
	if ev.Passes {
		state = "in use"
	}
	say(out, "%s: %s · adapter %s %s\n", map[bool]string{true: "passes", false: "does not pass"}[ev.Passes], ev.Why, version, state)
	return nil
}

// promoted is the lessons to consolidate: by the rule, or by hand.
func promoted(ctx context.Context, path, only string, rule parametric.PromoteRule) ([]types.Lesson, error) {
	j, err := openExisting(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%w (the second host's lessons: make collect BENCH=n)", err)
	}
	defer func() { _ = j.Close() }()
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return nil, err
	}
	if only != "" {
		var out []types.Lesson
		for _, id := range strings.Split(only, ",") {
			e, ok := m.Get(types.ID(strings.TrimSpace(id)))
			if !ok {
				return nil, fmt.Errorf("no lesson %s", id)
			}
			out = append(out, e.Lesson)
		}
		return out, nil
	}
	book, err := ledger.Credit(ctx, j, m, time.Now(), ledger.Rule{}, nil)
	if err != nil {
		return nil, err
	}
	return parametric.Promote(m, book.Lessons, time.Now(), rule), nil
}

// withModel starts a local server, gives f a model on it, and stops it.
func withModel(ctx context.Context, s mlx.Server, f func(ports.Model) error) error {
	url, stop, err := s.Start(ctx)
	if err != nil {
		return err
	}
	defer stop()
	return f(llm.OpenAI{Provider: "mlx", BaseURL: url, Model: s.Base, APIKey: "local"})
}

// runAll runs the agent on every exercise, returning each run and the
// training examples passing first attempts gave.
func runAll(ctx context.Context, out io.Writer, m ports.Model, exs []coder.Exercise, lessons []types.Lesson) ([]parametric.Run, []parametric.Example, error) {
	agent := coder.Agent{Model: m}
	var runs []parametric.Run
	var examples []parametric.Example
	passed := 0
	for _, ex := range exs {
		res, err := agent.Solve(ctx, ex, lessons)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, coder.ErrNoSandbox) {
				return nil, nil, err
			}
			say(out, "  %s: %v\n", ex.Name, err)
			continue
		}
		runs = append(runs, parametric.Run{Task: ex.Name, Passed: res.Passed, PromptChars: res.PromptChars})
		if res.Passed {
			passed++
		}
		if res.Example != nil {
			examples = append(examples, parametric.Example{Task: ex.Name, System: res.Example.System,
				User: res.Example.User, Answer: res.Example.Answer})
		}
	}
	say(out, "  passed %d of %d\n", passed, len(exs))
	return runs, examples, nil
}

// cmdAdapter lists trained adapters, or puts one in use or rolls back.
func cmdAdapter(_ context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("adapter", flag.ContinueOnError)
	fs.SetOutput(out)
	dir := fs.String("dir", defaultTrainDir, "the adapter registry")
	use := fs.String("use", "", "put this version in use (\"none\": the base model alone)")
	rollback := fs.Bool("rollback", false, "put back the version in use before the current one")
	reason := fs.String("reason", "", "why (kept in the registry)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg := parametric.Registry{Dir: *dir}
	switch {
	case *use != "":
		name := *use
		if name == "none" {
			name = ""
		}
		if err := reg.Use(name, *reason, time.Now()); err != nil {
			return err
		}
	case *rollback:
		prev, err := reg.Rollback(*reason, time.Now())
		if err != nil {
			return err
		}
		say(out, "rolled back to %s\n", orDash(prev))
	}
	s, err := reg.Load()
	if err != nil {
		return err
	}
	if len(s.Versions) == 0 {
		say(out, "no adapters trained yet (metabolism train)\n")
		return nil
	}
	for _, v := range s.Versions {
		mark := " "
		if v.Name == s.Active {
			mark = "*"
		}
		why := ""
		if v.Eval != nil {
			why = v.Eval.Why
		}
		say(out, "%s %s  %s  %d lessons, %d examples  %s\n", mark, v.Name, v.Base, len(v.Lessons), v.Examples, why)
	}
	say(out, "in use: %s\n", orDash(s.Active))
	return nil
}
