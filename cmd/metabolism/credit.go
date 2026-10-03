package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/adapters/ndjson"
	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/deliver"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// defaultLessonsFile is where Helix reads delivered lessons.
func defaultLessonsFile() string {
	return filepath.Join(filepath.Dir(defaultHelixFile()), "lessons.json")
}

// cmdCredit reports what live turns say about each delivered lesson and,
// with -apply, records the evidence and moves the lessons the rule decides.
func cmdCredit(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("credit", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	apply := fs.Bool("apply", false, "record credit evidence and accept or eliminate the lessons the rule decides")
	maturity := fs.Duration("maturity", time.Hour, "ignore turns younger than this (late outcomes may still arrive)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	j, err := sqlite.Open(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	now := time.Now()
	tallies, err := ledger.Count(ctx, j, now, *maturity, ledger.Eras(m))
	if err != nil {
		return err
	}
	ds, err := ledger.Settle(ctx, m, tallies, ledger.Rule{Maturity: *maturity}, !*apply)
	if err != nil {
		return err
	}
	if len(ds) == 0 {
		say(out, "no trial or accepted lessons: nothing is being delivered yet (metabolism verify moves lessons to trial)\n")
		return nil
	}
	say(out, "credit from live turns · delivered vs withheld by coin flip · since each lesson's last state change\n\n")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  LESSON\tSTATE\tDELIVERED\tWITHHELD\tP(BETTER)\tBF\tCALLS/TURN\tDECISION\n")
	moved := 0
	for _, d := range ds {
		t := d.Tally
		e, w := t.Rate()
		ce, cw := t.Calls()
		decision := "stays"
		if d.To != "" {
			decision = "→ " + string(d.To)
			moved++
		}
		say(tw, "  %s\t%s\t%s of %d\t%s of %d\t%.3f\t%.3g\t%.1f / %.1f\t%s\n",
			d.Lesson.ID, d.Lesson.State, pct(e), t.Exposed, pct(w), t.Withheld,
			t.PBetter(), t.BayesFactor(), ce, cw, decision)
	}
	_ = tw.Flush()
	for _, d := range ds {
		say(out, "\n  %s  %s\n    %s\n", d.Lesson.ID, truncate(d.Lesson.Text, 90), d.Reason)
		if d.Tally.Immature > 0 {
			say(out, "    %d turn(s) younger than %s not counted yet\n", d.Tally.Immature, *maturity)
		}
	}
	switch {
	case moved > 0 && *apply:
		say(out, "\nmoved %d lesson(s); run `metabolism export` so Helix delivers the new states\n", moved)
	case moved > 0:
		say(out, "\n%d lesson(s) would move; nothing was recorded (add -apply)\n", moved)
	}
	say(out, "\nrule (D-020): accept at Bayes factor ≥ 30 for better, eliminate at ≥ 300 for worse,\n"+
		"after ≥ 20 delivered and ≥ 10 withheld turns. Success = the turn ended done with no negative outcome.\n")
	return nil
}

// cmdExport writes the delivery file Helix reads.
func cmdExport(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	to := fs.String("to", defaultLessonsFile(), "delivery file to write (Helix reads ~/.helix/metabolism/lessons.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	j, err := sqlite.Open(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	now := time.Now()
	tallies, err := ledger.Count(ctx, j, now, time.Hour, ledger.Eras(m))
	if err != nil {
		return err
	}
	f := deliver.Build(m, tallies, now)
	if err := deliver.Write(*to, f); err != nil {
		return err
	}
	trial, accepted := 0, 0
	for _, l := range f.Lessons {
		if l.State == string(types.StateAccepted) {
			accepted++
		} else {
			trial++
		}
	}
	say(out, "wrote %d lesson(s) to %s: %d accepted (delivered %.0f%% of turns), %d trial (%.0f%%)\n",
		len(f.Lessons), *to, accepted, 100*deliver.PAccepted, trial, 100*deliver.PTrial)
	say(out, "Helix delivers them only with /lessons on (and /metabolism on, so the turns are recorded)\n")
	return nil
}

// cmdCreditEval runs the ledger on the simulator, where the truth is known.
func cmdCreditEval(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("credit-eval", flag.ContinueOnError)
	fs.SetOutput(out)
	seeds := fs.Int("seeds", 5, "simulated histories")
	turns := fs.Int("turns", 3000, "live turns per history")
	every := fs.Int("settle-every", 100, "settle the ledger every N turns, as a user running credit -apply would")
	outDir := fs.String("out", "", "directory to write credit.json (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ss []uint64
	for i := 1; i <= *seeds; i++ {
		ss = append(ss, uint64(i))
	}
	r, err := bench.EvaluateCredit(ctx, ss, *turns, *every, ledger.Rule{})
	if err != nil {
		return err
	}
	say(out, "credit ledger on simulated Helix · %d histories × %d turns · settled every %d turns\n\n", r.Seeds, r.Turns, r.SettleEvery)
	say(out, "  lessons that help, accepted          %d / %d  (%s)\n", r.HelpAccepted, r.HelpTotal, pct(r.HelpAcceptRate))
	say(out, "  lessons that harm, eliminated        %d / %d  (%s)\n", r.HarmEliminated, r.HarmTotal, pct(r.HarmEliminateRate))
	say(out, "  lessons that change no outcome, moved %d / %d  (eliminated %d)\n\n", r.NeutralWrongMoves, r.NeutralTotal, r.NeutralEliminated)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  SEED\tEXPECT\tFINAL\tAT TURN\tDELIVERED\tWITHHELD\tLESSON\n")
	for _, row := range r.Rows {
		e, w := row.Tally.Rate()
		at := "-"
		if row.At > 0 {
			at = fmt.Sprint(row.At)
		}
		say(tw, "  %d\t%s\t%s\t%s\t%s of %d\t%s of %d\t%s\n", row.Seed, row.Expect, row.Final, at,
			pct(e), row.Tally.Exposed, pct(w), row.Tally.Withheld, truncate(row.Text, 60))
	}
	_ = tw.Flush()
	say(out, "\nSynthetic: the simulator knows which lesson repairs which failure. This checks the ledger's\n"+
		"logic and its false-decision rate under frequent settling, not how fast real lessons earn credit.\n")
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(*outDir, "credit.json"), func(w io.Writer) error { return writeJSON(w, r) }); err != nil {
			return err
		}
		say(out, "\nwrote %s/credit.json\n", *outDir)
	}
	return nil
}

// cmdPoisonEval runs the poisoning suite (docs/poisoning.md).
func cmdPoisonEval(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("poison-eval", flag.ContinueOnError)
	fs.SetOutput(out)
	seeds := fs.Int("seeds", 2, "simulated histories (48 cases each)")
	turns := fs.Int("turns", 1500, "live turns per case")
	outDir := fs.String("out", "", "directory to write poison.json (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ss []uint64
	for i := 1; i <= *seeds; i++ {
		ss = append(ss, uint64(i))
	}
	r, err := bench.EvaluatePoisoning(ctx, ss, *turns)
	if err != nil {
		return err
	}
	say(out, "%s\n", bench.PoisonSummary(r))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  BY GOAL\tCASES\tRECORDED\tPERSISTED\tATTACK SUCCESS\n")
	for _, p := range helixsim.Poisons() {
		g := r.ByGoal[p.Goal]
		say(tw, "  %s\t%d\t%s\t%s\t%s\n", p.Goal, g.Cases, pct(g.Recorded), pct(g.MPSR), pct(g.E2EASR))
	}
	say(tw, "  BY CARRIER\t\t\t\t\n")
	for _, c := range bench.Carriers {
		g := r.ByCarrier[c]
		say(tw, "  %s\t%d\t%s\t%s\t%s\n", c, g.Cases, pct(g.Recorded), pct(g.MPSR), pct(g.E2EASR))
	}
	_ = tw.Flush()
	say(out, "\nSynthetic: simulated Helix, a reflector that writes every poison it is shown, and attacks we\n"+
		"thought of. A pass says these layers stop these attacks, nothing about attacks not in the suite.\n")
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(*outDir, "poison.json"), func(w io.Writer) error { return writeJSON(w, r) }); err != nil {
			return err
		}
	}
	return nil
}

// applyFeedback eliminates the lessons the user forgot in Helix. Lessons
// change only through the metabolome (D-014), so ingest hands the feedback
// here. Feedback already applied, or for a lesson this journal does not
// hold, is reported and skipped.
func applyFeedback(ctx context.Context, j ports.Journal, fbs []ndjson.Feedback, out io.Writer) error {
	if len(fbs) == 0 {
		return nil
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	applied, already, unknown := 0, 0, 0
	for _, fb := range fbs {
		e, ok := m.Get(fb.LessonID)
		switch {
		case !ok:
			unknown++
			continue
		case e.Lesson.State == types.StateEliminated:
			already++
			continue
		}
		at := fb.At
		m.SetClock(func() time.Time { return at })
		reason := fmt.Sprintf("forgotten by the user in Helix (/lessons forget): %s", fb.Reason)
		if err := m.Move(ctx, fb.LessonID, types.StateEliminated, reason); err != nil {
			if errors.Is(err, ports.ErrInvalid) {
				already++
				continue
			}
			return err
		}
		applied++
	}
	say(out, "  feedback: %d lesson(s) forgotten in Helix eliminated", applied)
	if already > 0 {
		say(out, ", %d already applied", already)
	}
	if unknown > 0 {
		say(out, ", %d for lessons this journal does not hold", unknown)
	}
	say(out, "\n")
	if applied > 0 {
		say(out, "  run `metabolism export` to refresh what Helix delivers\n")
	}
	return nil
}
