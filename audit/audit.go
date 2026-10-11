// Package audit is the scorecard: every open exit criterion of the roadmap,
// its bar, what the evidence shows today, and what is still missing
// (D-032). `metabolism scorecard` gathers the evidence; this package only
// judges it, so the bars are in one place and tested.
//
// A criterion is one of:
//   - met: the evidence clears the bar;
//   - short: there is evidence, and it does not clear the bar (yet);
//   - waiting: the evidence does not exist yet (real use, time, labels);
//   - manual: a person decides it (a go/no-go, a publication);
//   - unbuilt: the code that would produce the evidence is not built yet.
//
// Nothing here claims a rubric criterion: vision.md's bars do that, and the
// Phase 9 audit reads this scorecard as its input, not as its verdict.
package audit

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/bench"
)

// Status is where a criterion stands.
type Status string

const (
	Met     Status = "met"
	Short   Status = "short"
	Waiting Status = "waiting"
	Manual  Status = "manual"
	Unbuilt Status = "unbuilt"
)

// Criterion is one exit criterion and how it stands.
type Criterion struct {
	Phase  int    `json:"phase"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Bar    string `json:"bar"`
	Have   string `json:"have"`
	Status Status `json:"status"`
	// Next is what moves it: who does what.
	Next string `json:"next,omitempty"`
}

// Stage is where real use stands: the baseline is collected with lessons
// off; learning starts once it is in.
type Stage string

const (
	StageNone     Stage = "none"     // no real episodes yet
	StageBaseline Stage = "baseline" // collecting the lessons-off baseline
	StageLearning Stage = "learning" // baseline in: digest, verify, deliver, credit
)

// Real is what the user's own Helix use shows, from the real journal.
type Real struct {
	Episodes int `json:"episodes"`
	// LessonsOff counts episodes with no lesson delivered or withheld: the
	// baseline. Span is the time from the first to the last of them.
	LessonsOff     int           `json:"lessons_off"`
	LessonsOffSpan time.Duration `json:"lessons_off_span"`
	Metrics        bench.Metrics `json:"metrics"`
	// Recall is episodic retrieval's skill on the real episodes (nil until
	// there are enough of them).
	Recall *bench.RecallResult `json:"recall,omitempty"`
	// CreditDecisions counts lessons the ledger accepted, eliminated or
	// superseded on live turns.
	CreditDecisions int `json:"credit_decisions"`
	// Guards counts guard candidates the miner found (a person undid or
	// declined the same kind of step).
	Guards int `json:"guard_candidates"`
	// Cost per finished task, first and last full week of use with lessons
	// on (0 when there is no such week).
	CostFirstWeek, CostLastWeek float64
	WeeksWithLessons            int
}

// Labels is the human judge sample.
type Labels struct {
	Pooled    int             `json:"pooled"`    // comparisons available to label
	Labelled  int             `json:"labelled"`  // labelled so far
	Uniform   bool            `json:"uniform"`   // ≥ 90% one letter: unusable
	Agreement bench.Agreement `json:"agreement"` // the judge against them
	Judge     string          `json:"judge"`     // which judge was measured
	// Self is the person against their own earlier labels (label -retest):
	// how much any judge could agree with these labels.
	Self bench.Agreement `json:"self_agreement"`
}

// Evidence is everything the scorecard judges.
type Evidence struct {
	Now  time.Time `json:"now"`
	Real Real      `json:"real"`
	// AutoEpisodes is how many synthetic episodes the automated runs hold.
	AutoEpisodes int `json:"auto_episodes"`
	// LessonsLearned counts lessons a reflector proposed with sources, in
	// every journal (C2).
	LessonsLearned int            `json:"lessons_learned"`
	Thinking       bench.Thinking `json:"thinking"`
	Labels         Labels         `json:"labels"`

	BaselineCommitted bool `json:"baseline_committed"` // bench/results/baseline-helix exists
	GoNoGoRecorded    bool `json:"go_no_go_recorded"`  // a go/no-go entry in decisions.md

	// Later phases fill these in as they are built; nil means unbuilt.
	Benchmark  *Benchmark  `json:"benchmark,omitempty"`
	Planted    *Planted    `json:"planted,omitempty"`
	Transfer   *Transfer   `json:"transfer,omitempty"`
	Retention  *Retention  `json:"retention,omitempty"`
	SecondHost *SecondHost `json:"second_host,omitempty"`
}

// Benchmark is the public benchmark's lessons-off runs (Phase 0, through
// the second host).
type Benchmark struct {
	Tasks   int     `json:"tasks"`   // exercises in the benchmark
	Covered int     `json:"covered"` // of which run at least once with lessons off
	Runs    int     `json:"runs"`    // lessons-off runs
	Passed  float64 `json:"passed"`  // their pass rate
}

// Planted is the nutrient test against planted wrong lessons on real replays.
type Planted struct {
	Planted, Rejected int
}

// Transfer is a lesson learned in one project tried in another (Phase 6).
type Transfer struct {
	Lessons int  `json:"lessons"` // lessons tried on held-out projects
	Helped  int  `json:"helped"`  // passed the nutrient test there
	Harmed  int  `json:"harmed"`  // failed it there
	Decided bool `json:"decided"` // enough lessons to judge
}

// Retention is the parametric tier's forgetting check (Phase 7).
type Retention struct {
	OldTasks     int     `json:"old_tasks"`
	Retained     float64 `json:"retained"` // share of the old-task suite still passed
	GainVsText   float64 `json:"gain_vs_text"`
	TokensVsText float64 `json:"tokens_vs_text"` // adapter tokens per task / text-lesson tokens
	RolledBack   bool    `json:"rolled_back"`
	Trained      bool    `json:"trained"`
}

// SecondHost is the second host's runs (Phase 8).
type SecondHost struct {
	Episodes int `json:"episodes"`
	// LessonsTested counts lessons the nutrient test judged on this host,
	// and LessonsDecided those the ledger then accepted or eliminated: the
	// whole loop, run on a host the engine has no code for.
	LessonsTested  int `json:"lessons_tested"`
	LessonsDecided int `json:"lessons_decided"`
}

// The bars D-032 sets where the roadmap says only "weeks".
const (
	BaselineEpisodes = 300
	BaselineSpan     = 21 * 24 * time.Hour
	MinLabels        = 30
	// TargetLabels is how many labels D-033 asks for before the judge is
	// changed again: at 30, an agreement of 60% could be 45% to 75%.
	TargetLabels = 50
	// D-036: a judge is measured against the labeller's own consistency.
	// It must reach CeilingShare of the labeller's self-agreement and of
	// their κ, measured on at least MinRetest re-labelled pairs.
	CeilingShare = 0.8
	MinRetest    = 20
	// ThinkingJudgedAt is the flipped turns after which the planning-mode
	// measurement was judged (2026-10-11, 683 turns): more days do not
	// change a verdict that did not converge.
	ThinkingJudgedAt = 600
	Endurance        = 90 * 24 * time.Hour
	// MinTransferLessons is how many lessons must be measured on held-out
	// projects before transfer is judged.
	MinTransferLessons = 5
	// PlantedSuite is how many wrong lessons the planted run has (the CLI's
	// list; a test keeps the two equal).
	PlantedSuite = 10
)

// Report is the scorecard.
type Report struct {
	Now      time.Time    `json:"now"`
	Stage    Stage        `json:"stage"`
	Criteria []Criterion  `json:"criteria"`
	Rubric   []RubricItem `json:"rubric"`
	Evidence Evidence     `json:"evidence"`
}

// Count returns how many criteria have each status.
func (r Report) Count() map[Status]int {
	out := map[Status]int{}
	for _, c := range r.Criteria {
		out[c.Status]++
	}
	return out
}

// StageOf says where real use stands.
func StageOf(r Real) Stage {
	switch {
	case r.Episodes == 0:
		return StageNone
	case r.LessonsOff < BaselineEpisodes || r.LessonsOffSpan < BaselineSpan:
		return StageBaseline
	}
	return StageLearning
}

// Evaluate judges the evidence against every open criterion.
func Evaluate(ev Evidence) Report {
	r := Report{Now: ev.Now, Stage: StageOf(ev.Real), Evidence: ev}
	add := func(c Criterion) { r.Criteria = append(r.Criteria, c) }
	re := ev.Real
	useHelix := "use Helix for your own work with /metabolism on (lessons off until the baseline is in)"

	// Phase 0.
	{
		c := Criterion{Phase: 0, ID: "0.real-baseline", Name: "Real Helix baseline, lessons off",
			Bar:  fmt.Sprintf("≥ %d lessons-off episodes over ≥ %d days, curves committed", BaselineEpisodes, int(BaselineSpan.Hours()/24)),
			Have: fmt.Sprintf("%d episodes over %s", re.LessonsOff, days(re.LessonsOffSpan))}
		switch {
		case re.LessonsOff == 0:
			c.Status, c.Next = Waiting, useHelix
		case StageOf(re) == StageBaseline:
			c.Status, c.Next = Short, useHelix
		case !ev.BaselineCommitted:
			c.Status, c.Next = Short, "make helix-baseline, then commit bench/results/baseline-helix"
		default:
			c.Status = Met
		}
		add(c)
	}
	add(benchmarkCriterion(ev.Benchmark))

	// Phase 1.
	{
		c := Criterion{Phase: 1, ID: "1.outcome-labels", Name: "Outcome labels on real runs",
			Bar: "≥ 90% of real episodes have an outcome", Have: fmt.Sprintf("%.0f%% of %d", 100*re.Metrics.OutcomeCoverage, re.Episodes)}
		c.Status, c.Next = byRate(re.Episodes > 0, re.Metrics.OutcomeCoverage >= 0.9), useHelix
		add(c)
	}
	{
		c := Criterion{Phase: 1, ID: "1.recall-real", Name: "Episodic retrieval measured on the real baseline",
			Bar: "skill measured on the real baseline (no gain is allowed; it is the data point)"}
		switch {
		case re.Recall == nil || r.Stage != StageLearning:
			c.Status, c.Have, c.Next = Waiting, "no real baseline yet", useHelix
		default:
			c.Status, c.Have = Met, fmt.Sprintf("skill %.2f on %d episodes", re.Recall.Skill, re.Recall.Evaluated)
		}
		add(c)
	}

	// Phase 2.
	add(plantedCriterion(ev.Planted))
	{
		l := ev.Labels
		c := Criterion{Phase: 2, ID: "2.judge-agreement", Name: "Judge agrees with your labels",
			Bar: fmt.Sprintf("D-036: agreement and κ each ≥ %.0f%% of the labeller's own (label -retest), on ≥ %d labels", 100*CeilingShare, MinLabels),
			Have: fmt.Sprintf("%s: %d labelled of %d pooled; agreement %.0f%%, κ %.2f",
				orNone(l.Judge), l.Labelled, l.Pooled, 100*l.Agreement.Rate, l.Agreement.Kappa)}
		if l.Self.N > 0 {
			c.Have += fmt.Sprintf("; you agree with yourself on %.0f%% of %d (κ %.2f), so the bar is %.0f%% and κ %.2f",
				100*l.Self.Rate, l.Self.N, l.Self.Kappa, 100*CeilingShare*l.Self.Rate, CeilingShare*l.Self.Kappa)
		}
		switch {
		case l.Uniform:
			c.Status, c.Next = Short, "the labels are ≥ 90% one letter and cannot measure a judge: label again (make collect asks)"
		case l.Labelled < MinLabels:
			c.Status, c.Next = Waiting, fmt.Sprintf("label %d more comparisons (make collect asks; about 2 minutes each)", MinLabels-l.Labelled)
		case l.Self.N < MinRetest:
			c.Status, c.Next = Short, fmt.Sprintf("re-label %d pairs to find the ceiling: bin/metabolism label -from .runs/labels/comparisons.ndjson -retest %d (D-035)", MinRetest, MinRetest)
		case l.Agreement.Rate >= CeilingShare*l.Self.Rate && l.Agreement.Kappa >= CeilingShare*l.Self.Kappa:
			c.Status = Met
		case l.Labelled < TargetLabels:
			c.Status, c.Next = Short, fmt.Sprintf("label %d more to narrow the estimate (make collect asks; D-033)", TargetLabels-l.Labelled)
		default:
			c.Status, c.Next = Short, "improve the judge toward the labeller's own consistency (D-036)"
		}
		add(c)
	}

	// Phase 3.
	{
		c := Criterion{Phase: 3, ID: "3.real-credit", Name: "Lessons decided by live real turns",
			Bar: "≥ 1 lesson accepted, eliminated or superseded by real credit", Have: fmt.Sprintf("%d decisions", re.CreditDecisions)}
		switch {
		case re.CreditDecisions > 0:
			c.Status = Met
		case r.Stage != StageLearning:
			c.Status, c.Next = Waiting, "the baseline first; then make collect digests, verifies and exports real lessons, and you turn /lessons on"
		default:
			c.Status, c.Next = Short, "keep using Helix with /lessons on; credit needs ≥ 20 delivered and ≥ 10 withheld relevant turns per lesson"
		}
		add(c)
	}
	add(Criterion{Phase: 3, ID: "3.go-no-go", Name: "Go/no-go recorded", Bar: "a go/no-go entry in decisions.md",
		Have: yesNo(ev.GoNoGoRecorded, "recorded", "not yet"), Status: manualOr(ev.GoNoGoRecorded),
		Next: "decided together once real credit decisions exist"})
	add(Criterion{Phase: 3, ID: "3.publication", Name: "Publication out", Bar: "a write-up published",
		Have: "not yet", Status: Manual, Next: "after the go/no-go"})

	// Phase 4.
	{
		c := Criterion{Phase: 4, ID: "4.drift-real", Name: "Drift N on real data", Bar: "N set from real credit decisions",
			Have: fmt.Sprintf("%d real credit decisions", re.CreditDecisions)}
		c.Status, c.Next = Waiting, "needs real credit decisions first"
		if re.CreditDecisions >= 3 {
			c.Status, c.Next = Short, "set N from the real decisions (a code change made from the scorecard)"
		}
		add(c)
	}

	// Phase 5.
	add(thinkingCriterion(ev.Thinking))
	{
		c := Criterion{Phase: 5, ID: "5c.guard", Name: "An approved guard lowers failures", Bar: "a guard approved, then credited (BF ≥ 30)",
			Have: fmt.Sprintf("%d guard candidates in real use", re.Guards)}
		c.Status, c.Next = Waiting, "real use: a guard needs you to undo or decline the same kind of step more than once"
		if re.Guards > 0 {
			c.Status, c.Next = Short, "a guard candidate exists: the approval flow is next"
		}
		add(c)
	}
	{
		c := Criterion{Phase: 5, ID: "5.cost-curve", Name: "Cost per finished task falls", Bar: "last full week with lessons below the first",
			Have: fmt.Sprintf("%d weeks with lessons", re.WeeksWithLessons)}
		switch {
		case re.WeeksWithLessons < 2:
			c.Status, c.Next = Waiting, "at least two weeks of real use with /lessons on"
		case re.CostLastWeek < re.CostFirstWeek:
			c.Status = Met
			c.Have = fmt.Sprintf("%.1f → %.1f model calls per finished task", re.CostFirstWeek, re.CostLastWeek)
		default:
			c.Status = Short
			c.Have = fmt.Sprintf("%.1f → %.1f model calls per finished task", re.CostFirstWeek, re.CostLastWeek)
		}
		add(c)
	}

	// Phase 6.
	add(transferCriterion(ev.Transfer))

	// Phase 7.
	add(retentionCriterion(ev.Retention))

	// Phase 8.
	add(enduranceCriterion(re, r.Stage))
	add(secondHostCriterion(ev.SecondHost))

	// Phase 9.
	add(Criterion{Phase: 9, ID: "9.audit", Name: "The 100% audit", Bar: "every criterion above met, scored against vision.md",
		Have: fmt.Sprintf("%d of %d met", countMet(r.Criteria), len(r.Criteria)), Status: auditStatus(r.Criteria),
		Next: "the last step: an outside reviewer if possible"})
	r.Rubric = Rubric(r)
	return r
}

func benchmarkCriterion(b *Benchmark) Criterion {
	c := Criterion{Phase: 0, ID: "0.public-benchmark", Name: "Public benchmark baseline",
		Bar: "every exercise of the public benchmark run with learning off"}
	switch {
	case b == nil:
		c.Status, c.Have, c.Next = Unbuilt, "no runner yet", "built with the second host (Phase 8, D-032)"
	case b.Tasks == 0:
		c.Status, c.Have, c.Next = Waiting, "benchmark not downloaded", "make bench-setup (once), then make collect BENCH=n"
	case b.Covered < b.Tasks:
		c.Status, c.Have, c.Next = Short, fmt.Sprintf("%d of %d exercises run, %.0f%% passed", b.Covered, b.Tasks, 100*b.Passed),
			fmt.Sprintf("make collect BENCH=%d (lessons stay off until every exercise has run)", b.Tasks-b.Covered)
	default:
		c.Status, c.Have = Met, fmt.Sprintf("all %d exercises, %d runs, %.0f%% passed", b.Tasks, b.Runs, 100*b.Passed)
	}
	return c
}

func plantedCriterion(p *Planted) Criterion {
	c := Criterion{Phase: 2, ID: "2.planted-real", Name: "Planted wrong lessons rejected on real replays", Bar: "≥ 90% rejected"}
	switch {
	case p == nil:
		c.Status, c.Have, c.Next = Unbuilt, "simulator only (25/25)", "the planted run on real replays is built in a later step"
	case p.Planted == 0:
		c.Status, c.Have, c.Next = Waiting, "no planted run yet", "make collect PLANTED=3 (paid replays in a copy of the journal, off-peak)"
	default:
		rate := float64(p.Rejected) / float64(p.Planted)
		c.Have = fmt.Sprintf("%d of %d kept out of trial (%.0f%%)", p.Rejected, p.Planted, 100*rate)
		c.Status, c.Next = Short, fmt.Sprintf("test all %d planted lessons (make collect PLANTED=3)", PlantedSuite)
		switch {
		case p.Planted >= PlantedSuite && rate >= 0.9:
			c.Status, c.Next = Met, ""
		case rate < 0.9:
			c.Next = "a planted wrong lesson passed: see .runs/planted, then tighten the nutrient test"
		}
	}
	return c
}

func thinkingCriterion(th bench.Thinking) Criterion {
	c := Criterion{Phase: 5, ID: "5b.planning-mode", Name: "Planning mode: thinking off is harmless and cheaper",
		Bar: "reasoning ≥ 15% of planner cost; off ≤ 5 points worse (P ≥ 0.95) and cheaper", Have: th.Verdict}
	switch {
	case th.Episodes == 0 || th.Flipped == 0:
		c.Status, c.Next = Waiting, "make collect DAYS=1 (an automated day with the thinking coin flip)"
	case strings.HasPrefix(th.Verdict, "passes"):
		c.Status, c.Next = Met, "the habit itself: decide per request, credited live (5b)"
	case strings.HasPrefix(th.Verdict, "stop"):
		c.Status, c.Next = Short, "negative result: write it up beside 5a's"
	case th.On.Turns+th.Off.Turns >= ThinkingJudgedAt:
		c.Status, c.Next = Short, "judged negative after the flipped days (habits.md, D-031): cheaper, not shown harmless; no more days needed"
	default:
		c.Status, c.Next = Short, fmt.Sprintf("more flipped days: %d off and %d on turns so far, about 100–200 each needed",
			th.Off.Turns, th.On.Turns)
	}
	return c
}

func transferCriterion(t *Transfer) Criterion {
	c := Criterion{Phase: 6, ID: "6.transfer", Name: "Lessons transfer to held-out projects",
		Bar: fmt.Sprintf("C4: on ≥ %d lessons tested on projects they were not learned in, more help than harm, and at least one helps", MinTransferLessons)}
	switch {
	case t == nil:
		c.Status, c.Have, c.Next = Unbuilt, "not built", "Phase 6 (D-032)"
	case !t.Decided:
		c.Status, c.Have, c.Next = Waiting, fmt.Sprintf("%d of %d lessons measured", t.Lessons, MinTransferLessons),
			"make collect TRANSFER=1 (paid replays: a few lessons per run)"
	default:
		c.Have = fmt.Sprintf("%d of %d helped on a held-out project, %d harmed", t.Helped, t.Lessons, t.Harmed)
		c.Status, c.Next = Short, "no transfer shown: more lessons, or lessons scoped to their project"
		if t.Helped > t.Harmed && t.Helped > 0 {
			c.Status, c.Next = Met, ""
		}
	}
	return c
}

func retentionCriterion(rt *Retention) Criterion {
	c := Criterion{Phase: 7, ID: "7.retention", Name: "Trained adapter keeps old skills and beats text lessons",
		Bar: "retention ≥ 95%; gains ≥ text lessons at fewer tokens; rollback works"}
	switch {
	case rt == nil:
		c.Status, c.Have, c.Next = Unbuilt, "not built", "Phase 7 (D-032: local MLX)"
	case !rt.Trained:
		c.Status, c.Have, c.Next = Waiting, "no adapter trained",
			"make trainer-setup once, then make collect TRAIN=1 (it needs lessons accepted and credited on the second host)"
	default:
		c.Have = fmt.Sprintf("retained %.0f%% of %d old tasks; gain vs text %+.1f points at %.0f%% of the tokens; rollback %s",
			100*rt.Retained, rt.OldTasks, 100*rt.GainVsText, 100*rt.TokensVsText, yesNo(rt.RolledBack, "works", "untested"))
		c.Status = Short
		if rt.Retained >= 0.95 && rt.GainVsText >= 0 && rt.TokensVsText < 1 && rt.RolledBack {
			c.Status = Met
		}
	}
	return c
}

func enduranceCriterion(re Real, stage Stage) Criterion {
	c := Criterion{Phase: 8, ID: "8.endurance", Name: "Curves stable or improving over 3 months of real use",
		Bar:  "≥ 90 days of real use with the engine on, curves stable or improving (C8)",
		Have: fmt.Sprintf("%d weeks with lessons", re.WeeksWithLessons)}
	switch {
	case stage != StageLearning:
		c.Status, c.Next = Waiting, "starts when the baseline is in and /lessons is on"
	case re.WeeksWithLessons*7 < int(Endurance.Hours()/24):
		c.Status, c.Next = Short, "keep using Helix; this one is calendar time"
	case re.CostLastWeek <= re.CostFirstWeek:
		c.Status = Met
	default:
		c.Status, c.Next = Short, "cost per finished task rose over the period: investigate"
	}
	return c
}

func secondHostCriterion(s *SecondHost) Criterion {
	c := Criterion{Phase: 8, ID: "8.second-host", Name: "A second host with no engine changes",
		Bar: "the coding-agent host (D-032) runs the whole loop: lessons digested, tested and credited, with no engine code for it"}
	switch {
	case s == nil:
		c.Status, c.Have, c.Next = Unbuilt, "not built", "Phase 8 (D-032)"
	case s.Episodes == 0:
		c.Status, c.Have, c.Next = Waiting, "no runs", "make bench-setup (once), then make collect BENCH=n"
	case s.LessonsTested == 0:
		c.Status, c.Have, c.Next = Short, fmt.Sprintf("%d episodes, no lesson tested yet", s.Episodes),
			"more runs: make collect BENCH=n digests and tests lessons on them"
	default:
		c.Status, c.Have = Met, fmt.Sprintf("%d episodes, %d lessons tested, %d decided by credit", s.Episodes, s.LessonsTested, s.LessonsDecided)
	}
	return c
}

func byRate(have, ok bool) Status {
	switch {
	case !have:
		return Waiting
	case ok:
		return Met
	}
	return Short
}

func manualOr(done bool) Status {
	if done {
		return Met
	}
	return Manual
}

func countMet(cs []Criterion) int {
	n := 0
	for _, c := range cs {
		if c.Status == Met {
			n++
		}
	}
	return n
}

func auditStatus(cs []Criterion) Status {
	if countMet(cs) == len(cs) {
		return Manual
	}
	return Waiting
}

func orNone(s string) string {
	if s == "" {
		return "no judge yet"
	}
	return s
}

func yesNo(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func days(d time.Duration) string {
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}
