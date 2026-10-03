package ledger

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// Pairs (D-027). Each lesson's coin is flipped on its own, so on the turns two
// lessons share, every combination happens: both delivered, one, the other,
// neither. That is a two-by-two experiment the host runs for free, and it
// answers what one lesson's credit cannot:
//
//   - Contradiction: delivering A hurts when B is delivered too. Two lessons
//     that each repair a failure their own way can, together, give the host
//     two plans to follow. Each lesson's own credit mixes turns with and
//     without the other, and can look neutral while a quarter of the shared
//     turns fail.
//   - Redundancy: delivering A adds nothing when B is delivered. A paraphrase
//     of a lesson in force passes alone and is neutral beside it, so its own
//     credit never moves it, and it holds a place in the host's budget for
//     good.
//
// The lesson that hurts, or adds nothing, is superseded by the other: set
// aside, not eliminated. It is not wrong on its own, so it comes back
// through trial if the other leaves.

// Pair is what the turns two lessons share say about them together. A is the
// older lesson. Shared turns are those that bear on both (Relevance).
type Pair struct {
	A types.ID `json:"a"`
	B types.ID `json:"b"`
	// AddA is A's credit on shared turns where B was delivered: A delivered
	// against A withheld. SoloA is the same where B was withheld. AddB and
	// SoloB are B's. Only the lesson's own coin differs between its arms,
	// so these are as fair as its own credit, whatever the other lesson's
	// coin did.
	AddA, AddB, SoloA, SoloB Stratified
	// Alone compares A delivered without B (Exposed) against B delivered
	// without A (Withheld), to choose between two lessons that contradict
	// each other.
	Alone Stratified
}

// Stratified is a tally kept per period in which the delivery
// probabilities stood still (D-027). A lesson's probability changes with
// its state, so turns from different periods cannot be pooled into one
// comparison (Eras). Pair evidence accrues slowly, though, and restarting
// it at every state change would lose it, so it is kept per period and the
// periods' Bayes factors are multiplied. Under "no difference" each period's
// factor is a test martingale over that period's turns, and so is the
// product over consecutive periods: the bars stay valid however often the
// ledger looks.
type Stratified struct {
	Strata []Tally `json:"strata"`
	keys   []string
}

func (s *Stratified) add(key string, delivered bool, win int) {
	i := slices.Index(s.keys, key)
	if i < 0 {
		s.keys = append(s.keys, key)
		s.Strata = append(s.Strata, Tally{})
		i = len(s.Strata) - 1
	}
	t := &s.Strata[i]
	if delivered {
		t.Exposed++
		t.ExposedSuccesses += win
	} else {
		t.Withheld++
		t.WithheldSuccesses += win
	}
}

// Pooled sums the periods, for rates, the direction of an effect and the
// minimum-turns rule.
func (s Stratified) Pooled() Tally {
	var t Tally
	for _, x := range s.Strata {
		t.Exposed += x.Exposed
		t.ExposedSuccesses += x.ExposedSuccesses
		t.Withheld += x.Withheld
		t.WithheldSuccesses += x.WithheldSuccesses
	}
	return t
}

// BayesFactor is the product of the periods' Bayes factors.
func (s Stratified) BayesFactor() float64 {
	bf := 1.0
	for _, x := range s.Strata {
		bf *= x.BayesFactor()
	}
	return bf
}

// Book is the ledger's view of the live turns: each lesson's tally and each
// pair's.
type Book struct {
	Lessons map[types.ID]*Tally
	Pairs   []*Pair
}

// Period is a stretch of time a lesson spent in one state.
type Period struct {
	Start time.Time
	State types.LessonState
}

// Periods returns every lesson's states over time, from its lifecycle
// events, oldest first.
func Periods(m *metabolome.Metabolome) map[types.ID][]Period {
	out := map[types.ID][]Period{}
	for _, l := range m.Lessons() {
		e, _ := m.Get(l.ID)
		for _, ev := range e.Events {
			out[l.ID] = append(out[l.ID], Period{Start: ev.At, State: ev.To})
		}
	}
	return out
}

// periodAt returns the start of the period of ps in force at t, and
// whether the lesson was in trial or accepted then.
func periodAt(ps []Period, t time.Time) (string, bool) {
	i := sort.Search(len(ps), func(i int) bool { return ps[i].Start.After(t) }) - 1
	if i < 0 || !ps[i].State.Injectable() {
		return "", false
	}
	return ps[i].Start.Format(time.RFC3339Nano), true
}

// PairTallies counts every pair of lessons delivered or withheld together on
// mature turns that bear on both (rel; nil: every turn), across every period
// both were in trial or accepted. order lists the lessons oldest first,
// which fixes each pair's A and B; pairs of lessons not in order are skipped.
func PairTallies(eps []types.Episode, outs map[types.ID][]types.Outcome, now time.Time, maturity time.Duration,
	periods map[types.ID][]Period, rel Relevant, order []types.ID) []*Pair {
	rank := map[types.ID]int{}
	for i, id := range order {
		rank[id] = i
	}
	pairs := map[[2]types.ID]*Pair{}
	cutoff := now.Add(-maturity)
	for _, ep := range eps {
		if ep.EndedAt.After(cutoff) {
			continue
		}
		delivered := map[types.ID]bool{}
		var in []types.ID
		for _, id := range ep.Exposure {
			delivered[id] = true
			in = append(in, id)
		}
		in = append(in, ep.Withheld...)
		period := map[types.ID]string{}
		in = slices.DeleteFunc(in, func(id types.ID) bool {
			if _, known := rank[id]; !known || (rel != nil && !rel(id, ep)) {
				return true
			}
			k, ok := periodAt(periods[id], ep.StartedAt)
			period[id] = k
			return !ok
		})
		if len(in) < 2 {
			continue
		}
		slices.SortFunc(in, func(a, b types.ID) int { return rank[a] - rank[b] })
		win := 0
		if types.Succeeded(ep, outs[ep.ID]) {
			win = 1
		}
		for i, a := range in {
			for _, b := range in[i+1:] {
				p := pairs[[2]types.ID{a, b}]
				if p == nil {
					p = &Pair{A: a, B: b}
					pairs[[2]types.ID{a, b}] = p
				}
				if delivered[b] {
					p.AddA.add(period[a], delivered[a], win)
				} else {
					p.SoloA.add(period[a], delivered[a], win)
				}
				if delivered[a] {
					p.AddB.add(period[b], delivered[b], win)
				} else {
					p.SoloB.add(period[b], delivered[b], win)
				}
				if delivered[a] != delivered[b] {
					p.Alone.add(period[a]+" "+period[b], delivered[a], win)
				}
			}
		}
	}
	out := make([]*Pair, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p)
	}
	slices.SortFunc(out, func(x, y *Pair) int {
		if d := rank[x.A] - rank[y.A]; d != 0 {
			return d
		}
		return rank[x.B] - rank[y.B]
	})
	return out
}

// PairDecision is what settling a pair concluded: Loser is superseded by
// Winner, or nothing moves when Loser is empty.
type PairDecision struct {
	Winner, Loser types.Lesson
	Kind          types.EvidenceKind // contradiction or redundancy
	// Tally is the loser's credit beside the winner, pooled over periods:
	// the counts the decision rests on. BayesFactor is the periods' product.
	Tally       Tally
	BayesFactor float64
	Reason      string
}

// decidePair applies the pair rule.
//
//   - Contradiction: delivering one hurts when the other is delivered too,
//     by the elimination bar, like any harm, though it does not look worse
//     when the other is withheld (then it is just harmful, and the ledger's
//     own rule eliminates it). If each hurts the other, the
//     one that does better alone stays; when the turns cannot tell, the
//     older one, which was there first.
//   - Redundancy: delivering one adds nothing when the other is delivered
//     (a Bayes factor of rule.Redundant for "no difference"), though it helps
//     when the other is withheld (rule.Accept): the other does what it does.
//     A lesson that helps nowhere is not redundant, only neutral, and the
//     ledger leaves neutral lessons alone. Supersession is reversible, so it
//     needs as much evidence as acceptance, not as elimination.
func decidePair(a, b types.Lesson, p Pair, r Rule) PairDecision {
	enough := func(s Stratified) bool {
		t := s.Pooled()
		return t.Exposed >= r.MinExposed && t.Withheld >= r.MinWithheld
	}
	hurts := func(s Stratified) bool {
		return enough(s) && s.Pooled().PBetter() < 0.5 && s.BayesFactor() >= r.Eliminate
	}
	helps := func(s Stratified) bool {
		return enough(s) && s.Pooled().PBetter() > 0.5 && s.BayesFactor() >= r.Accept
	}
	nothing := func(s Stratified) bool { return enough(s) && s.BayesFactor() <= 1/r.Redundant }
	loses := func(loser, winner types.Lesson, s Stratified, kind types.EvidenceKind, why string) PairDecision {
		return PairDecision{Winner: winner, Loser: loser, Kind: kind, Tally: s.Pooled(), BayesFactor: s.BayesFactor(),
			Reason: fmt.Sprintf("superseded by %s: %s: delivered beside it, it succeeded %s", winner.ID, why, stratRates(s))}
	}
	// A lesson that looks worse on its own too is not in a contradiction,
	// only harmful: the ledger's own rule eliminates it.
	ha := hurts(p.AddA) && p.SoloA.Pooled().PBetter() >= 0.5
	hb := hurts(p.AddB) && p.SoloB.Pooled().PBetter() >= 0.5
	switch {
	case ha && hb:
		if al := p.Alone; enough(al) && al.Pooled().PBetter() < 0.5 && al.BayesFactor() >= r.Accept {
			return loses(a, b, p.AddA, types.EvidenceContradiction, fmt.Sprintf(
				"they contradict each other, and %s does better alone (%s)", b.ID, stratRates(al)))
		}
		return loses(b, a, p.AddB, types.EvidenceContradiction, "they contradict each other; the older lesson stays")
	case ha:
		return loses(a, b, p.AddA, types.EvidenceContradiction, "it undoes what the other does")
	case hb:
		return loses(b, a, p.AddB, types.EvidenceContradiction, "it undoes what the other does")
	}
	ra := nothing(p.AddA) && helps(p.SoloA)
	rb := nothing(p.AddB) && helps(p.SoloB)
	switch {
	case ra && rb:
		return loses(b, a, p.AddB, types.EvidenceRedundancy, "each does what the other does; the older lesson stays")
	case ra:
		return loses(a, b, p.AddA, types.EvidenceRedundancy, "the other does what it does")
	case rb:
		return loses(b, a, p.AddB, types.EvidenceRedundancy, "the other does what it does")
	}
	return PairDecision{}
}

func stratRates(s Stratified) string {
	t := s.Pooled()
	e, w := t.Rate()
	return fmt.Sprintf("%.0f%% of %d turns delivered vs %.0f%% of %d withheld, Bayes factor %.3g over %d period(s)",
		100*e, t.Exposed, 100*w, t.Withheld, s.BayesFactor(), len(s.Strata))
}

// settlePairs decides the pairs of lessons in force, oldest pairs first,
// leaving out the lessons in moved (eliminated by their own credit this
// settle). A lesson set aside in one pair is not decided again in the same
// settle.
func settlePairs(ctx context.Context, m *metabolome.Metabolome, pairs []*Pair, rule Rule, dryRun bool, moved map[types.ID]bool) ([]PairDecision, error) {
	var out []PairDecision
	for _, p := range pairs {
		if moved[p.A] || moved[p.B] {
			continue
		}
		ea, okA := m.Get(p.A)
		eb, okB := m.Get(p.B)
		if !okA || !okB || !ea.Lesson.State.Injectable() || !eb.Lesson.State.Injectable() {
			continue
		}
		d := decidePair(ea.Lesson, eb.Lesson, *p, rule)
		if d.Loser.ID == "" {
			continue
		}
		moved[d.Loser.ID] = true
		out = append(out, d)
		if dryRun {
			continue
		}
		t := d.Tally
		ev, err := m.RecordEvidence(ctx, types.Evidence{
			LessonID: d.Loser.ID, Kind: d.Kind, Result: types.ResultFail, Summary: d.Reason, Other: d.Winner.ID,
			Exposed: t.Exposed, ExposedSuccesses: t.ExposedSuccesses,
			Withheld: t.Withheld, WithheldSuccesses: t.WithheldSuccesses, PBetter: round4(t.PBetter()),
			BayesFactor: math.Round(d.BayesFactor*1e4) / 1e4,
		})
		if err != nil {
			return out, err
		}
		if err := m.Move(ctx, d.Loser.ID, types.StateSuperseded, d.Reason, ev.ID); err != nil {
			return out, err
		}
	}
	return out, nil
}

// restore brings back, through trial, every superseded lesson whose winner
// is no longer in force: it was set aside for that lesson, not for itself.
// It cites the nutrient test it passed to enter trial the first time.
func restore(ctx context.Context, m *metabolome.Metabolome, dryRun bool) ([]Decision, error) {
	var out []Decision
	for _, l := range m.Lessons(types.StateSuperseded) {
		by, ok := m.SupersededBy(l.ID)
		if !ok {
			continue
		}
		if w, ok := m.Get(by); ok && w.Lesson.State.Injectable() {
			continue
		}
		e, _ := m.Get(l.ID)
		var passed types.ID
		for _, ev := range e.Evidence {
			if ev.Kind == types.EvidenceNutrientTest && ev.Passed() {
				passed = ev.ID
			}
		}
		if passed == "" {
			continue
		}
		w, _ := m.Get(by)
		reason := fmt.Sprintf("restored to trial: %s, which superseded it, is now %s", by, w.Lesson.State)
		out = append(out, Decision{Lesson: l, To: types.StateTrial, Reason: reason})
		if dryRun {
			continue
		}
		if err := m.Move(ctx, l.ID, types.StateTrial, reason, passed); err != nil {
			return out, err
		}
	}
	return out, nil
}
