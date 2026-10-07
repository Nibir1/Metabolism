package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/types"
)

// cmdDecisions measures where a host's model calls go, from episodes with
// decision records (recording wire v3), for Phase 5a's stop condition
// (docs/habits.md, D-031).
func cmdDecisions(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("decisions", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	outDir := fs.String("out", "", "directory to write decisions.json (optional)")
	since := fs.String("since", "", "only episodes started at or after this time (RFC 3339 or 2006-01-02T15:04), e.g. after a host fix")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var from time.Time
	if *since != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, *since, time.Local); err == nil {
				from = t
				break
			}
		}
		if from.IsZero() {
			return fmt.Errorf("-since %q: use 2006-01-02T15:04 or RFC 3339", *since)
		}
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	var eps []types.Episode
	for r, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode}}) {
		if err != nil {
			return err
		}
		if r.Episode.StartedAt.Before(from) {
			continue
		}
		eps = append(eps, *r.Episode)
	}
	d := bench.MeasureDecisions(eps)
	say(out, "where the model calls went · %d episodes with decision records (%d older ones left out) · %d calls\n\n",
		d.Episodes, d.Older, d.Calls)
	if d.Episodes == 0 {
		say(out, "no episode carries decision records yet: they start with Helix's recording wire v3 (D-031)\n")
		return nil
	}
	pct := func(n int) string { return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(max(d.Calls, 1))) }
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  BY KIND\tCALLS\tSHARE\n")
	kinds := make([]string, 0, len(d.ByKind))
	for k := range d.ByKind {
		kinds = append(kinds, k)
	}
	slices.SortFunc(kinds, func(a, b string) int { return d.ByKind[b] - d.ByKind[a] })
	for _, k := range kinds {
		say(tw, "  %s\t%d\t%s\n", k, d.ByKind[k], pct(d.ByKind[k]))
	}
	say(tw, "  PLANNER BY CAUSE\t\t\n")
	say(tw, "  first round, first attempt\t%d\t%s\n", d.FirstAttempts, pct(d.FirstAttempts))
	say(tw, "  retries and smaller prompts\t%d\t%s\n", d.Retries, pct(d.Retries))
	say(tw, "  follow-up rounds\t%d\t%s\n", d.FollowUpCalls, pct(d.FollowUpCalls))
	_ = tw.Flush()
	say(out, "\n  %d planner rounds: %d ended without a usable plan, %d needed a smaller prompt, %d follow-up rounds only answered\n",
		d.Plans, d.FailedPlannings, d.SmallerPrompts, d.AnsweredRound)
	say(out, "  critic: %d review(s), %d said no · fallbacks: %v\n\n", d.CriticRuns, d.CriticNo, d.Fallbacks)

	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  CANDIDATE DECISION\tCALLS\tSHARE\t≥ %.0f%%?\n", 100*bench.StopShare)
	for _, c := range d.Candidates {
		mark := "no"
		if c.Passes {
			mark = "yes"
		}
		say(tw, "  %s\t%d\t%.1f%%\t%s\n", c.Name, c.Calls, 100*c.Share, mark)
	}
	_ = tw.Flush()
	say(out, "\nStop condition (D-031): Phase 5's cost work goes on only if a candidate touches ≥ %.0f%% of model calls\n"+
		"and half of those can be saved. Share is the first half; savability is measured offline next.\n"+
		"Planning mode saves tokens rather than calls and is not in this table.\n", 100*bench.StopShare)
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		return writeFile(filepath.Join(*outDir, "decisions.json"), func(w io.Writer) error { return writeJSON(w, d) })
	}
	return nil
}
