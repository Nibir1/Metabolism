package bench

import (
	"fmt"
	"math"

	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/types"
)

// Thinking is Phase 5's planning-mode measurement (D-031): does the host's
// planner need to reason before every plan? Two questions, the same shape as
// 5a's stop condition:
//
//   - Share: how much of what planning costs is reasoning? Measured on the
//     provider's own token reports (wire v4), priced, over turns that
//     thought. Below StopShare there is too little to save.
//   - No harm: on turns the host's coin put in the "off" arm, is success no
//     more than ThinkingMargin below the "on" arm, with P ≥ ThinkingSure?
//     And are they cheaper?
//
// The comparison reuses the credit ledger's statistics (D-020, D-030), with
// thinking off as the treated arm: Exposed is "off", Withheld is "on". Cost
// is the turn's priced tokens rather than its model calls, since turning
// thinking off saves tokens, not calls; the log-cost sums hold
// log(1 + micro-dollars).
type Thinking struct {
	Episodes int `json:"episodes"` // episodes with reported tokens
	Flipped  int `json:"flipped"`  // of which the coin put in an arm

	// Planner rounds, and how many came with a token report.
	Plans         int `json:"plans"`
	ReportedPlans int `json:"reported_plans"`

	// What planning cost on turns that thought (the "on" arm, and turns no
	// coin was flipped for), in tokens and priced.
	Planner        types.Tokens `json:"planner_tokens"`
	PlannerCost    Cost         `json:"planner_cost"`
	ReasoningShare float64      `json:"reasoning_share"` // of the planner's priced cost

	On  Arm `json:"on"`
	Off Arm `json:"off"`

	PNotWorse float64 `json:"p_not_worse"` // P(off ≥ on − ThinkingMargin)
	CostBF    float64 `json:"cost_bayes_factor"`
	Cheaper   bool    `json:"off_cheaper"`

	PassesShare bool   `json:"passes_share"`
	Verdict     string `json:"verdict"`
}

// Arm is one side of the coin.
type Arm struct {
	Turns     int          `json:"turns"`
	Succeeded int          `json:"succeeded"`
	Calls     int          `json:"model_calls"`
	Tokens    types.Tokens `json:"tokens"`
	Cost      Cost         `json:"cost"`
}

// Rate is the arm's success rate.
func (a Arm) Rate() float64 { return ratio(a.Succeeded, a.Turns) }

// PerTurn is the arm's mean priced cost per turn, in dollars.
func (a Arm) PerTurn() float64 {
	if a.Turns == 0 {
		return 0
	}
	return a.Cost.Total() / float64(a.Turns)
}

// Cost is priced tokens, in dollars, split by what they paid for.
type Cost struct {
	CacheHit  float64 `json:"cache_hit"`  // prompt tokens served from the cache
	CacheMiss float64 `json:"cache_miss"` // the rest of the prompt
	Answer    float64 `json:"answer"`     // completion tokens that were not reasoning
	Reasoning float64 `json:"reasoning"`
}

// Total is the whole cost.
func (c Cost) Total() float64 { return c.CacheHit + c.CacheMiss + c.Answer + c.Reasoning }

func (c Cost) add(o Cost) Cost {
	return Cost{c.CacheHit + o.CacheHit, c.CacheMiss + o.CacheMiss, c.Answer + o.Answer, c.Reasoning + o.Reasoning}
}

// Price is dollars per million tokens. Reasoning is billed as completion.
type Price struct {
	CacheHit, CacheMiss, Output float64
}

// DeepSeekFlashOffPeak is DeepSeek-V4.1-Flash outside peak hours, the only
// hours automated days run in (api-docs.deepseek.com/quick_start/pricing,
// read 2026-10-08). Peak doubles every price, which leaves every share
// unchanged.
var DeepSeekFlashOffPeak = Price{CacheHit: 0.003, CacheMiss: 0.15, Output: 0.6}

// Of prices a token count.
func (p Price) Of(t types.Tokens) Cost {
	m := 1e-6
	return Cost{
		CacheHit:  float64(t.CacheHit) * p.CacheHit * m,
		CacheMiss: float64(t.Prompt-t.CacheHit) * p.CacheMiss * m,
		Answer:    float64(t.Completion-t.Reasoning) * p.Output * m,
		Reasoning: float64(t.Reasoning) * p.Output * m,
	}
}

// The bars of the planning-mode test (D-031).
const (
	ThinkingMargin = 0.05 // success may fall at most 5 points with thinking off
	ThinkingSure   = 0.95 // ... with this posterior probability
	ThinkingMinArm = 30   // turns per arm before the no-harm half is judged
)

// MeasureThinking reads episodes and their outcomes.
func MeasureThinking(eps []types.Episode, outs map[types.ID][]types.Outcome, price Price) Thinking {
	var th Thinking
	var tally ledger.Tally
	for _, ep := range eps {
		if ep.Usage.Tokens == nil {
			continue
		}
		th.Episodes++
		thought := ep.Thinking != types.ThinkingOff
		for _, p := range ep.Plans {
			th.Plans++
			if p.Tokens == nil {
				continue
			}
			th.ReportedPlans++
			if thought {
				th.Planner = th.Planner.Add(*p.Tokens)
			}
		}
		if ep.Thinking == "" {
			continue
		}
		th.Flipped++
		arm := &th.On
		off := ep.Thinking == types.ThinkingOff
		if off {
			arm = &th.Off
		}
		ok := types.Succeeded(ep, outs[ep.ID])
		cost := price.Of(*ep.Usage.Tokens)
		arm.Turns++
		arm.Calls += ep.Usage.ModelCalls
		arm.Tokens = arm.Tokens.Add(*ep.Usage.Tokens)
		arm.Cost = arm.Cost.add(cost)
		y := math.Log1p(cost.Total() * 1e6)
		if off {
			tally.Exposed++
			tally.ExposedLogCalls += y
			tally.ExposedLogCalls2 += y * y
			if ok {
				arm.Succeeded++
				tally.ExposedSuccesses++
			}
		} else {
			tally.Withheld++
			tally.WithheldLogCalls += y
			tally.WithheldLogCalls2 += y * y
			if ok {
				arm.Succeeded++
				tally.WithheldSuccesses++
			}
		}
	}
	th.PlannerCost = price.Of(th.Planner)
	if total := th.PlannerCost.Total(); total > 0 {
		th.ReasoningShare = th.PlannerCost.Reasoning / total
	}
	th.PassesShare = th.ReasoningShare >= StopShare
	th.PNotWorse = tally.NotWorse(ThinkingMargin)
	th.CostBF = tally.CostBayesFactor()
	th.Cheaper = tally.Cheaper()
	th.Verdict = th.verdict()
	return th
}

func (th Thinking) verdict() string {
	switch {
	case th.Episodes == 0:
		return "no episodes with reported tokens yet (recording wire v4, from 2026-10-08)"
	case th.ReasoningShare > 0 && !th.PassesShare:
		return fmt.Sprintf("stop: reasoning is %.0f%% of planner cost, under the %.0f%% bar", 100*th.ReasoningShare, 100*StopShare)
	case th.Flipped == 0:
		return "share measured; no coin-flipped turns yet (metabolism auto -thinking-flip)"
	case th.On.Turns < ThinkingMinArm || th.Off.Turns < ThinkingMinArm:
		return fmt.Sprintf("not enough turns yet: %d off, %d on (need %d each)", th.Off.Turns, th.On.Turns, ThinkingMinArm)
	case th.PNotWorse < ThinkingSure:
		return fmt.Sprintf("not shown harmless: P(success with thinking off ≥ on − %.0f points) is %.2f, under %.2f",
			100*ThinkingMargin, th.PNotWorse, ThinkingSure)
	case !th.Cheaper:
		return "harmless but not cheaper with thinking off"
	}
	return fmt.Sprintf("passes: thinking off costs %.0f%% less per turn at P(not worse) %.2f (cost Bayes factor %.3g)",
		100*(1-th.Off.PerTurn()/th.On.PerTurn()), th.PNotWorse, th.CostBF)
}
