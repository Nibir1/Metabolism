package bench

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/types"
)

// thinkingEp is a one-round turn in an arm, with its reported tokens.
func thinkingEp(i int, arm string, end types.RunEnd, tok types.Tokens) types.Episode {
	return types.Episode{
		ID:       types.ID(fmt.Sprintf("%028x", i+1)),
		End:      end,
		Thinking: arm,
		Usage:    types.Usage{ModelCalls: 1, Tokens: &tok},
		Plans:    []types.PlanRecord{{Round: 1, Prompt: "full", Calls: 1, Result: "planned", Tokens: &tok}},
	}
}

var (
	thoughtTok = types.Tokens{Prompt: 4600, CacheHit: 3800, Completion: 900, Reasoning: 780, Calls: 1}
	quickTok   = types.Tokens{Prompt: 4600, CacheHit: 3800, Completion: 110, Calls: 1}
)

// Pricing splits a token report by what each part paid for.
func TestPriceOf(t *testing.T) {
	c := DeepSeekFlashOffPeak.Of(thoughtTok)
	want := Cost{CacheHit: 3800 * 0.003e-6, CacheMiss: 800 * 0.15e-6, Answer: 120 * 0.6e-6, Reasoning: 780 * 0.6e-6}
	for _, pair := range [][2]float64{{c.CacheHit, want.CacheHit}, {c.CacheMiss, want.CacheMiss}, {c.Answer, want.Answer}, {c.Reasoning, want.Reasoning}} {
		if math.Abs(pair[0]-pair[1]) > 1e-12 {
			t.Fatalf("cost %+v, want %+v", c, want)
		}
	}
}

// With equal success and cheaper turns off, the test passes; the share is
// measured on turns that thought only.
func TestMeasureThinkingPasses(t *testing.T) {
	var eps []types.Episode
	outs := map[types.ID][]types.Outcome{}
	// Showing "at most 5 points worse" takes a few hundred turns: at 40 per
	// arm and equal rates, P(not worse) is only about 0.77.
	for i := range 400 {
		arm, tok := types.ThinkingOn, thoughtTok
		if i%2 == 1 {
			arm, tok = types.ThinkingOff, quickTok
		}
		end := types.RunDone
		if i%40 == 0 || i%40 == 1 { // one failure in twenty, in both arms
			end = types.RunFailed
		}
		eps = append(eps, thinkingEp(i, arm, end, tok))
	}
	eps = append(eps, types.Episode{Usage: types.Usage{ModelCalls: 3}}) // before wire v4
	th := MeasureThinking(eps, outs, DeepSeekFlashOffPeak)
	if th.Episodes != 400 || th.Flipped != 400 || th.On.Turns != 200 || th.Off.Turns != 200 || th.ReportedPlans != 400 {
		t.Fatalf("counts %+v", th)
	}
	if th.Planner.Reasoning != 200*780 {
		t.Fatalf("planner tokens should be the on arm's only: %+v", th.Planner)
	}
	// 780 reasoning of (3800 hit, 800 miss, 120 answer, 780 reasoning) priced.
	c := DeepSeekFlashOffPeak.Of(thoughtTok)
	if math.Abs(th.ReasoningShare-c.Reasoning/c.Total()) > 1e-9 || !th.PassesShare {
		t.Fatalf("share %.3f, passes %v", th.ReasoningShare, th.PassesShare)
	}
	if th.On.Rate() != th.Off.Rate() || !th.Cheaper || th.PNotWorse < ThinkingSure || th.CostBF < 30 {
		t.Fatalf("arms %+v / %+v, P %.3f, BF %.3g", th.On, th.Off, th.PNotWorse, th.CostBF)
	}
	if !strings.HasPrefix(th.Verdict, "passes") {
		t.Fatalf("verdict %q", th.Verdict)
	}
}

// Thinking off that breaks turns is not shown harmless, however cheap.
func TestMeasureThinkingHarm(t *testing.T) {
	var eps []types.Episode
	for i := range 80 {
		arm, tok, end := types.ThinkingOn, thoughtTok, types.RunDone
		if i%2 == 1 {
			arm, tok = types.ThinkingOff, quickTok
			if i%4 == 1 {
				end = types.RunFailed // half the off turns fail
			}
		}
		eps = append(eps, thinkingEp(i, arm, end, tok))
	}
	th := MeasureThinking(eps, nil, DeepSeekFlashOffPeak)
	if th.PNotWorse > 0.05 || !strings.HasPrefix(th.Verdict, "not shown harmless") {
		t.Fatalf("P %.3f, verdict %q", th.PNotWorse, th.Verdict)
	}
}

// Little reasoning stops the test on the share alone, before any coin.
func TestMeasureThinkingStopsOnShare(t *testing.T) {
	light := types.Tokens{Prompt: 4600, CacheHit: 3000, Completion: 200, Reasoning: 10, Calls: 1}
	th := MeasureThinking([]types.Episode{thinkingEp(0, "", types.RunDone, light)}, nil, DeepSeekFlashOffPeak)
	if th.PassesShare || !strings.HasPrefix(th.Verdict, "stop") {
		t.Fatalf("share %.3f, verdict %q", th.ReasoningShare, th.Verdict)
	}
	if th := MeasureThinking(nil, nil, DeepSeekFlashOffPeak); !strings.HasPrefix(th.Verdict, "no episodes") {
		t.Fatalf("empty verdict %q", th.Verdict)
	}
}
