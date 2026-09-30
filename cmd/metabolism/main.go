// Command metabolism inspects and measures a Metabolism journal.
//
//	metabolism doctor  [-journal PATH] [-from FILE] [-helix-config FILE]
//	metabolism ingest  -journal PATH [-from FILE]
//	metabolism sim     -journal PATH [-seed N] [-count N] [-days N]
//	metabolism curves  -journal PATH [-window 7d] [-label NAME] [-out DIR]
//	metabolism inspect -journal PATH [-n 10] [-end E] [-scope S] [-id ID]
//	metabolism recall  -journal PATH -q TEXT [-scope S] [-k 5]
//	metabolism recall-eval -journal PATH [-k 5] [-label ATTR] [-out DIR]
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

run "metabolism <command> -h" for a command's flags
`

func main() {
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
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return nil
	}
	return fmt.Errorf("unknown command %q (try: doctor, ingest, sim, curves, inspect, recall, recall-eval)", args[0])
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
	if st.Orphans > 0 {
		_, _ = fmt.Fprintf(out, "  %d outcome(s) skipped: their episode rotated out before it was ingested\n", st.Orphans)
	}
	if st.Invalid+st.Malformed > 0 {
		_, _ = fmt.Fprintf(out, "  %d invalid and %d malformed line(s) skipped:\n", st.Invalid, st.Malformed)
		for _, p := range st.Problems {
			_, _ = fmt.Fprintf(out, "    %s\n", p)
		}
	}
	return nil
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
