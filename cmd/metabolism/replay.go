package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/types"
	"github.com/Nibir1/metabolism/verify"
)

// cmdReplay replays one episode with and without a lesson, shows both plans
// and the judge's verdict, and records nothing. It is the quickest way to see
// the whole loop on real data before there are lessons to test.
func cmdReplay(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	epID := fs.String("episode", "", "episode to replay (required)")
	lessonText := fs.String("lesson", "", "lesson text to replay with (required)")
	hostName := fs.String("host", "helix", "sim or helix")
	helixBin := fs.String("helix-bin", "helix", "helix binary for -host helix")
	replayDir := fs.String("replay-dir", "", "directory Helix plans the replay in (default: this one)")
	rounds := fs.Int("rounds", defaultRounds, "planner rounds; between rounds only read-only file steps run (1: first plan only)")
	judgeName := fs.String("judge", "jev", "heuristic, ollama, deepseek, jev")
	judgeModel := fs.String("judge-model", "", "model for the judge")
	allowRemote := fs.Bool("allow-remote", false, "allow remote replay and judging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *epID == "" || strings.TrimSpace(*lessonText) == "" {
		return errors.New("-episode and -lesson are required")
	}
	if err := checkPair(*hostName, *judgeName); err != nil {
		return err
	}
	rem := remote{allowed: *allowRemote, out: out}
	host, err := pickHost(*hostName, *helixBin, *replayDir, *rounds, rem)
	if err != nil {
		return err
	}
	defer closeHost(host)
	jd, err := pickJudge(*judgeName, *judgeModel, rem)
	if err != nil {
		return err
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	h, err := verify.LoadHistory(ctx, j)
	if err != nil {
		return err
	}
	ep, outs, ok := h.Episode(types.ID(*epID))
	if !ok {
		return fmt.Errorf("no episode %s", *epID)
	}
	lesson := types.Lesson{ID: types.NewID(time.Now()), Text: *lessonText}
	without, err := host.Replay(ctx, ep, nil)
	if err != nil {
		return err
	}
	with, err := host.Replay(ctx, ep, []types.Lesson{lesson})
	if err != nil {
		return err
	}
	say(out, "request: %s\n", ep.Request.Text)
	printPlan(out, "without the lesson", without)
	printPlan(out, "with the lesson", with)
	if sameReplay(with, without) {
		say(out, "\nverdict: identical plans; the lesson changed nothing here (no judge asked)\n")
		return nil
	}
	v, err := jd.Compare(ctx, ep, outs, with, without)
	if err != nil {
		return err
	}
	say(out, "\nverdict (%s): prefers %s, confidence %.2f: %s\n", jd.Name(), v.Prefer, v.Confidence, v.Reason)
	return nil
}

func printPlan(out io.Writer, title string, r types.ReplayResult) {
	say(out, "\n%s (%d model calls):\n", title, r.Usage.ModelCalls)
	for _, s := range r.Steps {
		line := strings.TrimSpace(s.Tool + " " + s.Action)
		if s.Subject != "" {
			line += ": " + s.Subject
		}
		say(out, "  - %s\n", line)
	}
	if r.Text != "" {
		say(out, "  reply: %s\n", truncate(strings.ReplaceAll(r.Text, "\n", " "), 200))
	}
}

func sameReplay(a, b types.ReplayResult) bool {
	if a.Text != b.Text || len(a.Steps) != len(b.Steps) {
		return false
	}
	for i := range a.Steps {
		if a.Steps[i] != b.Steps[i] {
			return false
		}
	}
	return true
}

// comparisonRecord is one judged comparison saved for blind labelling.
type comparisonRecord struct {
	Lesson          types.ID         `json:"lesson"`
	LessonText      string           `json:"lesson_text"`
	Episode         types.ID         `json:"episode"`
	Regression      bool             `json:"regression_set"`
	Request         string           `json:"request"`
	Original        string           `json:"original_attempt"`
	PlanA           []string         `json:"plan_a"`
	PlanB           []string         `json:"plan_b"`
	WithIsA         bool             `json:"with_is_a"`
	Judge           string           `json:"judge"`
	JudgePrefer     types.Preference `json:"judge_prefer"`
	JudgeConfidence float64          `json:"judge_confidence"`
}

func saveComparisons(path string, l types.Lesson, rep verify.Report, h *verify.History, judgeName string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, c := range rep.Comparisons {
		if !c.Judged {
			continue
		}
		ep, outs, _ := h.Episode(c.Episode)
		pair := judge.NewPair(ep, outs, c.With, c.Without)
		if err := enc.Encode(comparisonRecord{
			Lesson: l.ID, LessonText: l.Text, Episode: c.Episode, Regression: c.Regression, Request: pair.Request, Original: pair.Original,
			PlanA: pair.PlanA, PlanB: pair.PlanB, WithIsA: pair.WithIsA,
			Judge: judgeName, JudgePrefer: c.Verdict.Prefer, JudgeConfidence: c.Verdict.Confidence,
		}); err != nil {
			return err
		}
	}
	return nil
}

type labelRecord struct {
	Lesson  types.ID  `json:"lesson"`
	Episode types.ID  `json:"episode"`
	Label   string    `json:"label"` // a | b | same
	At      time.Time `json:"at"`
}

// cmdLabel shows saved comparisons blind and records a person's judgement,
// or (with -score) measures the judge against those labels.
func cmdLabel(ctx context.Context, args []string, out io.Writer, in io.Reader) error {
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	fs.SetOutput(out)
	from := fs.String("from", "", "comparisons file written by verify -save (required)")
	score := fs.Bool("score", false, "score the judge against the labels instead of labelling")
	redo := fs.Bool("redo", false, "start labelling again; earlier labels are kept in <file>.labels.<time>")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("-from is required")
	}
	comps, err := readNDJSON[comparisonRecord](*from)
	if err != nil {
		return err
	}
	labelsPath := *from + ".labels"
	if *redo {
		if _, err := os.Stat(labelsPath); err == nil {
			kept := labelsPath + "." + time.Now().UTC().Format("20060102T150405")
			if err := os.Rename(labelsPath, kept); err != nil {
				return err
			}
			say(out, "earlier labels kept in %s\n", kept)
		}
	}
	labels, err := readNDJSON[labelRecord](labelsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	done := map[string]string{}
	for _, l := range labels {
		done[string(l.Lesson)+"/"+string(l.Episode)] = l.Label
	}
	if *score {
		return scoreLabels(out, comps, done)
	}

	f, err := os.OpenFile(labelsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	sc := bufio.NewScanner(in)
	todo := 0
	for _, c := range comps {
		if _, ok := done[string(c.Lesson)+"/"+string(c.Episode)]; !ok {
			todo++
		}
	}
	say(out, "%d comparisons, %d to label. You will not see which plan had the lesson, or what the judge said.\n", len(comps), todo)
	n := 0
	for _, c := range comps {
		key := string(c.Lesson) + "/" + string(c.Episode)
		if _, ok := done[key]; ok {
			continue
		}
		n++
		say(out, "\n━━ %d/%d ━━\nrequest:  %s\noriginal: %s\n\nplan A:\n", n, todo, c.Request, c.Original)
		for _, l := range c.PlanA {
			say(out, "  - %s\n", l)
		}
		say(out, "plan B:\n")
		for _, l := range c.PlanB {
			say(out, "  - %s\n", l)
		}
		var label string
		for label == "" {
			say(out, "\nwhich plan better accomplishes the request? [a/b/s(ame)/q(uit)] ")
			if !sc.Scan() {
				return sc.Err()
			}
			switch strings.ToLower(strings.TrimSpace(sc.Text())) {
			case "a":
				label = "a"
			case "b":
				label = "b"
			case "s", "same":
				label = "same"
			case "q", "quit":
				say(out, "saved; run the same command to continue\n")
				return nil
			}
		}
		if err := enc.Encode(labelRecord{Lesson: c.Lesson, Episode: c.Episode, Label: label, At: time.Now().UTC()}); err != nil {
			return err
		}
		done[key] = label
	}
	say(out, "\nall labelled. Score the judge with: metabolism label -from %s -score\n", *from)
	return nil
}

func scoreLabels(out io.Writer, comps []comparisonRecord, done map[string]string) error {
	byJudge := map[string][]bench.Labelled{}
	for _, c := range comps {
		label, ok := done[string(c.Lesson)+"/"+string(c.Episode)]
		if !ok {
			continue
		}
		pair := judge.Pair{WithIsA: c.WithIsA}
		human := pair.Verdict(label, 1, "").Prefer
		byJudge[c.Judge] = append(byJudge[c.Judge], bench.Labelled{Human: human, Judge: c.JudgePrefer})
	}
	if len(byJudge) == 0 {
		say(out, "no labelled comparisons yet: run metabolism label -from <file> first\n")
		return nil
	}
	// A labeller who gives (almost) the same answer every time has not
	// compared anything, and an agreement score against that would measure
	// only how often the judge picked the same letter. The with-lesson plan's
	// position is randomised, so honest labels are not this uniform.
	letters := map[string]int{}
	n := 0
	for _, l := range done {
		letters[l]++
		n++
	}
	for l, k := range letters {
		if n >= 10 && float64(k)/float64(n) >= 0.9 {
			say(out, "%d of %d labels are %q: these labels cannot measure a judge, so no score is given.\n", k, n, l)
			say(out, "Label again with: metabolism label -from <file> -redo\n")
			return nil
		}
	}
	for name, ls := range byJudge {
		a := bench.Score(ls)
		verdict := "meets the Phase 2 bar (≥ 80%)"
		if a.Rate < 0.8 {
			verdict = "below the Phase 2 bar (≥ 80%): improve or replace this judge"
		}
		say(out, "%s: agrees on %d of %d labelled comparisons (%s), Cohen's κ %.2f; %s\n",
			name, a.Agree, a.N, pct(a.Rate), a.Kappa, verdict)
		for k, v := range a.Confused {
			say(out, "  disagreements human→judge %s: %d\n", k, v)
		}
		if a.N < 30 {
			say(out, "  (fewer than 30 labels: treat this as an early reading)\n")
		}
	}
	return nil
}

func readNDJSON[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var v T
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, v)
	}
	return out, sc.Err()
}
