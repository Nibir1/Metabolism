package main

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/Nibir1/metabolism/mine"
	"github.com/Nibir1/metabolism/types"
)

// cmdPatterns lists failures that recur the same way, sorted by what could
// answer them: a guard for the user to approve, a lesson, or nothing (Phase
// 5c, D-031). It proposes; it changes nothing.
func cmdPatterns(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("patterns", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	minCount := fs.Int("min", 3, "fewest occurrences of a pattern")
	minReq := fs.Int("min-requests", 2, "fewest distinct requests it must occur in")
	outDir := fs.String("out", "", "directory to write patterns.json (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	var eps []types.Episode
	outs := map[types.ID][]types.Outcome{}
	for r, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode, types.RecordOutcome}}) {
		if err != nil {
			return err
		}
		if r.Episode != nil {
			eps = append(eps, *r.Episode)
		} else {
			outs[r.Outcome.EpisodeID] = append(outs[r.Outcome.EpisodeID], *r.Outcome)
		}
	}
	ps := mine.Patterns(eps, outs, mine.Options{MinCount: *minCount, MinRequests: *minReq})
	say(out, "recurring failures in %d episodes · at least %d times, in at least %d different requests\n\n", len(eps), *minCount, *minReq)
	if len(ps) == 0 {
		say(out, "none\n")
	} else {
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		say(tw, "  ANSWER\tWHY\tTIMES\tREQUESTS\tSTEP\tERROR\n")
		for _, p := range ps {
			say(tw, "  %s\t%s\t%d\t%d\t%s\t%s\n", p.Kind, p.Why, p.Count, p.Requests, p.Step, orDash(truncate(p.Error, 60)))
		}
		_ = tw.Flush()
		guards := 0
		for _, p := range ps {
			if p.Kind == mine.KindGuard {
				if guards == 0 {
					say(out, "\nguard proposals (for you to approve in Helix; nothing is active):\n")
				}
				guards++
				say(out, "  • %s  (%d times, e.g. %q)\n", p.Proposal, p.Count, truncate(p.Example, 70))
			}
		}
		if guards == 0 {
			say(out, "\nno guard candidates: a guard needs a person undoing or declining the same kind of step repeatedly\n")
		}
	}
	say(out, "\nguard: a person undid or declined it · lesson: it failed the same way (advice, for the reflector)\n"+
		"excluded: refused with nobody asked (an unattended run refuses every confirmation)\n")
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		return writeFile(filepath.Join(*outDir, "patterns.json"), func(w io.Writer) error { return writeJSON(w, ps) })
	}
	return nil
}
