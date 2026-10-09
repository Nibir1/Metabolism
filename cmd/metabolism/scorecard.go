package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/audit"
	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/hosts/coder"
	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/mine"
	"github.com/Nibir1/metabolism/parametric"
	"github.com/Nibir1/metabolism/types"
)

// cmdScorecard gathers the evidence for every open exit criterion and
// judges it (audit, D-032): the summary `make collect` ends with.
func cmdScorecard(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("scorecard", flag.ContinueOnError)
	fs.SetOutput(out)
	real := fs.String("real", defaultJournal(), "the journal of real Helix use")
	auto := fs.String("auto", filepath.Join(".runs", "auto", "journal.db"), "the automated runs' journal")
	labels := fs.String("labels", filepath.Join(".runs", "labels", "comparisons.ndjson"), "the pooled comparisons to label")
	benchJ := fs.String("bench", filepath.Join(".runs", "bench", "journal.db"), "the second host's journal")
	exercises := fs.String("exercises", defaultExercises, "the public benchmark's exercises")
	trainDir := fs.String("train", defaultTrainDir, "the adapter registry (Phase 7)")
	plantedDir := fs.String("planted", filepath.Join(".runs", "planted"), "planted wrong-lesson results (Phase 2)")
	root := fs.String("root", ".", "the Metabolism checkout (for committed results and decisions.md)")
	outDir := fs.String("out", "", "write summary.md and scorecard.json here")
	stageOnly := fs.Bool("stage", false, "print only where real use stands: none, baseline or learning")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ev, err := gatherEvidence(ctx, *real, *auto, *labels, *root, time.Now())
	if err != nil {
		return err
	}
	ev.Benchmark, ev.SecondHost, err = gatherBench(ctx, *benchJ, *exercises)
	if err != nil {
		return err
	}
	ev.Retention, err = gatherRetention(*trainDir)
	if err != nil {
		return err
	}
	planted, err := readPlanted(*plantedDir)
	if err != nil {
		return err
	}
	p := plantedTally(planted)
	ev.Planted = &p
	if err != nil {
		return err
	}
	if *stageOnly {
		say(out, "%s\n", audit.StageOf(ev.Real))
		return nil
	}
	rep := audit.Evaluate(ev)
	md := audit.Markdown(rep)
	say(out, "%s", md)
	if *outDir == "" {
		return nil
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*outDir, "summary.md"), []byte(md), 0o644); err != nil {
		return err
	}
	return writeFile(filepath.Join(*outDir, "scorecard.json"), func(w io.Writer) error { return writeJSON(w, rep) })
}

func gatherEvidence(ctx context.Context, realPath, autoPath, labelsPath, root string, now time.Time) (audit.Evidence, error) {
	ev := audit.Evidence{Now: now}
	if _, err := os.Stat(realPath); err == nil {
		r, err := gatherReal(ctx, realPath, now)
		if err != nil {
			return ev, fmt.Errorf("real journal: %w", err)
		}
		ev.Real = r
	}
	if _, err := os.Stat(autoPath); err == nil {
		j, err := openExisting(ctx, autoPath)
		if err != nil {
			return ev, err
		}
		ds, err := bench.Load(ctx, j)
		_ = j.Close()
		if err != nil {
			return ev, err
		}
		ev.AutoEpisodes = len(ds.Episodes)
		ev.Thinking = bench.MeasureThinking(ds.Episodes, ds.Outcomes, bench.DeepSeekFlashOffPeak)
	}
	// Transfer is measured wherever lessons were tested: both journals.
	ev.Transfer = &audit.Transfer{}
	for _, p := range []string{realPath, autoPath} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		j, err := openExisting(ctx, p)
		if err != nil {
			return ev, err
		}
		m, err := metabolome.Load(ctx, j)
		_ = j.Close()
		if err != nil {
			return ev, err
		}
		for _, l := range m.Lessons() {
			if len(l.Sources) > 0 {
				ev.LessonsLearned++
			}
		}
		t := transferTally(m)
		ev.Transfer.Lessons += t.Lessons
		ev.Transfer.Helped += t.Helped
		ev.Transfer.Harmed += t.Harmed
	}
	ev.Transfer.Decided = ev.Transfer.Lessons >= audit.MinTransferLessons
	l, err := gatherLabels(labelsPath)
	if err != nil {
		return ev, err
	}
	ev.Labels = l
	if _, err := os.Stat(filepath.Join(root, "bench", "results", "baseline-helix", "curve.txt")); err == nil {
		ev.BaselineCommitted = true
	}
	if data, err := os.ReadFile(filepath.Join(root, "docs", "decisions.md")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			// The gate itself is D-010; the decision is its own entry.
			if strings.HasPrefix(line, "### D-") && strings.Contains(strings.ToLower(line), "phase 3 go/no-go decision") {
				ev.GoNoGoRecorded = true
			}
		}
	}
	return ev, nil
}

func gatherReal(ctx context.Context, path string, now time.Time) (audit.Real, error) {
	var r audit.Real
	j, err := openExisting(ctx, path)
	if err != nil {
		return r, err
	}
	defer func() { _ = j.Close() }()
	ds, err := bench.Load(ctx, j)
	if err != nil {
		return r, err
	}
	r.Episodes = len(ds.Episodes)
	r.Metrics = bench.Compute(ds.Episodes, ds.Outcomes)

	var off, on []types.Episode
	for _, ep := range ds.Episodes {
		if len(ep.Exposure) == 0 && len(ep.Withheld) == 0 {
			off = append(off, ep)
		} else {
			on = append(on, ep)
		}
	}
	r.LessonsOff = len(off)
	if len(off) > 0 {
		first, last := off[0].StartedAt, off[0].StartedAt
		for _, ep := range off {
			first = minTime(first, ep.StartedAt)
			last = maxTime(last, ep.StartedAt)
		}
		r.LessonsOffSpan = last.Sub(first)
	}
	if audit.StageOf(r) == audit.StageLearning {
		rec := bench.EvaluateRecall("real", bench.Dataset{Episodes: off, Outcomes: ds.Outcomes}, bench.RecallConfig{})
		r.Recall = &rec
	}
	r.WeeksWithLessons, r.CostFirstWeek, r.CostLastWeek = weeklyCost(on, ds.Outcomes, now)

	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return r, err
	}
	r.CreditDecisions = creditDecisions(m)
	for _, p := range mine.Patterns(ds.Episodes, ds.Outcomes, mine.Options{}) {
		if p.Kind == mine.KindGuard {
			r.Guards++
		}
	}
	return r, nil
}

// weeklyCost is model calls per finished task in the first and the last
// full week of lessons-on use, and how many full weeks there are.
func weeklyCost(on []types.Episode, outs map[types.ID][]types.Outcome, now time.Time) (int, float64, float64) {
	if len(on) == 0 {
		return 0, 0, 0
	}
	start := on[0].StartedAt
	for _, ep := range on {
		start = minTime(start, ep.StartedAt)
	}
	week := 7 * 24 * time.Hour
	full := int(now.Sub(start) / week)
	if full == 0 {
		return 0, 0, 0
	}
	byWeek := make([][]types.Episode, full)
	for _, ep := range on {
		if w := int(ep.StartedAt.Sub(start) / week); w < full {
			byWeek[w] = append(byWeek[w], ep)
		}
	}
	return full, bench.Compute(byWeek[0], outs).ModelCallsPerFinished, bench.Compute(byWeek[full-1], outs).ModelCallsPerFinished
}

// creditDecisions counts lifecycle moves the ledger made on live evidence:
// to accepted, eliminated or superseded, justified by credit, contradiction
// or redundancy evidence.
func creditDecisions(m *metabolome.Metabolome) int {
	n := 0
	for _, l := range m.Lessons() {
		e, _ := m.Get(l.ID)
		live := map[types.ID]bool{}
		for _, ev := range e.Evidence {
			switch ev.Kind {
			case types.EvidenceCredit, types.EvidenceContradiction, types.EvidenceRedundancy:
				live[ev.ID] = true
			}
		}
		for _, mv := range e.Events {
			if mv.To != types.StateAccepted && mv.To != types.StateEliminated && mv.To != types.StateSuperseded {
				continue
			}
			if slices.ContainsFunc(mv.Evidence, func(id types.ID) bool { return live[id] }) {
				n++
			}
		}
	}
	return n
}

// gatherBench reads the second host's journal: its lessons-off runs are
// the public benchmark's baseline, and its lessons show the loop running.
func gatherBench(ctx context.Context, path, dir string) (*audit.Benchmark, *audit.SecondHost, error) {
	b, sh := &audit.Benchmark{}, &audit.SecondHost{}
	if exs, err := coder.Load(dir); err == nil {
		b.Tasks = len(exs)
	}
	if _, err := os.Stat(path); err != nil {
		return b, sh, nil
	}
	j, err := openExisting(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = j.Close() }()
	ds, err := bench.Load(ctx, j)
	if err != nil {
		return nil, nil, err
	}
	covered := map[string]bool{}
	passed := 0
	for _, ep := range ds.Episodes {
		sh.Episodes++
		if len(ep.Exposure) > 0 || len(ep.Withheld) > 0 {
			continue
		}
		b.Runs++
		covered[ep.Attrs["exercise"]] = true
		if types.Succeeded(ep, ds.Outcomes[ep.ID]) {
			passed++
		}
	}
	b.Covered = len(covered)
	if b.Runs > 0 {
		b.Passed = float64(passed) / float64(b.Runs)
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return nil, nil, err
	}
	for _, l := range m.Lessons() {
		e, _ := m.Get(l.ID)
		if slices.ContainsFunc(e.Evidence, func(ev types.Evidence) bool { return ev.Kind == types.EvidenceNutrientTest }) {
			sh.LessonsTested++
		}
	}
	sh.LessonsDecided = creditDecisions(m)
	return b, sh, nil
}

// gatherRetention reads the latest trained adapter's evaluation.
func gatherRetention(dir string) (*audit.Retention, error) {
	s, err := parametric.Registry{Dir: dir}.Load()
	if err != nil {
		return nil, err
	}
	v, ok := s.Latest()
	if !ok {
		return &audit.Retention{}, nil
	}
	ev := *v.Eval
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(v.Path), "eval.json")); err == nil {
		_ = json.Unmarshal(data, &ev) // the copy written after the rollback check
	}
	return &audit.Retention{Trained: true, OldTasks: ev.OldTasks, Retained: ev.Retained,
		GainVsText: ev.Gain, TokensVsText: ev.TokensRatio, RolledBack: ev.RolledBack}, nil
}

// gatherLabels reads the pooled comparisons and the labels given so far.
func gatherLabels(path string) (audit.Labels, error) {
	var l audit.Labels
	comps, err := readNDJSON[comparisonRecord](path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	l.Pooled = len(comps)
	labels, err := readNDJSON[labelRecord](path + ".labels")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return l, err
	}
	done := map[string]string{}
	for _, lr := range labels {
		done[string(lr.Lesson)+"/"+string(lr.Episode)] = lr.Label
	}
	// The judge in use is measured by its model's re-judging of each
	// labelled pair (crosscheck -labelled); a pair it has not re-judged
	// counts only if the judge in use judged it in the first place.
	again := map[string]types.Preference{}
	if xs, err := readNDJSON[crossRecord](crossPath(path, currentJudgeModel)); err == nil {
		for _, x := range xs {
			again[string(x.Lesson)+"/"+string(x.Episode)] = x.SecondSays
		}
		l.Judge = currentJudgeModel + " (the model in " + currentJudge + ")"
	} else {
		l.Judge = currentJudge
	}
	letters := map[string]int{}
	var ls []bench.Labelled
	for _, c := range comps {
		k := string(c.Lesson) + "/" + string(c.Episode)
		label, ok := done[k]
		if !ok {
			continue
		}
		says, judged := again[k]
		if !judged && c.Judge == currentJudge {
			says, judged = c.JudgePrefer, true
		}
		if !judged {
			continue
		}
		letters[label]++
		human := judge.Pair{WithIsA: c.WithIsA}.Verdict(label, 1, "").Prefer
		ls = append(ls, bench.Labelled{Human: human, Judge: says})
	}
	l.Labelled = len(ls)
	for _, k := range letters {
		if l.Labelled >= 10 && float64(k)/float64(l.Labelled) >= 0.9 {
			l.Uniform = true
		}
	}
	l.Agreement = bench.Score(ls)
	return l, nil
}

// cmdPool adds the judged comparisons of every automated day (and any other
// comparisons files given) to the labelling pool, once each: the pool is
// what `metabolism label` shows, and labels are kept by lesson and episode.
func cmdPool(_ context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("pool", flag.ContinueOnError)
	fs.SetOutput(out)
	to := fs.String("to", filepath.Join(".runs", "labels", "comparisons.ndjson"), "the pool")
	judgeName := fs.String("judge", "", "pool only this judge's comparisons (default: any; a label is about the pair, not the judge)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	have, err := readNDJSON[comparisonRecord](*to)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	seen := map[string]bool{}
	for _, c := range have {
		seen[string(c.Lesson)+"/"+string(c.Episode)] = true
	}
	var add []comparisonRecord
	for _, path := range fs.Args() {
		comps, err := readNDJSON[comparisonRecord](path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, c := range comps {
			// The judge's view of a pair whose plans were the same is not a
			// judgement anyone could check.
			if slices.Equal(c.PlanA, c.PlanB) || (*judgeName != "" && c.Judge != *judgeName) {
				continue
			}
			k := string(c.Lesson) + "/" + string(c.Episode)
			if !seen[k] {
				seen[k] = true
				add = append(add, c)
			}
		}
	}
	// Shuffled by a hash of each pair, so the comparisons labelled first are
	// not all from one day or one lesson.
	slices.SortFunc(add, func(a, b comparisonRecord) int {
		return strings.Compare(pairHash(a), pairHash(b))
	})
	if err := os.MkdirAll(filepath.Dir(*to), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(*to, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, c := range add {
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	say(out, "labelling pool: %d new comparison(s), %d in all, at %s\n", len(add), len(have)+len(add), *to)
	return nil
}

// currentJudge is the judge the nutrient test uses (D-024, D-029, D-033),
// and currentJudgeModel its model, which re-judges labelled pairs to
// measure it against people (crosscheck -labelled).
const (
	currentJudge      = "outcome+deepseek"
	currentJudgeModel = "llm:deepseek/deepseek-flash"
)

func pairHash(c comparisonRecord) string {
	h := sha256.Sum256([]byte(string(c.Lesson) + "/" + string(c.Episode)))
	return hex.EncodeToString(h[:8])
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
