package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Nibir1/metabolism/adapters/ndjson"
	"github.com/Nibir1/metabolism/deliver"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
	"github.com/Nibir1/metabolism/versions"
)

// The versioned metabolome (Phase 4d, D-028): snapshot, versions, diff,
// rollback and forget.

func cmdSnapshot(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	note := fs.String("note", "", "why this version is worth naming")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: metabolism snapshot [-note TEXT] NAME")
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	t, err := versions.Snapshot(ctx, j, fs.Arg(0), *note, time.Now())
	if err != nil {
		return err
	}
	say(out, "named version %q: the journal up to record %d (fingerprint %s)\n", t.Name, t.Seq, t.Fingerprint[:12])
	return nil
}

func cmdVersions(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("versions", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	tags, err := versions.Tags(ctx, j)
	if err != nil {
		return err
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	last, err := j.LastSeq(ctx)
	if err != nil {
		return err
	}
	say(out, "now: record %d, %d lesson(s)\n\n", last, len(m.Lessons()))
	if len(tags) == 0 {
		say(out, "no named versions yet (metabolism snapshot NAME)\n")
	} else {
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		say(tw, "  NAME\tRECORD\tTAKEN\tREPRODUCES\tNOTE\n")
		for _, t := range tags {
			past, err := metabolome.LoadAsOf(ctx, j, t.Seq)
			if err != nil {
				return err
			}
			same := "yes"
			if versions.Fingerprint(past) != t.Fingerprint {
				same = "no: a forget since removed records"
			}
			say(tw, "  %s\t%d\t%s\t%s\t%s\n", t.Name, t.Seq, t.At.Local().Format("2006-01-02 15:04"), same, orDash(t.Note))
		}
		_ = tw.Flush()
	}
	if fg := m.Forgets(); len(fg) > 0 {
		say(out, "\nforgotten with proof:\n")
		for _, f := range fg {
			say(out, "  %s  %s by %s: %d record(s)  %s\n", f.ID, f.At.Local().Format("2006-01-02 15:04"), f.By, len(f.Targets), f.Reason)
		}
	}
	return nil
}

func cmdDiff(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return errors.New("usage: metabolism diff FROM [TO]   (a version name, a record number, or now; TO defaults to now)")
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	to := "now"
	if fs.NArg() == 2 {
		to = fs.Arg(1)
	}
	a, _, err := versions.At(ctx, j, fs.Arg(0))
	if err != nil {
		return err
	}
	b, _, err := versions.At(ctx, j, to)
	if err != nil {
		return err
	}
	changes := versions.Diff(a, b)
	if len(changes) == 0 {
		say(out, "no lesson changed state between %s and %s\n", fs.Arg(0), to)
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  LESSON\t%s\t%s\tTEXT\n", strings.ToUpper(fs.Arg(0)), strings.ToUpper(to))
	for _, c := range changes {
		say(tw, "  %s\t%s\t%s\t%s\n", c.Lesson.ID, orDash(string(c.From)), c.To, truncate(c.Lesson.Text, 60))
	}
	return tw.Flush()
}

func cmdRollback(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	apply := fs.Bool("apply", false, "record the rollback (without it, only show what it would do)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: metabolism rollback [-apply] VERSION   (a version name or record number)")
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	past, seq, err := versions.At(ctx, j, fs.Arg(0))
	if err != nil {
		return err
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	steps := versions.PlanRollback(past, m)
	if len(steps) == 0 {
		say(out, "the lessons delivered now are the ones delivered at %s: nothing to roll back\n", fs.Arg(0))
		return nil
	}
	say(out, "rollback to %s (record %d): what is delivered goes back to what was delivered then\n\n", fs.Arg(0), seq)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	moves := 0
	for _, s := range steps {
		if s.To == "" {
			say(tw, "  %s\t%s, stays\t%s\n", s.Lesson.ID, s.From, s.Why)
			continue
		}
		moves++
		say(tw, "  %s\t%s → %s\t%s\n", s.Lesson.ID, s.From, s.To, truncate(s.Lesson.Text, 60))
	}
	_ = tw.Flush()
	if moves == 0 {
		say(out, "\nnothing to roll back\n")
		return nil
	}
	say(out, "\nA lesson brought back returns to trial, on a coin flip, and earns acceptance again.\n")
	if !*apply {
		say(out, "nothing was recorded (add -apply)\n")
		return nil
	}
	before, err := versions.Rollback(ctx, j, m, steps, seq, fs.Arg(0), time.Now())
	if err != nil {
		return err
	}
	say(out, "rolled back; the version before it is named %q, so `metabolism rollback -apply %s` undoes this\n", before.Name, before.Name)
	say(out, "run `metabolism export` so Helix delivers the restored lessons\n")
	return nil
}

func cmdForget(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("forget", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	var episodes, lessons multi
	fs.Var(&episodes, "episode", "an episode to forget, with its outcomes (repeatable)")
	fs.Var(&lessons, "lesson", "a lesson to forget, with its events and evidence (repeatable)")
	match := fs.String("match", "", "forget every episode and lesson whose records contain this text (a pasted key, a name)")
	keep := fs.Bool("keep-lessons", false, "keep lessons learned from a forgotten episode, eliminated, instead of forgetting them too")
	reason := fs.String("reason", "", "why (kept in the audit record)")
	recording := fs.String("recording", defaultHelixFile(), "the host's recording to purge too (none: leave it)")
	lessonsFile := fs.String("lessons-file", defaultLessonsFile(), "the delivery file to rewrite if it holds a forgotten lesson (none: leave it)")
	verify := fs.Bool("verify", false, "check that nothing forgotten is left in the journal or the recording")
	apply := fs.Bool("apply", false, "delete (without it, only show what would be deleted)")
	if err := fs.Parse(args); err != nil {
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
	if *verify {
		return verifyForgets(ctx, j, m, *recording, out)
	}
	if len(episodes)+len(lessons) == 0 && *match == "" {
		return errors.New("name what to forget: -episode ID, -lesson ID or -match TEXT")
	}
	req := versions.ForgetRequest{Match: *match, KeepLessons: *keep}
	for _, e := range episodes {
		req.Episodes = append(req.Episodes, types.ID(e))
	}
	for _, l := range lessons {
		req.Lessons = append(req.Lessons, types.ID(l))
	}
	plan, err := versions.PlanForget(ctx, j, m, req)
	if versions.ErrNothingToForget(err) {
		say(out, "nothing matches: no record would be forgotten\n")
		return nil
	}
	if err != nil {
		return err
	}
	say(out, "forget: %d episode(s), %d lesson(s), %d record(s) in all\n", len(plan.Episodes), len(plan.Lessons), len(plan.Targets))
	for _, id := range plan.Episodes {
		say(out, "  episode %s\n", id)
	}
	for _, l := range plan.Lessons {
		why := ""
		if containsID(plan.Learned, l.ID) {
			why = "  (learned from a forgotten episode)"
		}
		say(out, "  lesson  %s %s%s\n", l.ID, truncate(l.Text, 60), why)
	}
	for _, l := range plan.Eliminate {
		say(out, "  keep, eliminated: %s %s\n", l.ID, truncate(l.Text, 60))
	}
	if !*apply {
		say(out, "\nnothing was deleted (add -apply and -reason)\n")
		return nil
	}
	if strings.TrimSpace(*reason) == "" {
		return errors.New("-reason is required: it is kept in the audit record")
	}
	f, err := versions.Forget(ctx, j, m, plan, *reason, "user", time.Now())
	if err != nil {
		return err
	}
	say(out, "\ndeleted %d record(s); audit record %s keeps each one's SHA-256, not its content\n", len(f.Targets), f.ID)
	if *recording != "none" {
		st, err := ndjson.Purge(*recording, plan.IDs(), *match)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return fmt.Errorf("the journal is purged, the recording is not: %w", err)
		default:
			say(out, "recording: %d line(s) removed from %d file(s)\n", st.Removed, len(st.Files))
		}
	}
	if *lessonsFile != "none" && len(plan.Lessons) > 0 {
		if _, err := os.Stat(*lessonsFile); err == nil {
			book, err := ledger.Credit(ctx, j, m, time.Now(), ledger.Rule{}, nil)
			if err != nil {
				return err
			}
			if err := deliver.Write(*lessonsFile, deliver.Build(m, book.Lessons, time.Now())); err != nil {
				return err
			}
			say(out, "delivery file rewritten: %s\n", *lessonsFile)
		}
	}
	say(out, "copies elsewhere (backups, .runs/ journals) are not touched; `metabolism forget -verify` checks the journal and the recording\n")
	return nil
}

// verifyForgets checks every forget record: none of its targets is in the
// journal, and none is mentioned in the host's recording.
func verifyForgets(ctx context.Context, j ports.Journal, m *metabolome.Metabolome, recording string, out io.Writer) error {
	ids := map[types.ID]bool{}
	for _, f := range m.Forgets() {
		for _, t := range f.Targets {
			ids[t.ID] = true
		}
	}
	if len(ids) == 0 {
		say(out, "nothing has been forgotten in this journal\n")
		return nil
	}
	left := 0
	for r, err := range j.Scan(ctx, types.JournalQuery{}) {
		if err != nil {
			return err
		}
		if id := recordID(r); ids[id] {
			left++
		}
	}
	say(out, "%d forget record(s), %d record(s) forgotten\n", len(m.Forgets()), len(ids))
	say(out, "  journal: %d forgotten record(s) still present\n", left)
	bad := left > 0
	if recording != "none" {
		files, err := ndjson.Mentions(recording, ids)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		say(out, "  recording: %d file(s) still mention a forgotten record %v\n", len(files), files)
		bad = bad || len(files) > 0
	}
	if bad {
		return errors.New("forgotten records remain; run forget again on them, or purge the files named")
	}
	return nil
}

func recordID(r types.Record) types.ID {
	switch {
	case r.Episode != nil:
		return r.Episode.ID
	case r.Outcome != nil:
		return r.Outcome.ID
	case r.Lesson != nil:
		return r.Lesson.ID
	case r.Event != nil:
		return r.Event.ID
	case r.Evidence != nil:
		return r.Evidence.ID
	case r.Tag != nil:
		return r.Tag.ID
	case r.Forget != nil:
		return r.Forget.ID
	}
	return ""
}

func containsID(ids []types.ID, id types.ID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// multi is a repeatable string flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }
