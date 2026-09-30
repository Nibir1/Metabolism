package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/episodic"
	"github.com/Nibir1/metabolism/types"
)

// openExisting opens a journal that must already exist: opening creates one,
// which is never what a read-only command means.
func openExisting(ctx context.Context, path string) (*sqlite.Journal, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no journal at %s", path)
	}
	return sqlite.Open(ctx, path)
}

// parseScope reads "global", "machine:KEY" or "project:KEY". Empty is no scope.
func parseScope(s string) (types.Scope, error) {
	if s == "" {
		return types.Scope{}, nil
	}
	level, key, _ := strings.Cut(s, ":")
	sc := types.Scope{Level: types.ScopeLevel(level), Key: key}
	if err := sc.Validate(); err != nil {
		return types.Scope{}, fmt.Errorf("-scope %q: %w (want global, machine:KEY or project:KEY)", s, err)
	}
	return sc, nil
}

func scopeString(sc types.Scope) string {
	if sc.Key == "" {
		return string(sc.Level)
	}
	return string(sc.Level) + ":" + sc.Key
}

func cmdInspect(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	n := fs.Int("n", 10, "recent episodes to list")
	end := fs.String("end", "", "only episodes that ended this way (done, failed, budget-exhausted, open-work)")
	scopeFlag := fs.String("scope", "", "only episodes in this scope (global, machine:KEY, project:KEY)")
	id := fs.String("id", "", "show one episode in full, with its outcomes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	scope, err := parseScope(*scopeFlag)
	if err != nil {
		return err
	}
	if *end != "" && !types.RunEnd(*end).Valid() {
		return fmt.Errorf("-end %q is not a run end", *end)
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()

	if *id != "" {
		return inspectOne(ctx, j, types.ID(*id), out)
	}

	counts := map[types.RecordKind]int{}
	var recent []types.Episode
	for r, err := range j.Scan(ctx, types.JournalQuery{}) {
		if err != nil {
			return err
		}
		counts[r.Kind]++
		if r.Kind != types.RecordEpisode {
			continue
		}
		e := *r.Episode
		if (*end != "" && e.End != types.RunEnd(*end)) || (scope.Level != "" && e.Scope != scope) {
			continue
		}
		recent = append(recent, e)
		if len(recent) > *n {
			recent = recent[1:]
		}
	}
	_, _ = fmt.Fprintf(out, "%s\n  episodes %d · outcomes %d · lifecycle events %d\n",
		*path, counts[types.RecordEpisode], counts[types.RecordOutcome], counts[types.RecordEvent])
	if len(recent) == 0 {
		return nil
	}
	_, _ = fmt.Fprintf(out, "\nlast %d matching episodes:\n", len(recent))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, e := range recent {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%d steps\t%s\t%s\n", e.ID, e.StartedAt.UTC().Format("2006-01-02 15:04"),
			e.End, len(e.Steps), e.Request.Provenance, truncate(e.Request.Text, 50))
	}
	return tw.Flush()
}

func inspectOne(ctx context.Context, j *sqlite.Journal, id types.ID, out io.Writer) error {
	var ep *types.Episode
	var outs []types.Outcome
	for r, err := range j.Scan(ctx, types.JournalQuery{EpisodeID: id}) {
		if err != nil {
			return err
		}
		switch {
		case r.Episode != nil:
			ep = r.Episode
		case r.Outcome != nil:
			outs = append(outs, *r.Outcome)
		}
	}
	if ep == nil {
		return fmt.Errorf("no episode %s", id)
	}
	verdict := "did not succeed"
	if types.Succeeded(*ep, outs) {
		verdict = "succeeded"
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	row := func(k, v string) { _, _ = fmt.Fprintf(tw, "  %s\t%s\n", k, v) }
	_, _ = fmt.Fprintf(out, "episode %s\n", ep.ID)
	row("host", ep.Host)
	row("scope", scopeString(ep.Scope))
	row("when", fmt.Sprintf("%s (%s)", ep.StartedAt.UTC().Format(time.DateTime), ep.EndedAt.Sub(ep.StartedAt).Round(time.Millisecond)))
	row("request", ep.Request.Text)
	row("provenance", string(ep.Request.Provenance))
	row("ended", fmt.Sprintf("%s · %s", ep.End, verdict))
	row("usage", fmt.Sprintf("%d model calls · %d chars in · %d chars out",
		ep.Usage.ModelCalls, ep.Usage.InputChars, ep.Usage.OutputChars))
	if len(ep.Exposure) > 0 {
		row("lessons", fmt.Sprint(ep.Exposure))
	}
	_ = tw.Flush()
	if len(ep.Steps) > 0 {
		_, _ = fmt.Fprintln(out, "\nsteps:")
		for i, s := range ep.Steps {
			mark := "✔"
			if !s.OK {
				mark = "✘"
			}
			line := fmt.Sprintf("  %d %s %s", i+1, mark, strings.TrimSpace(s.Tool+" "+s.Action))
			if s.Subject != "" {
				line += "  " + s.Subject
			}
			if s.Err != "" {
				line += "  (" + s.Err + ")"
			}
			_, _ = fmt.Fprintln(out, line)
		}
	}
	if len(outs) > 0 {
		_, _ = fmt.Fprintln(out, "\noutcomes:")
		for _, o := range outs {
			line := fmt.Sprintf("  %s  %s from %s", o.At.UTC().Format(time.DateTime), o.Kind, o.Source)
			if o.Note != "" {
				line += "  " + o.Note
			}
			_, _ = fmt.Fprintln(out, line)
		}
	}
	return nil
}

func cmdRecall(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	q := fs.String("q", "", "the request to recall cases for (required)")
	scopeFlag := fs.String("scope", "", "prefer cases from this scope (global, machine:KEY, project:KEY)")
	k := fs.Int("k", 5, "cases to return")
	untrusted := fs.Bool("untrusted", false, "include episodes whose request has untrusted provenance")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*q) == "" {
		return errors.New("-q is required")
	}
	scope, err := parseScope(*scopeFlag)
	if err != nil {
		return err
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	ix := episodic.New(episodic.Options{})
	if _, err := ix.Refresh(ctx, j); err != nil {
		return err
	}
	cases := ix.Retrieve(episodic.Query{Text: *q, Scope: scope, K: *k, IncludeUntrusted: *untrusted})
	if len(cases) == 0 {
		_, _ = fmt.Fprintf(out, "no past episode shares a word with %q (%d indexed)\n", *q, ix.Len())
		return nil
	}
	_, _ = fmt.Fprintf(out, "%d case(s) for %q from %d episodes:\n\n", len(cases), *q, ix.Len())
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  score\twhen\tresult\tsteps\trequest\tid")
	for _, c := range cases {
		result := "✔ succeeded"
		if !c.Succeeded {
			result = "✘ " + string(c.Episode.End)
			for _, o := range c.Outcomes {
				if o.Kind.Negative() {
					result = "✘ " + string(o.Kind)
				}
			}
		}
		_, _ = fmt.Fprintf(tw, "  %.2f\t%s\t%s\t%d\t%s\t%s\n", c.Score, c.Episode.StartedAt.UTC().Format(time.DateOnly),
			result, len(c.Episode.Steps), truncate(c.Episode.Request.Text, 50), c.Episode.ID)
	}
	return tw.Flush()
}

func cmdRecallEval(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("recall-eval", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	k := fs.Int("k", 5, "cases recalled per episode")
	minHist := fs.Int("min-history", 20, "earlier episodes needed before one is evaluated")
	label := fs.String("label", "", "episode attribute naming the task, for same-task precision (helixsim: scenario)")
	name := fs.String("name", "", "result label (default: the journal's file name)")
	outDir := fs.String("out", "", "directory to write recall.json (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		*name = strings.TrimSuffix(filepath.Base(*path), filepath.Ext(*path))
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	ds, err := bench.Load(ctx, j)
	if err != nil {
		return err
	}
	r := bench.EvaluateRecall(*name, ds, bench.RecallConfig{K: *k, MinHistory: *minHist, LabelAttr: *label})
	printRecall(out, r)
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(*outDir, "recall.json"), func(w io.Writer) error {
			return writeJSON(w, r)
		}); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "\nwrote %s/recall.json\n", *outDir)
	}
	return nil
}

func printRecall(out io.Writer, r bench.RecallResult) {
	_, _ = fmt.Fprintf(out, "%s · episodic recall, k=%d · %d of %d episodes evaluated\n\n",
		r.Label, r.K, r.Evaluated, r.Episodes)
	if r.Evaluated == 0 {
		_, _ = fmt.Fprintln(out, "  not enough history to evaluate")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  \tmemory\tbase rate\t")
	_, _ = fmt.Fprintf(tw, "  brier (lower is better)\t%.4f\t%.4f\t\n", r.BrierMemory, r.BrierBase)
	_, _ = fmt.Fprintf(tw, "  accuracy\t%s\t%s\t\n", pct(r.AccuracyMemory), pct(r.AccuracyBase))
	_ = tw.Flush()
	_, _ = fmt.Fprintf(out, "\n  skill %.3f · recalled something for %s of episodes", r.Skill, pct(r.Covered))
	if r.SameTaskPrecision >= 0 {
		_, _ = fmt.Fprintf(out, " · same-task precision %s", pct(r.SameTaskPrecision))
	}
	_, _ = fmt.Fprintln(out)
	switch {
	case r.Skill > 0.05:
		_, _ = fmt.Fprintln(out, "  memory predicts outcomes better than the base rate: recalled cases carry signal")
	case r.Skill < -0.05:
		_, _ = fmt.Fprintln(out, "  memory predicts WORSE than the base rate: recalled cases mislead")
	default:
		_, _ = fmt.Fprintln(out, "  no meaningful difference from the base rate: recall carries little signal here")
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
