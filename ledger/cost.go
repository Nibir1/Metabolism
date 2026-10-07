package ledger

import (
	"fmt"
	"math"
)

// Cost (D-030). Success alone cannot see a lesson that saves work: on the
// first lessons-on days turns succeeded 94 to 100% whether a lesson was
// delivered or not, while model calls per turn differed (3.9 against 5.2
// for one lesson, 4.8 against 4.1 for another). Cost is half of what the
// engine is for (vision.md: cost per good outcome falling while quality
// rises), so the ledger also compares cost on the same coin-flipped turns.
//
// The measure is log(1 + model calls) per turn, so one turn that loops does
// not dominate. The test is the two-sample Bayes factor of Gönen, Johnson,
// Lu and Westfall (2005): a normal prior on the standardised difference
// (scale costPrior), and the usual flat prior on the shared mean and 1/σ on
// the shared spread. Those nuisance priors are the right Haar priors of the
// location-scale problem, under which a Bayes factor is a safe test however
// often it is looked at (Grünwald, de Heide and Koolen; the t-test is their
// worked case), the property the success rule needs too (D-020).

// costPrior is the prior scale of the standardised difference in log cost.
const costPrior = 1.0

func (t *Tally) addCost(delivered bool, calls int) {
	y := math.Log1p(float64(max(calls, 0)))
	if delivered {
		t.ExposedLogCalls += y
		t.ExposedLogCalls2 += y * y
	} else {
		t.WithheldLogCalls += y
		t.WithheldLogCalls2 += y * y
	}
}

// CostBayesFactor weighs "delivering the lesson changes the cost of a turn"
// against "it does not", on log(1 + model calls). Large values favour a
// difference, in the direction Cheaper shows. 1 when there are too few
// turns to say.
func (t Tally) CostBayesFactor() float64 {
	n1, n2 := float64(t.Exposed), float64(t.Withheld)
	nu := n1 + n2 - 2
	if n1 < 2 || n2 < 2 {
		return 1
	}
	m1, m2 := t.ExposedLogCalls/n1, t.WithheldLogCalls/n2
	ss := (t.ExposedLogCalls2 - n1*m1*m1) + (t.WithheldLogCalls2 - n2*m2*m2)
	nd := 1 / (1/n1 + 1/n2)
	c := 1 + nd*costPrior*costPrior
	diff := m1 - m2
	var logRatio float64 // log((1 + t²/ν) / (1 + t²/(ν c)))
	switch {
	case math.Abs(diff) < 1e-12:
		logRatio = 0
	case ss <= 1e-12:
		// No spread at all and a difference: t is unbounded, and the ratio
		// tends to c.
		logRatio = math.Log(c)
	default:
		s2 := ss / nu
		t2 := diff * diff / (s2 * (1/n1 + 1/n2))
		logRatio = math.Log1p(t2/nu) - math.Log1p(t2/(nu*c))
	}
	// Capped well inside float64, so evidence stays encodable: past 1e300
	// the bars are long since decided.
	return math.Exp(min((nu+1)/2*logRatio-0.5*math.Log(c), 690))
}

// Cheaper reports whether turns cost less with the lesson delivered.
func (t Tally) Cheaper() bool {
	if t.Exposed == 0 || t.Withheld == 0 {
		return false
	}
	return t.ExposedLogCalls/float64(t.Exposed) < t.WithheldLogCalls/float64(t.Withheld)
}

// NotWorse is the posterior probability that the success rate delivered is
// no more than margin below the rate withheld, each rate under a uniform
// Beta(1,1) prior: the guard that keeps a lesson from being accepted for
// being cheap while it breaks things.
func (t Tally) NotWorse(margin float64) float64 {
	se, fe := float64(t.ExposedSuccesses), float64(t.Exposed-t.ExposedSuccesses)
	sw, fw := float64(t.WithheldSuccesses), float64(t.Withheld-t.WithheldSuccesses)
	// P(pe > pw - margin) = ∫ f_w(x) · (1 - F_e(x - margin)) dx, on a grid.
	const n = 2000
	h := 1.0 / n
	le := lbeta(1+se, 1+fe)
	lw := lbeta(1+sw, 1+fw)
	dens := func(x, a, b, lb float64) float64 {
		if x <= 0 || x >= 1 {
			return 0
		}
		return math.Exp((a-1)*math.Log(x) + (b-1)*math.Log(1-x) - lb)
	}
	// F_e on the grid, by the midpoint rule.
	cdf := make([]float64, n+1)
	for i := 1; i <= n; i++ {
		cdf[i] = cdf[i-1] + dens((float64(i)-0.5)*h, 1+se, 1+fe, le)*h
	}
	at := func(x float64) float64 {
		switch {
		case x <= 0:
			return 0
		case x >= 1:
			return 1
		}
		return cdf[int(x*n)]
	}
	p := 0.0
	for i := 0; i < n; i++ {
		x := (float64(i) + 0.5) * h
		p += dens(x, 1+sw, 1+fw, lw) * h * (1 - at(x-margin))
	}
	return math.Min(1, math.Max(0, p))
}

// costLine says what the cost comparison shows, for reasons.
func costLine(t Tally) string {
	ce, cw := t.Calls()
	return fmt.Sprintf("%.1f model calls per turn delivered vs %.1f withheld, cost Bayes factor %.3g", ce, cw, t.CostBayesFactor())
}
