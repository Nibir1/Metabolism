package ledger

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/Nibir1/metabolism/types"
)

// tallyOf builds a tally from per-turn (calls, success) in each arm.
func tallyOf(delivered, withheld [][2]int) Tally {
	var t Tally
	for _, x := range delivered {
		t.Exposed++
		t.ExposedSuccesses += x[1]
		t.ExposedCalls += x[0]
		t.addCost(true, x[0])
	}
	for _, x := range withheld {
		t.Withheld++
		t.WithheldSuccesses += x[1]
		t.WithheldCalls += x[0]
		t.addCost(false, x[0])
	}
	return t
}

// exact gives n turns of which k succeed.
func exact(n, k int, calls func() int) [][2]int {
	out := make([][2]int, n)
	for i := range out {
		ok := 0
		if i < k {
			ok = 1
		}
		out[i] = [2]int{calls(), ok}
	}
	return out
}

func turns(r *rand.Rand, n int, calls func() int, pSuccess float64) [][2]int {
	out := make([][2]int, n)
	for i := range out {
		ok := 0
		if r.Float64() < pSuccess {
			ok = 1
		}
		out[i] = [2]int{calls(), ok}
	}
	return out
}

// spread draws model calls around a mean, as a varying planner does.
func spread(r *rand.Rand, mean float64) func() int {
	return func() int { return max(1, int(math.Round(mean+r.NormFloat64()*mean/3))) }
}

func TestCostBayesFactorSeesADifferenceAndNoDifference(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	same := tallyOf(turns(r, 60, spread(r, 5), 0.95), turns(r, 60, spread(r, 5), 0.95))
	if bf := same.CostBayesFactor(); bf >= 1 {
		t.Errorf("no difference: cost BF %.3g, want < 1", bf)
	}
	cheaper := tallyOf(turns(r, 60, spread(r, 3), 0.95), turns(r, 60, spread(r, 5), 0.95))
	if bf := cheaper.CostBayesFactor(); bf < 1000 || !cheaper.Cheaper() {
		t.Errorf("3 calls against 5: cost BF %.3g, cheaper %v", bf, cheaper.Cheaper())
	}
	// No spread at all, as on the simulator: finite, and decisive.
	flat := tallyOf(turns(r, 20, func() int { return 1 }, 1), turns(r, 10, func() int { return 4 }, 1))
	if bf := flat.CostBayesFactor(); math.IsInf(bf, 0) || math.IsNaN(bf) || bf < 30 {
		t.Errorf("1 call against 4, no spread: cost BF %v", bf)
	}
	if bf := (Tally{}).CostBayesFactor(); bf != 1 {
		t.Errorf("no turns: %v", bf)
	}
}

func TestNotWorseAgainstSimulation(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	cases := []Tally{
		{Exposed: 40, ExposedSuccesses: 38, Withheld: 40, WithheldSuccesses: 38},
		{Exposed: 40, ExposedSuccesses: 30, Withheld: 40, WithheldSuccesses: 38},
		{Exposed: 10, ExposedSuccesses: 10, Withheld: 30, WithheldSuccesses: 25},
	}
	beta := func(a, b float64) float64 { // by gamma draws
		x, y := gamma(r, a), gamma(r, b)
		return x / (x + y)
	}
	for _, c := range cases {
		hits := 0
		const n = 200000
		for range n {
			pe := beta(1+float64(c.ExposedSuccesses), 1+float64(c.Exposed-c.ExposedSuccesses))
			pw := beta(1+float64(c.WithheldSuccesses), 1+float64(c.Withheld-c.WithheldSuccesses))
			if pe > pw-0.05 {
				hits++
			}
		}
		want, got := float64(hits)/n, c.NotWorse(0.05)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("%+v: NotWorse %.4f, simulation %.4f", c, got, want)
		}
	}
}

// gamma draws Gamma(a, 1) for integer a, as a sum of exponentials.
func gamma(r *rand.Rand, a float64) float64 {
	s := 0.0
	for range int(a) {
		s += r.ExpFloat64()
	}
	return s
}

// Under "no difference", a ledger settled every 10 turns for 600 turns must
// not reach the acceptance bar much more often than 1 in 30, whatever the
// planner's spread: the property that lets the user settle whenever they
// like (D-020, D-030).
func TestCostBarHoldsUnderRepeatedLooks(t *testing.T) {
	if testing.Short() {
		t.Skip("simulates many ledgers")
	}
	r := rand.New(rand.NewPCG(5, 6))
	looping := func() int { // mostly short turns, now and then a long loop
		if r.Float64() < 0.1 {
			return 8 + r.IntN(12)
		}
		return 1 + r.IntN(4)
	}
	for name, calls := range map[string]func() int{"normal spread": spread(r, 5), "occasional loops": looping} {
		const runs = 1000
		crossed := 0
		for range runs {
			var tl Tally
			for turn := 1; turn <= 600; turn++ {
				c := calls()
				if r.IntN(2) == 0 {
					tl.Exposed++
					tl.addCost(true, c)
				} else {
					tl.Withheld++
					tl.addCost(false, c)
				}
				if turn%10 == 0 && tl.Exposed >= 20 && tl.Withheld >= 10 && tl.CostBayesFactor() >= 30 {
					crossed++
					break
				}
			}
		}
		rate := float64(crossed) / runs
		t.Logf("%s: the bar of 30 was crossed in %.1f%% of no-difference ledgers (bound about %.1f%%)", name, 100*rate, 100.0/30)
		if rate > 1.0/30*1.5 {
			t.Errorf("%s: crossed in %.1f%%", name, 100*rate)
		}
	}
}

// The rule: cheaper and not worse is accepted; cheaper but worse is not;
// costlier and no better is eliminated; costlier but clearly better is a
// fair trade and accepted on success.
func TestCostDecisions(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	rule := Rule{}.Defaults()
	trial := types.Lesson{State: types.StateTrial}
	cases := []struct {
		name string
		t    Tally
		want types.LessonState
	}{
		{"cheaper, not worse", tallyOf(exact(150, 144, spread(r, 3)), exact(150, 143, spread(r, 5))), types.StateAccepted},
		{"cheaper, a little worse", tallyOf(exact(150, 132, spread(r, 3)), exact(150, 143, spread(r, 5))), ""},
		{"cheaper, too few turns to know", tallyOf(exact(40, 38, spread(r, 3)), exact(40, 38, spread(r, 5))), ""},
		{"costlier, no better", tallyOf(exact(120, 114, spread(r, 7)), exact(120, 114, spread(r, 4))), types.StateEliminated},
		{"costlier, clearly better", tallyOf(exact(120, 118, spread(r, 7)), exact(120, 60, spread(r, 4))), types.StateAccepted},
		{"the same", tallyOf(exact(80, 76, spread(r, 5)), exact(80, 76, spread(r, 5))), ""},
	}
	for _, c := range cases {
		d := decide(trial, c.t, rule)
		if d.To != c.want {
			t.Errorf("%s: to %q, want %q: %s", c.name, d.To, c.want, d.Reason)
		}
	}
	rule.WithoutCost = true
	if d := decide(trial, cases[0].t, rule); d.To != "" {
		t.Errorf("without cost, a cheaper lesson moved: %s", d.Reason)
	}
}
