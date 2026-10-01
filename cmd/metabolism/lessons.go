package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/digest"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
	"github.com/Nibir1/metabolism/verify"
)

// Phase 2a wires only the simulator's reflector and host and the heuristic
// judge. Real ones arrive as adapters (Ollama and DeepSeek reflectors, Helix
// plan-only replay, a Jev judge); the flags exist now so the commands do not
// change shape when they do.

func pickReflector(name string) (ports.Reflector, error) {
	switch name {
	case "sim", "helixsim":
		return helixsim.Reflector{}, nil
	}
	return nil, fmt.Errorf("-reflector %q is not available yet (have: sim)", name)
}

func pickHost(name string) (ports.Host, error) {
	switch name {
	case "sim", "helixsim":
		return helixsim.Host{}, nil
	}
	return nil, fmt.Errorf("-host %q is not available yet (have: sim)", name)
}

func pickJudge(name string) (ports.Judge, error) {
	switch name {
	case "heuristic":
		return judge.Heuristic{}, nil
	}
	return nil, fmt.Errorf("-judge %q is not available yet (have: heuristic)", name)
}

func parseDay(flagName, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("-%s %q: want YYYY-MM-DD", flagName, s)
	}
	return t, nil
}

func cmdDigest(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("digest", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	refl := fs.String("reflector", "sim", "what proposes lessons (sim)")
	since := fs.String("since", "", "only episodes started on or after this day (YYYY-MM-DD)")
	until := fs.String("until", "", "only episodes started before this day (YYYY-MM-DD)")
	maxLessons := fs.Int("max", 10, "most lessons one run may record")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := pickReflector(*refl)
	if err != nil {
		return err
	}
	from, err := parseDay("since", *since)
	if err != nil {
		return err
	}
	to, err := parseDay("until", *until)
	if err != nil {
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
	batch, err := digest.Batch(ctx, j, from, to)
	if err != nil {
		return err
	}
	res, err := digest.Run(ctx, m, r, batch, digest.Config{MaxLessons: *maxLessons})
	if err != nil {
		return err
	}
	say(out, "%s reflected on %d episodes: %d proposed, %d recorded\n\n", res.Reflector, res.Batch, res.Proposed, res.Recorded)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, d := range res.Decisions {
		note := d.Reason
		if d.Lesson != "" {
			note = string(d.Lesson)
		}
		say(tw, "  %s\t%s\t%s\n", d.State, truncate(d.Text, 70), note)
	}
	_ = tw.Flush()
	if res.Recorded > 0 {
		say(out, "\nnext: metabolism verify   (the nutrient test)\n")
	}
	return nil
}

func cmdVerify(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	hostName := fs.String("host", "sim", "what replays episodes without side effects (sim)")
	judgeName := fs.String("judge", "heuristic", "what compares replays (heuristic)")
	only := fs.String("lesson", "", "test one lesson (default: every candidate and quarantined lesson)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, err := pickHost(*hostName)
	if err != nil {
		return err
	}
	jd, err := pickJudge(*judgeName)
	if err != nil {
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
	var ids []types.ID
	if *only != "" {
		ids = []types.ID{types.ID(*only)}
	} else {
		for _, l := range m.Lessons(types.StateCandidate, types.StateQuarantined) {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) == 0 {
		say(out, "nothing to test: no candidate or quarantined lessons (run metabolism digest first)\n")
		return nil
	}
	v := &verify.Verifier{Host: host, Judge: jd, History: h}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  result\tmove\tW/L/T\treg\tlesson\n")
	for _, id := range ids {
		rep, err := v.Test(ctx, m, id)
		if err != nil {
			return err
		}
		e, _ := m.Get(id)
		ev := rep.Evidence
		say(tw, "  %s\t%s → %s\t%d/%d/%d\t%d\t%s\n", ev.Result, rep.From, rep.To,
			ev.Wins, ev.Losses, ev.Ties, ev.Regressions, truncate(e.Lesson.Text, 60))
	}
	_ = tw.Flush()
	say(out, "\njudge: %s · details: metabolism lessons -id <ID>\n", jd.Name())
	return nil
}

func cmdLessons(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("lessons", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	state := fs.String("state", "", "only lessons in this state")
	id := fs.String("id", "", "show one lesson: its sources, history and evidence")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *state != "" && !types.LessonState(*state).Valid() {
		return fmt.Errorf("-state %q is not a lesson state", *state)
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
	if *id != "" {
		return lessonWhy(m, types.ID(*id), out)
	}
	var ls []types.Lesson
	if *state != "" {
		ls = m.Lessons(types.LessonState(*state))
	} else {
		ls = m.Lessons()
	}
	counts := map[types.LessonState]int{}
	for _, l := range m.Lessons() {
		counts[l.State]++
	}
	var parts []string
	for _, s := range []types.LessonState{types.StateCandidate, types.StateQuarantined, types.StateTrial,
		types.StateAccepted, types.StateSuperseded, types.StateEliminated} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s, counts[s]))
		}
	}
	if len(parts) == 0 {
		say(out, "no lessons yet (metabolism digest proposes them)\n")
		return nil
	}
	say(out, "%s\n\n", strings.Join(parts, " · "))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, l := range ls {
		say(tw, "  %s\t%s\t%s\t%s\n", l.ID, l.State, l.Kind, truncate(l.Text, 70))
	}
	return tw.Flush()
}

func lessonWhy(m *metabolome.Metabolome, id types.ID, out io.Writer) error {
	e, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no lesson %s", id)
	}
	l := e.Lesson
	say(out, "lesson %s · %s · %s · %s\n  %s\n", l.ID, l.State, l.Kind, scopeString(l.Scope), l.Text)
	if l.Rationale != "" {
		say(out, "  why proposed: %s\n", l.Rationale)
	}
	say(out, "  learned from %d episode(s), provenance %s, origin %s\n", len(l.Sources), l.Provenance, orDash(l.Origin))
	say(out, "\nhistory:\n")
	for _, ev := range e.Events {
		from := string(ev.From)
		if from == "" {
			from = "∅"
		}
		say(out, "  %s  %s → %s  %s\n", ev.At.UTC().Format(time.DateTime), from, ev.To, ev.Reason)
	}
	if len(e.Evidence) > 0 {
		say(out, "\nevidence:\n")
		for _, ev := range e.Evidence {
			say(out, "  %s  %s %s by %s: %d wins, %d losses, %d ties, %d regressions over %d episodes\n",
				ev.At.UTC().Format(time.DateTime), ev.Kind, ev.Result, orDash(ev.Judge),
				ev.Wins, ev.Losses, ev.Ties, ev.Regressions, len(ev.Episodes))
		}
	}
	return nil
}

func cmdNutrientEval(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("nutrient-eval", flag.ContinueOnError)
	fs.SetOutput(out)
	seeds := fs.Int("seeds", 5, "simulated histories to plant lessons in")
	count := fs.Int("count", 400, "episodes per history")
	outDir := fs.String("out", "", "directory to write nutrient.json (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ss []uint64
	for i := 1; i <= *seeds; i++ {
		ss = append(ss, uint64(i))
	}
	r, err := bench.EvaluateNutrient(ctx, ss, *count, verify.Config{})
	if err != nil {
		return err
	}
	say(out, "nutrient test on simulated Helix · %d histories × %d episodes · judge %s\n\n", r.Seeds, r.Count, r.Judge)
	say(out, "  wrong lessons rejected    %d / %d  (%s)   exit bar: ≥ 90%%\n", r.WrongRejected, r.WrongTotal, pct(r.WrongRejectRate))
	say(out, "  correct lessons accepted  %d / %d  (%s)\n", r.CorrectAccepted, r.CorrectTotal, pct(r.CorrectAcceptRate))
	say(out, "  inconclusive              %d\n\n", r.Inconclusive)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	seen := map[string]bool{}
	for _, row := range r.Rows {
		if seen[row.Text] {
			continue
		}
		seen[row.Text] = true
		agree := 0
		total := 0
		for _, x := range r.Rows {
			if x.Text == row.Text {
				total++
				if (x.Good && x.Result == types.ResultPass) || (!x.Good && x.Result == types.ResultFail) {
					agree++
				}
			}
		}
		kind := "wrong"
		if row.Good {
			kind = "good"
		}
		say(tw, "  %s\t%d/%d right\t%s\t%s\n", kind, agree, total, truncate(row.Text, 60), row.Why)
	}
	_ = tw.Flush()
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(*outDir, "nutrient.json"), func(w io.Writer) error { return writeJSON(w, r) }); err != nil {
			return err
		}
		say(out, "\nwrote %s/nutrient.json\n", *outDir)
	}
	return nil
}

func say(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
