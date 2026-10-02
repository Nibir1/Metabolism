// Package ledger is the credit ledger (Phase 3, D-020): it credits or blames
// each delivered lesson by the outcomes of the live turns it applied to.
//
// The host runs a randomised experiment per lesson. On every turn a lesson
// applies to, a coin flip decides whether it is delivered (the episode's
// Exposure) or held back (Withheld). Delivered and withheld turns differ only
// by the coin, so comparing their success rates measures the lesson itself,
// not the kind of turns it happened to be shown on. That is the matched
// no-lesson baseline a credit ledger needs; counting successes among exposed
// turns alone would credit a lesson for turns that were going well anyway.
//
// Like the nutrient test, the ledger only appends: evidence and lifecycle
// events go through the metabolome (D-014).
package ledger

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Tally is what the live turns say about one lesson.
type Tally struct {
	LessonID          types.ID `json:"lesson_id"`
	Exposed           int      `json:"exposed"`
	ExposedSuccesses  int      `json:"exposed_successes"`
	Withheld          int      `json:"withheld"`
	WithheldSuccesses int      `json:"withheld_successes"`
	// Model calls summed over each arm, for the cost side of the report.
	ExposedCalls  int `json:"exposed_calls"`
	WithheldCalls int `json:"withheld_calls"`
	// Immature counts turns too recent to judge: a late outcome (an undo, a
	// repeated request) may still arrive for them.
	Immature int `json:"immature"`
}

// PBetter is the posterior probability that the lesson's success rate is
// higher when delivered than when withheld, each rate under a uniform
// Beta(1,1) prior. 0.5 means the turns cannot tell.
func (t Tally) PBetter() float64 {
	return pGreater(t.ExposedSuccesses, t.Exposed-t.ExposedSuccesses,
		t.WithheldSuccesses, t.Withheld-t.WithheldSuccesses)
}

// BayesFactor weighs "delivering the lesson changes the success rate"
// (each arm its own rate, uniform priors) against "it does not" (one shared
// rate, uniform prior). Large values favour a difference, in the direction
// PBetter shows.
//
// Unlike a p-value, a Bayes factor can be checked after every turn: under
// the no-difference hypothesis the chance it ever reaches k is at most about
// 1/k, however often anyone looks (Ville's inequality for the mixture
// martingale). The ledger is settled whenever the user likes, so that is
// the property its bar needs (D-020).
func (t Tally) BayesFactor() float64 {
	se, fe := float64(t.ExposedSuccesses), float64(t.Exposed-t.ExposedSuccesses)
	sw, fw := float64(t.WithheldSuccesses), float64(t.Withheld-t.WithheldSuccesses)
	l1 := lbeta(1+se, 1+fe) + lbeta(1+sw, 1+fw)
	l0 := lbeta(1+se+sw, 1+fe+fw)
	return math.Exp(l1 - l0)
}

// Rate returns the success rate of each arm (0 for an empty arm).
func (t Tally) Rate() (exposed, withheld float64) {
	return ratio(t.ExposedSuccesses, t.Exposed), ratio(t.WithheldSuccesses, t.Withheld)
}

// Calls returns the mean model calls per turn in each arm.
func (t Tally) Calls() (exposed, withheld float64) {
	return ratio(t.ExposedCalls, t.Exposed), ratio(t.WithheldCalls, t.Withheld)
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Eras returns, for every trial and accepted lesson, when it entered its
// current state. A lesson is credited only on turns since then: its delivery
// probability changes with its state (deliver.PTrial, deliver.PAccepted), and
// mixing turns from both periods would compare an arm drawn mostly from one
// period with an arm drawn mostly from the other. Within one period the coin
// is the only difference between the arms, whatever else changed meanwhile.
func Eras(m *metabolome.Metabolome) map[types.ID]time.Time {
	eras := map[types.ID]time.Time{}
	for _, l := range m.Lessons(types.StateTrial, types.StateAccepted) {
		e, _ := m.Get(l.ID)
		if n := len(e.Events); n > 0 {
			eras[l.ID] = e.Events[n-1].At
		}
	}
	return eras
}

// Count tallies every lesson that was delivered or withheld on an episode in
// j that started at or after the lesson's era (Eras; a lesson with no era
// counts every episode). Episodes that ended after now-maturity are counted
// as immature only.
func Count(ctx context.Context, j ports.Journal, now time.Time, maturity time.Duration, eras map[types.ID]time.Time) (map[types.ID]*Tally, error) {
	var eps []types.Episode
	outs := map[types.ID][]types.Outcome{}
	q := types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode, types.RecordOutcome}}
	for r, err := range j.Scan(ctx, q) {
		if err != nil {
			return nil, fmt.Errorf("ledger: %w", err)
		}
		switch r.Kind {
		case types.RecordEpisode:
			eps = append(eps, *r.Episode)
		case types.RecordOutcome:
			outs[r.Outcome.EpisodeID] = append(outs[r.Outcome.EpisodeID], *r.Outcome)
		}
	}
	return Tallies(eps, outs, now, maturity, eras), nil
}

// Tallies is Count over episodes already in memory.
func Tallies(eps []types.Episode, outs map[types.ID][]types.Outcome, now time.Time, maturity time.Duration, eras map[types.ID]time.Time) map[types.ID]*Tally {
	tallies := map[types.ID]*Tally{}
	get := func(id types.ID) *Tally {
		t, ok := tallies[id]
		if !ok {
			t = &Tally{LessonID: id}
			tallies[id] = t
		}
		return t
	}
	cutoff := now.Add(-maturity)
	for _, ep := range eps {
		if len(ep.Exposure) == 0 && len(ep.Withheld) == 0 {
			continue
		}
		mature := !ep.EndedAt.After(cutoff)
		ok := types.Succeeded(ep, outs[ep.ID])
		win := 0
		if ok {
			win = 1
		}
		for _, id := range ep.Exposure {
			if ep.StartedAt.Before(eras[id]) {
				continue
			}
			t := get(id)
			if !mature {
				t.Immature++
				continue
			}
			t.Exposed++
			t.ExposedSuccesses += win
			t.ExposedCalls += ep.Usage.ModelCalls
		}
		for _, id := range ep.Withheld {
			if ep.StartedAt.Before(eras[id]) {
				continue
			}
			t := get(id)
			if !mature {
				t.Immature++
				continue
			}
			t.Withheld++
			t.WithheldSuccesses += win
			t.WithheldCalls += ep.Usage.ModelCalls
		}
	}
	return tallies
}

// Rule is the decision rule (D-020). The zero value selects the defaults.
type Rule struct {
	// MinExposed and MinWithheld are the fewest mature turns in each arm
	// before any decision (defaults 20 and 10).
	MinExposed  int
	MinWithheld int
	// Accept is the Bayes factor a trial lesson needs, in the "better"
	// direction, to be accepted (default 30). Acceptance is reversible: an
	// accepted lesson keeps a holdout and can still be eliminated.
	Accept float64
	// Eliminate is the Bayes factor, in the "worse" direction, that
	// eliminates a trial or accepted lesson (default 300). Elimination is
	// permanent, so its bar is ten times higher.
	Eliminate float64
	// Maturity is how old a turn must be before it counts (default 1h), so
	// late outcomes have arrived.
	Maturity time.Duration
}

// Defaults returns r with every zero field set to its default.
func (r Rule) Defaults() Rule {
	if r.MinExposed <= 0 {
		r.MinExposed = 20
	}
	if r.MinWithheld <= 0 {
		r.MinWithheld = 10
	}
	if r.Accept <= 0 {
		r.Accept = 30
	}
	if r.Eliminate <= 0 {
		r.Eliminate = 300
	}
	if r.Maturity <= 0 {
		r.Maturity = time.Hour
	}
	return r
}

// Decision is what Settle concluded for one lesson.
type Decision struct {
	Lesson types.Lesson
	Tally  Tally
	// To is the state the lesson moved to; empty when it stays.
	To     types.LessonState
	Reason string
}

// Settle decides every trial and accepted lesson from its tally, and unless
// dryRun, records the evidence and moves the lesson. A lesson without enough
// turns is reported and left alone.
func Settle(ctx context.Context, m *metabolome.Metabolome, tallies map[types.ID]*Tally, rule Rule, dryRun bool) ([]Decision, error) {
	rule = rule.Defaults()
	var out []Decision
	for _, l := range m.Lessons(types.StateTrial, types.StateAccepted) {
		t := Tally{LessonID: l.ID}
		if got := tallies[l.ID]; got != nil {
			t = *got
		}
		d := decide(l, t, rule)
		out = append(out, d)
		if dryRun || d.To == "" {
			continue
		}
		result := types.ResultPass
		if d.To == types.StateEliminated {
			result = types.ResultFail
		}
		ev, err := m.RecordEvidence(ctx, types.Evidence{
			LessonID: l.ID, Kind: types.EvidenceCredit, Result: result, Summary: d.Reason,
			Exposed: t.Exposed, ExposedSuccesses: t.ExposedSuccesses,
			Withheld: t.Withheld, WithheldSuccesses: t.WithheldSuccesses, PBetter: round4(t.PBetter()),
			BayesFactor: math.Round(t.BayesFactor()*100) / 100,
		})
		if err != nil {
			return out, err
		}
		if err := m.Move(ctx, l.ID, d.To, d.Reason, ev.ID); err != nil {
			return out, err
		}
	}
	slices.SortFunc(out, func(a, b Decision) int {
		switch {
		case a.Lesson.CreatedAt.Before(b.Lesson.CreatedAt):
			return -1
		case a.Lesson.CreatedAt.After(b.Lesson.CreatedAt):
			return 1
		}
		return 0
	})
	return out, nil
}

func decide(l types.Lesson, t Tally, r Rule) Decision {
	d := Decision{Lesson: l, Tally: t}
	if t.Exposed < r.MinExposed || t.Withheld < r.MinWithheld {
		d.Reason = fmt.Sprintf("not enough turns yet: %d delivered (need %d), %d withheld (need %d)",
			t.Exposed, r.MinExposed, t.Withheld, r.MinWithheld)
		return d
	}
	p, bf := t.PBetter(), t.BayesFactor()
	e, w := t.Rate()
	rates := fmt.Sprintf("succeeded %.0f%% of %d turns delivered vs %.0f%% of %d withheld, P(better) %.3f, Bayes factor %.3g",
		100*e, t.Exposed, 100*w, t.Withheld, p, bf)
	switch {
	case p < 0.5 && bf >= r.Eliminate:
		d.To = types.StateEliminated
		d.Reason = "credit: worse when delivered: " + rates
	case l.State == types.StateTrial && p > 0.5 && bf >= r.Accept:
		d.To = types.StateAccepted
		d.Reason = "credit: better when delivered: " + rates
	default:
		d.Reason = "undecided: " + rates
	}
	return d
}

func round4(x float64) float64 { return math.Round(x*1e4) / 1e4 }

// pGreater is P(X > Y) for X ~ Beta(1+s1, 1+f1) and Y ~ Beta(1+s2, 1+f2),
// integrated numerically: ∫ pdfX(x) · cdfY(x) dx. The grid is fine enough
// for decisions at the 0.95 and 0.99 levels.
func pGreater(s1, f1, s2, f2 int) float64 {
	const n = 4000
	a1, b1 := float64(s1+1), float64(f1+1)
	a2, b2 := float64(s2+1), float64(f2+1)
	lb1, lb2 := lbeta(a1, b1), lbeta(a2, b2)
	h := 1.0 / n
	cum, p := 0.0, 0.0 // cum: cdfY up to the current cell's left edge
	for i := range n {
		x := (float64(i) + 0.5) * h
		y := math.Exp((a2-1)*math.Log(x) + (b2-1)*math.Log1p(-x) - lb2)
		fx := math.Exp((a1-1)*math.Log(x) + (b1-1)*math.Log1p(-x) - lb1)
		p += fx * (cum + y*h/2) * h
		cum += y * h
	}
	return math.Min(math.Max(p, 0), 1)
}

func lbeta(a, b float64) float64 {
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	return la + lb - lab
}
