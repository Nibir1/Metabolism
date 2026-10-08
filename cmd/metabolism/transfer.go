package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"text/tabwriter"

	"github.com/Nibir1/metabolism/audit"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
	"github.com/Nibir1/metabolism/verify"
)

// defaultGroupBy finds the project an automated day's request names: the
// synthetic user writes every request with its workspace path (D-023).
const defaultGroupBy = `workspace/([A-Za-z0-9_-]+)`

// cmdTransfer is Phase 6's C4 measurement: each tested lesson's nutrient
// test, run only on episodes from groups (projects) its sources are not
// from. It records transfer evidence and never moves a lesson.
func cmdTransfer(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("transfer", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	groupBy := fs.String("group-by", defaultGroupBy,
		`what groups episodes: "scope" (the host's project scope), or a regular expression whose first group, found in the request, names the project`)
	hostName := fs.String("host", "sim", "what replays episodes without side effects: sim, helix")
	helixBin := fs.String("helix-bin", "helix", "helix binary for -host helix")
	replayDir := fs.String("replay-dir", "", "directory Helix plans replays in")
	rounds := fs.Int("rounds", defaultRounds, "planner rounds per Helix replay")
	judgeName := fs.String("judge", "heuristic", "what compares replays (as verify)")
	judgeModel := fs.String("judge-model", "", "model for an ollama, deepseek or jev judge")
	allowRemote := fs.Bool("allow-remote", false, "allow a remote host or judge")
	maxTest := fs.Int("max", 2, "lessons measured this run: never measured first")
	only := fs.String("lesson", "", "measure one lesson")
	if err := fs.Parse(args); err != nil {
		return err
	}
	group, err := grouper(*groupBy)
	if err != nil {
		return err
	}
	rem := remote{allowed: *allowRemote, out: out}
	if err := checkPair(*hostName, *judgeName); err != nil {
		return err
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	h, err := verify.LoadHistory(ctx, j)
	if err != nil {
		return err
	}
	ids := transferOrder(m, *only)
	if *maxTest > 0 && len(ids) > *maxTest {
		ids = ids[:*maxTest]
	}
	if len(ids) == 0 {
		say(out, "nothing to measure: no trial or accepted lessons\n")
		return printTransfer(out, m)
	}
	host, err := pickHost(*hostName, *helixBin, *replayDir, *rounds, rem)
	if err != nil {
		return err
	}
	defer closeHost(host)
	if *hostName == "helix" {
		rem.notice("%s", replayNotice(*rounds))
	}
	jd, err := pickJudge(*judgeName, *judgeModel, rem)
	if err != nil {
		return err
	}
	if isRemote(*judgeName) {
		rem.notice("each comparison (the request, how it went, and both plans) goes to %s", jd.Name())
	}
	v := &verify.Verifier{Host: host, Judge: jd, History: h}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  result\tfrom → held out\tW/L/T\tworse/better\tlesson\n")
	for _, id := range ids {
		e, _ := m.Get(id)
		own := map[string]bool{}
		for _, src := range e.Lesson.Sources {
			if ep, _, ok := h.Episode(src); ok {
				if g := group(ep); g != "" {
					own[g] = true
				}
			}
		}
		if len(own) == 0 {
			say(tw, "  skipped\t-\t-\t-\t%s (its sources name no group)\n", truncate(e.Lesson.Text, 60))
			continue
		}
		heldSet := map[string]bool{}
		for _, ep := range historyEpisodes(h) {
			if g := group(ep); g != "" && !own[g] {
				heldSet[g] = true
			}
		}
		heldOut := slices.Sorted(maps.Keys(heldSet))
		keep := func(ep types.Episode) bool { return heldSet[group(ep)] }
		rep, err := v.Transfer(ctx, m, id, heldOut, keep)
		switch {
		case errors.Is(err, verify.ErrNoHeldOut), errors.Is(err, ports.ErrCannotReplay):
			say(tw, "  skipped\t%s\t-\t-\t%s (%v)\n", keys(own), truncate(e.Lesson.Text, 60), err)
			continue
		case err != nil && ctx.Err() == nil:
			say(tw, "  error\t%s\t-\t-\t%s (%v)\n", keys(own), truncate(e.Lesson.Text, 60), err)
			continue
		case err != nil:
			return err
		}
		ev := rep.Evidence
		say(tw, "  %s\t%s → %v\t%d/%d/%d\t%d/%d\t%s\n", ev.Result, keys(own), ev.Groups,
			ev.Wins, ev.Losses, ev.Ties, ev.Regressions, ev.RegressionWins, truncate(e.Lesson.Text, 60))
	}
	_ = tw.Flush()
	if err := m.Refresh(ctx); err != nil {
		return err
	}
	return printTransfer(out, m)
}

func printTransfer(out io.Writer, m *metabolome.Metabolome) error {
	t := transferTally(m)
	say(out, "\ntransfer so far (C4): %d lesson(s) measured on held-out projects: %d helped, %d harmed, %d undecided",
		t.Lessons, t.Helped, t.Harmed, t.Lessons-t.Helped-t.Harmed)
	if !t.Decided {
		say(out, " (judged from %d lessons)", audit.MinTransferLessons)
	}
	say(out, "\n")
	return nil
}

// transferTally reads each lesson's latest transfer evidence.
func transferTally(m *metabolome.Metabolome) audit.Transfer {
	var t audit.Transfer
	for _, l := range m.Lessons() {
		e, _ := m.Get(l.ID)
		var last *types.Evidence
		for i := range e.Evidence {
			if e.Evidence[i].Kind == types.EvidenceTransfer {
				last = &e.Evidence[i]
			}
		}
		if last == nil {
			continue
		}
		t.Lessons++
		switch last.Result {
		case types.ResultPass:
			t.Helped++
		case types.ResultFail:
			t.Harmed++
		}
	}
	t.Decided = t.Lessons >= audit.MinTransferLessons
	return t
}

// transferOrder lists tested lessons, never measured first, then oldest.
func transferOrder(m *metabolome.Metabolome, only string) []types.ID {
	if only != "" {
		return []types.ID{types.ID(only)}
	}
	var never, again []types.ID
	for _, l := range m.Lessons(types.StateAccepted, types.StateTrial) {
		e, _ := m.Get(l.ID)
		if slices.ContainsFunc(e.Evidence, func(ev types.Evidence) bool { return ev.Kind == types.EvidenceTransfer }) {
			again = append(again, l.ID)
		} else {
			never = append(never, l.ID)
		}
	}
	return append(never, again...)
}

// grouper turns -group-by into a function naming an episode's group ("" for
// none).
func grouper(spec string) (func(types.Episode) string, error) {
	if spec == "scope" {
		return func(ep types.Episode) string {
			if ep.Scope.Key == "" {
				return ""
			}
			return string(ep.Scope.Level) + ":" + ep.Scope.Key
		}, nil
	}
	re, err := regexp.Compile(spec)
	if err != nil || re.NumSubexp() < 1 {
		return nil, fmt.Errorf("-group-by %q: want \"scope\" or a regular expression with one group", spec)
	}
	return func(ep types.Episode) string {
		if mt := re.FindStringSubmatch(ep.Request.Text); len(mt) > 1 {
			return mt[1]
		}
		return ""
	}, nil
}

func historyEpisodes(h *verify.History) []types.Episode {
	var out []types.Episode
	for _, id := range h.IDs() {
		ep, _, _ := h.Episode(id)
		out = append(out, ep)
	}
	return out
}

func keys(m map[string]bool) string { return fmt.Sprint(slices.Sorted(maps.Keys(m))) }
