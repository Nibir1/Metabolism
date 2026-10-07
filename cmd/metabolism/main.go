// Command metabolism inspects and measures a Metabolism journal.
//
//	metabolism doctor  [-journal PATH] [-from FILE] [-helix-config FILE]
//	metabolism ingest  -journal PATH [-from FILE]
//	metabolism sim     -journal PATH [-seed N] [-count N] [-days N]
//	metabolism curves  -journal PATH [-window 7d] [-label NAME] [-out DIR]
//	metabolism inspect -journal PATH [-n 10] [-end E] [-scope S] [-id ID]
//	metabolism recall  -journal PATH -q TEXT [-scope S] [-k 5]
//	metabolism recall-eval -journal PATH [-k 5] [-label ATTR] [-out DIR]
//	metabolism digest  -journal PATH [-reflector sim] [-since D] [-until D]
//	metabolism verify  -journal PATH [-host sim] [-judge heuristic] [-lesson ID]
//	metabolism lessons -journal PATH [-state S] [-id ID]
//	metabolism nutrient-eval [-seeds 5] [-count 400] [-out DIR]
//	metabolism replay  -journal PATH -episode ID -lesson TEXT [-host helix] [-judge jev] -allow-remote
//	metabolism label   -from FILE [-score] [-redo]
//	metabolism crosscheck -from FILE [-judge deepseek] -allow-remote
//	metabolism credit  -journal PATH [-apply]
//	metabolism export  -journal PATH [-to FILE]
//	metabolism credit-eval [-seeds 5] [-turns 3000] [-out DIR]
//	metabolism poison-eval [-seeds 2] [-turns 1500] [-out DIR]
//	metabolism drift-eval [-seeds 5] [-before 3000] [-after 3000] [-out DIR]
//	metabolism pairs-eval [-seeds 5] [-turns 6000] [-out DIR]
//	metabolism decisions -journal PATH [-since TIME] [-out DIR]
//	metabolism patterns -journal PATH [-min 3] [-min-requests 2] [-out DIR]
//	metabolism snapshot [-note TEXT] NAME
//	metabolism versions
//	metabolism diff    FROM [TO]
//	metabolism rollback [-apply] VERSION
//	metabolism forget  [-episode ID]... [-lesson ID]... [-match TEXT] [-keep-lessons] -reason R [-apply] [-verify]
//	metabolism auto    -allow-remote [-dir .runs/auto] [-requests 100] [-plan-only]
//
// The journal defaults to $METABOLISM_JOURNAL, then ~/.metabolism/journal.db.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/adapters/ndjson"
	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/hosts/helixsim"
)

const usage = `usage: metabolism <command> [flags]

commands:
  doctor   what the Helix → engine pipeline sees right now, and what to do next
  ingest   read a host's recorded episodes (Helix: /metabolism on) into a journal
  sim      fill a journal with simulated Helix episodes (for testing the harness)
  curves   compute learning curves from a journal
  inspect  summarise what a journal holds, or show one episode
  recall   the past episodes most like a request (episodic memory)
  recall-eval  does recall predict outcomes better than the base rate?
  digest   reflect on episodes and record candidate lessons
  verify   the nutrient test: replay with and without each candidate lesson
  lessons  list lessons, or show one's history and evidence
  nutrient-eval  does the nutrient test reject planted wrong lessons? (simulator)
  replay   replay one episode with and without a lesson; show both plans and the verdict
  label    label saved comparisons blind, or score a judge against your labels
  crosscheck  re-judge saved comparisons with a second judge and report agreement
  credit   what live turns say about each delivered lesson; -apply accepts or eliminates
  export   write the lessons Helix may deliver (~/.helix/metabolism/lessons.json)
  credit-eval  does the credit ledger accept what helps and eliminate what harms? (simulator)
  poison-eval  the poisoning suite: do poisoned lessons persist or do harm? (simulator)
  drift-eval   are accepted lessons retired when the world changes under them? (simulator)
  pairs-eval   are contradicting and redundant lessons set aside? (simulator)
  decisions where the host's model calls went, for Phase 5a (wire v3 episodes)
  patterns failures that recur the same way, and what could answer each (Phase 5c)
  snapshot name the current version of the metabolome
  versions list named versions and forget records
  diff     which lessons changed state between two versions
  rollback put what is delivered back to what was delivered at a version (-apply records it)
  forget   delete episodes or lessons with an audit record (forget-with-proof; -apply deletes)
  auto     one automated day: a synthetic user drives real Helix, then ingest, digest, verify, credit

run "metabolism <command> -h" for a command's flags
`

func main() {
	if err := loadDotEnv(dotEnvFile); err != nil {
		fmt.Fprintln(os.Stderr, "metabolism:", err)
		os.Exit(1)
	}
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "metabolism:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return errors.New("no command given")
	}
	switch args[0] {
	case "doctor":
		return cmdDoctor(ctx, args[1:], out)
	case "ingest":
		return cmdIngest(ctx, args[1:], out)
	case "sim":
		return cmdSim(ctx, args[1:], out)
	case "curves":
		return cmdCurves(ctx, args[1:], out)
	case "inspect":
		return cmdInspect(ctx, args[1:], out)
	case "recall":
		return cmdRecall(ctx, args[1:], out)
	case "recall-eval":
		return cmdRecallEval(ctx, args[1:], out)
	case "digest":
		return cmdDigest(ctx, args[1:], out)
	case "verify":
		return cmdVerify(ctx, args[1:], out)
	case "lessons":
		return cmdLessons(ctx, args[1:], out)
	case "nutrient-eval":
		return cmdNutrientEval(ctx, args[1:], out)
	case "replay":
		return cmdReplay(ctx, args[1:], out)
	case "crosscheck":
		return cmdCrosscheck(ctx, args[1:], out)
	case "label":
		return cmdLabel(ctx, args[1:], out, os.Stdin)
	case "credit":
		return cmdCredit(ctx, args[1:], out)
	case "export":
		return cmdExport(ctx, args[1:], out)
	case "credit-eval":
		return cmdCreditEval(ctx, args[1:], out)
	case "auto":
		return cmdAuto(ctx, args[1:], out)
	case "patterns":
		return cmdPatterns(ctx, args[1:], out)
	case "decisions":
		return cmdDecisions(ctx, args[1:], out)
	case "snapshot":
		return cmdSnapshot(ctx, args[1:], out)
	case "versions":
		return cmdVersions(ctx, args[1:], out)
	case "diff":
		return cmdDiff(ctx, args[1:], out)
	case "rollback":
		return cmdRollback(ctx, args[1:], out)
	case "forget":
		return cmdForget(ctx, args[1:], out)
	case "pairs-eval":
		return cmdPairsEval(ctx, args[1:], out)
	case "drift-eval":
		return cmdDriftEval(ctx, args[1:], out)
	case "poison-eval":
		return cmdPoisonEval(ctx, args[1:], out)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return nil
	}
	return fmt.Errorf("unknown command %q (try: doctor, ingest, sim, curves, inspect, recall, recall-eval, digest, verify, lessons, nutrient-eval, replay, label, crosscheck, credit, export, credit-eval, auto, poison-eval, drift-eval, pairs-eval, snapshot, versions, diff, rollback, forget, decisions, patterns)", args[0])
}

func defaultJournal() string {
	if p := os.Getenv("METABOLISM_JOURNAL"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "journal.db"
	}
	return filepath.Join(home, ".metabolism", "journal.db")
}

// defaultHelixFile is where Helix's /metabolism writes.
func defaultHelixFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "episodes.ndjson"
	}
	return filepath.Join(home, ".helix", "metabolism", "episodes.ndjson")
}

func cmdIngest(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	from := fs.String("from", defaultHelixFile(), "NDJSON episode file (rotated generations are read too)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	j, err := sqlite.Open(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	st, err := ndjson.Ingest(ctx, j, *from)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "read %d records from %d file(s) into %s\n", st.Lines, len(st.Files), *path)
	_, _ = fmt.Fprintf(out, "  episodes %d · outcomes %d (appended, or already present)\n", st.Episodes, st.Outcomes)
	if st.Forgotten > 0 {
		_, _ = fmt.Fprintf(out, "  %d record(s) skipped: forgotten with proof (metabolism forget purges them from the recording)\n", st.Forgotten)
	}
	if st.Orphans > 0 {
		_, _ = fmt.Fprintf(out, "  %d outcome(s) skipped: their episode rotated out before it was ingested\n", st.Orphans)
	}
	if st.Invalid+st.Malformed > 0 {
		_, _ = fmt.Fprintf(out, "  %d invalid and %d malformed line(s) skipped:\n", st.Invalid, st.Malformed)
		for _, p := range st.Problems {
			_, _ = fmt.Fprintf(out, "    %s\n", p)
		}
	}
	return applyFeedback(ctx, j, st.Feedback, *from, out)
}

func cmdSim(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("sim", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	seed := fs.Uint64("seed", 1, "generator seed")
	count := fs.Int("count", 1000, "episodes to generate")
	days := fs.Int("days", 56, "days the episodes are spread across")
	start := fs.String("start", "2026-08-03", "first day (YYYY-MM-DD, UTC)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	from, err := time.Parse(time.DateOnly, *start)
	if err != nil {
		return fmt.Errorf("-start: %w", err)
	}
	j, err := sqlite.Open(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()

	eps, outs := helixsim.Generate(helixsim.Config{
		Seed: *seed, Count: *count, Start: from, Span: time.Duration(*days) * 24 * time.Hour,
	})
	for _, e := range eps {
		if err := j.AppendEpisode(ctx, e); err != nil {
			return err
		}
	}
	for _, o := range outs {
		if err := j.AppendOutcome(ctx, o); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(out, "wrote %d simulated episodes and %d outcomes to %s\n", len(eps), len(outs), *path)
	return nil
}

func cmdCurves(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("curves", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	windowFlag := fs.String("window", "7d", "window size (Go duration, or Nd for days)")
	label := fs.String("label", "", "curve label (default: the journal's file name)")
	outDir := fs.String("out", "", "directory to write curve.json and curve.csv (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	window, err := parseWindow(*windowFlag)
	if err != nil {
		return err
	}
	if *label == "" {
		*label = strings.TrimSuffix(filepath.Base(*path), filepath.Ext(*path))
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
	c, err := bench.BuildCurve(*label, ds, window)
	if err != nil {
		return err
	}
	printCurve(out, c)

	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(*outDir, "curve.json"), c.WriteJSON); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(*outDir, "curve.csv"), c.WriteCSV); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "\nwrote %s/curve.json and curve.csv\n", *outDir)
	}
	return nil
}

func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := write(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func printCurve(out io.Writer, c bench.Curve) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintf(out, "%s · window %s · %d episodes · tokens are estimates (%d chars/token)\n\n",
		c.Label, formatWindow(c.Window), c.Total.Episodes, bench.CharsPerToken)
	_, _ = fmt.Fprintln(tw, "window\tepisodes\tlabelled\tfinish\tcalls/fin\test tok/fin\tbudget\tfail step\tundo\tdecline\trepeat\t")
	row := func(name string, m bench.Metrics) {
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%.2f\t%.0f\t%s\t%s\t%s\t%s\t%s\t\n", name, m.Episodes,
			pct(m.OutcomeCoverage), pct(m.FinishRate), m.ModelCallsPerFinished, m.EstTokensPerFinished,
			pct(m.BudgetExhaustionRate), pct(m.FailedStepRate),
			pct(m.UndoRate), pct(m.DeclineRate), pct(m.RepeatRate))
	}
	for _, p := range c.Points {
		row(p.Start.UTC().Format(time.DateOnly), p.Metrics)
	}
	row("total", c.Total)
	_ = tw.Flush()
}

func pct(v float64) string { return strconv.FormatFloat(v*100, 'f', 1, 64) + "%" }

func parseWindow(s string) (time.Duration, error) {
	if d, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(d)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("-window %q: want a positive number of days", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	w, err := time.ParseDuration(s)
	if err != nil || w <= 0 {
		return 0, fmt.Errorf("-window %q: want a positive duration like 7d or 12h", s)
	}
	return w, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// formatWindow prints whole days as "7d", the form -window accepts.
func formatWindow(w time.Duration) string {
	if day := 24 * time.Hour; w%day == 0 {
		return strconv.Itoa(int(w/day)) + "d"
	}
	return w.String()
}
