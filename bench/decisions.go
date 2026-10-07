package bench

import (
	"slices"

	"github.com/Nibir1/metabolism/types"
)

// Decisions is where a host's model calls went, from episodes recorded with
// decision records (wire v3), and how much of it each Phase 5 candidate
// decision could touch (docs/habits.md, D-031). It is 5a's measurement: the
// stop condition is that no candidate covers StopShare of all calls.
type Decisions struct {
	Episodes int `json:"episodes"` // episodes with decision records
	Older    int `json:"older"`    // episodes recorded before wire v3, left out
	Calls    int `json:"calls"`    // model calls in those episodes

	ByKind map[string]int `json:"by_kind"`

	// Planner calls by cause.
	Plans           int `json:"plans"`
	FirstAttempts   int `json:"first_attempts"`   // one per planning of the first round
	Retries         int `json:"retries"`          // extra calls in any round (retries, smaller prompts)
	FollowUpCalls   int `json:"follow_up_calls"`  // every call of rounds after the first
	SmallerPrompts  int `json:"smaller_prompts"`  // plans produced by the compact or minimal prompt
	FailedPlannings int `json:"failed_plannings"` // plans with no usable result

	CriticRuns    int            `json:"critic_runs"`
	CriticNo      int            `json:"critic_no"`
	Fallbacks     map[string]int `json:"fallbacks"`
	PlanResults   map[string]int `json:"plan_results"`
	FirstTools    map[string]int `json:"first_tools"`
	AnsweredRound int            `json:"answered_rounds"` // follow-up rounds whose plan only answered

	// Planner input characters (plans that record them), and the part the
	// answer-only follow-up rounds took: what an answer-now habit could save
	// by sending a small prompt instead of the full one.
	PlannerInputChars  int `json:"planner_input_chars"`
	AnsweredInputChars int `json:"answered_input_chars"`
	SizedPlans         int `json:"sized_plans"`

	Candidates []Candidate `json:"candidates"`
}

// Candidate is one decision Phase 5 might compile, with the share of all
// model calls it touches. Touching is not saving: whether half of them can
// be saved is the second half of 5a (predictability, offline).
type Candidate struct {
	Name   string  `json:"name"`
	What   string  `json:"what"`
	Calls  int     `json:"calls"`
	Share  float64 `json:"share"`
	Passes bool    `json:"passes_share"`
}

// StopShare is the share of all model calls a candidate decision must touch
// for Phase 5's cost work to go on (D-031).
const StopShare = 0.15

// MeasureDecisions reads episodes and counts.
func MeasureDecisions(eps []types.Episode) Decisions {
	d := Decisions{ByKind: map[string]int{}, Fallbacks: map[string]int{}, PlanResults: map[string]int{}, FirstTools: map[string]int{}}
	answeredFollowUp, retryCalls := 0, 0
	for _, ep := range eps {
		if len(ep.Plans) == 0 && len(ep.Usage.ByKind) == 0 {
			d.Older++
			continue
		}
		d.Episodes++
		d.Calls += ep.Usage.ModelCalls
		for k, n := range ep.Usage.ByKind {
			d.ByKind[k] += n
		}
		if ep.Critic != nil {
			d.CriticRuns++
			if ep.Critic.Verdict == "no" {
				d.CriticNo++
			}
		}
		if ep.Fallback != "" {
			d.Fallbacks[ep.Fallback]++
		}
		for _, p := range ep.Plans {
			d.Plans++
			if p.InputChars > 0 {
				d.SizedPlans++
				d.PlannerInputChars += p.InputChars
				if p.Round > 1 && p.Answered {
					d.AnsweredInputChars += p.InputChars
				}
			}
			d.PlanResults[p.Result]++
			extra := max(p.Calls-1, 0)
			d.Retries += extra
			retryCalls += extra
			if p.Prompt != "" && p.Prompt != "full" {
				d.SmallerPrompts++
			}
			if p.Result != "planned" {
				d.FailedPlannings++
			}
			if p.FirstTool != "" {
				d.FirstTools[p.FirstTool]++
			}
			if p.Round <= 1 {
				d.FirstAttempts += min(p.Calls, 1)
				continue
			}
			d.FollowUpCalls += p.Calls
			if p.Answered {
				d.AnsweredRound++
				answeredFollowUp += p.Calls
			}
		}
	}
	share := func(n int) float64 {
		if d.Calls == 0 {
			return 0
		}
		return float64(n) / float64(d.Calls)
	}
	add := func(name, what string, n int) {
		s := share(n)
		d.Candidates = append(d.Candidates, Candidate{Name: name, What: what, Calls: n, Share: s, Passes: s >= StopShare})
	}
	add("answer now or read more",
		"follow-up rounds that only answered: a small answer-from-what-you-read prompt instead of the full planner",
		answeredFollowUp)
	add("retry level",
		"planner calls spent on retries and smaller prompts: start at the level that usually succeeds",
		retryCalls)
	add("follow-up rounds (upper bound)",
		"every call of rounds after the first: what any habit on the lookup loop could touch at most",
		d.FollowUpCalls)
	slices.SortStableFunc(d.Candidates, func(a, b Candidate) int { return b.Calls - a.Calls })
	return d
}
