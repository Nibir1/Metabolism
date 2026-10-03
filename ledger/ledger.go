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
	// Unrelated counts turns, delivered or withheld, whose request does not
	// bear on the lesson (Relevance): they are left out of both arms.
	Unrelated int `json:"unrelated,omitempty"`
	// All is the tally over every turn, related or not, when the counts
	// above are filtered by relevance. Harm is judged on both (D-026).
	All *Tally `json:"all,omitempty"`

	// The same counts over the lesson's most recent DriftWindow mature
	// turns (delivered and withheld together): what drift is judged on.
	RecentExposed           int `json:"recent_exposed,omitempty"`
	RecentExposedSuccesses  int `json:"recent_exposed_successes,omitempty"`
	RecentWithheld          int `json:"recent_withheld,omitempty"`
	RecentWithheldSuccesses int `json:"recent_withheld_successes,omitempty"`
}

// DriftWindow is how many of a lesson's most recent turns drift is judged
// on (D-026). An accepted lesson has a long good record; if the world
// changes and it starts doing harm, the harm is diluted by that record and
// the whole-history test would take very long to see it. The window is what
// the world looks like now.
//
// 200, not less: an accepted lesson keeps only a 10% holdout, so a window
// has about a tenth of its turns withheld. With 10 withheld turns even a
// drastic change (10% success delivered against 60% withheld) reaches a
// Bayes factor of about 113, short of the elimination bar of 300; with 20 it
// reaches about 28,000.
const DriftWindow = 200

// Recent is the tally of the lesson's most recent DriftWindow turns.
func (t Tally) Recent() Tally {
	return Tally{LessonID: t.LessonID, Exposed: t.RecentExposed, ExposedSuccesses: t.RecentExposedSuccesses,
		Withheld: t.RecentWithheld, WithheldSuccesses: t.RecentWithheldSuccesses}
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

// Credit counts the live turns in j for m's lessons under rule: in each
// lesson's era, and only the turns that bear on it unless rule.AllTurns. rel
// may be nil; keep one across repeated calls to read only what j gained.
func Credit(ctx context.Context, j ports.Journal, m *metabolome.Metabolome, now time.Time, rule Rule, rel *Relevance) (map[types.ID]*Tally, error) {
	rule = rule.Defaults()
	eras := Eras(m)
	var fn Relevant
	if !rule.AllTurns {
		if rel == nil {
			rel = NewRelevance()
		}
		var err error
		if fn, err = rel.Update(ctx, j, m, eras); err != nil {
			return nil, err
		}
	}
	return Count(ctx, j, now, rule.Maturity, eras, fn)
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
// counts every episode), and that rel says bears on it (nil: every episode).
// Episodes that ended after now-maturity are counted as immature only.
func Count(ctx context.Context, j ports.Journal, now time.Time, maturity time.Duration, eras map[types.ID]time.Time, rel Relevant) (map[types.ID]*Tally, error) {
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
	return Tallies(eps, outs, now, maturity, eras, rel), nil
}

// Tallies is Count over episodes already in memory. With rel, each tally
// counts the turns that bear on its lesson, and its All the every-turn tally.
func Tallies(eps []types.Episode, outs map[types.ID][]types.Outcome, now time.Time, maturity time.Duration, eras map[types.ID]time.Time, rel Relevant) map[types.ID]*Tally {
	all := tallies(eps, outs, now, maturity, eras, nil)
	if rel == nil {
		return all
	}
	related := tallies(eps, outs, now, maturity, eras, rel)
	for id, t := range related {
		t.All = all[id]
	}
	return related
}

func tallies(eps []types.Episode, outs map[types.ID][]types.Outcome, now time.Time, maturity time.Duration, eras map[types.ID]time.Time, rel Relevant) map[types.ID]*Tally {
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
	// Newest first, so each lesson's first DriftWindow mature turns are its
	// most recent ones.
	order := make([]int, len(eps))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return eps[b].StartedAt.Compare(eps[a].StartedAt) })
	for _, i := range order {
		ep := eps[i]
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
			if rel != nil && !rel(id, ep) {
				t.Unrelated++
				continue
			}
			if !mature {
				t.Immature++
				continue
			}
			if t.RecentExposed+t.RecentWithheld < DriftWindow {
				t.RecentExposed++
				t.RecentExposedSuccesses += win
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
			if rel != nil && !rel(id, ep) {
				t.Unrelated++
				continue
			}
			if !mature {
				t.Immature++
				continue
			}
			if t.RecentExposed+t.RecentWithheld < DriftWindow {
				t.RecentWithheld++
				t.RecentWithheldSuccesses += win
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
	// AllTurns credits a lesson on every turn it was delivered or withheld
	// on, as D-020 first did, instead of only the turns that bear on it
	// (Relevance, D-026). For comparison.
	AllTurns bool
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
		t = d.Tally // the counts the decision rests on: a view or a window
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
	// Harm anywhere counts: a lesson is eliminated on the turns that bear
	// on it or over every turn. Help must show where the lesson applies.
	views := []view{{t, ""}}
	if t.All != nil {
		views = []view{{t, " on the turns that bear on it"}, {*t.All, " over every turn"}}
	}
	for _, v := range views {
		if d, ok := decideHarm(l, v, r); ok {
			return d
		}
	}
	d := Decision{Lesson: l, Tally: t}
	if t.Exposed < r.MinExposed || t.Withheld < r.MinWithheld {
		d.Reason = fmt.Sprintf("not enough turns yet%s: %d delivered (need %d), %d withheld (need %d)",
			views[0].where, t.Exposed, r.MinExposed, t.Withheld, r.MinWithheld)
		return d
	}
	if l.State == types.StateTrial && t.PBetter() > 0.5 && t.BayesFactor() >= r.Accept {
		d.To = types.StateAccepted
		d.Reason = "credit: better when delivered" + views[0].where + ": " + rates(t)
		return d
	}
	if l.State == types.StateAccepted {
		for _, v := range views {
			if drifted, ok := decideDrift(l, view{v.t.Recent(), v.where}, r); ok {
				return drifted
			}
		}
	}
	d.Reason = "undecided" + views[0].where + ": " + rates(t)
	return d
}

// view is a tally and the turns it covers, for reasons.
type view struct {
	t     Tally
	where string
}

func rates(t Tally) string {
	e, w := t.Rate()
	return fmt.Sprintf("succeeded %.0f%% of %d turns delivered vs %.0f%% of %d withheld, P(better) %.3f, Bayes factor %.3g",
		100*e, t.Exposed, 100*w, t.Withheld, t.PBetter(), t.BayesFactor())
}

func decideHarm(l types.Lesson, v view, r Rule) (Decision, bool) {
	t := v.t
	if t.Exposed < r.MinExposed || t.Withheld < r.MinWithheld || t.PBetter() >= 0.5 || t.BayesFactor() < r.Eliminate {
		return Decision{}, false
	}
	return Decision{Lesson: l, Tally: t, To: types.StateEliminated,
		Reason: "credit: worse when delivered" + v.where + ": " + rates(t)}, true
}

// decideDrift retires an accepted lesson whose recent turns show it doing
// harm, by the same bar as any elimination, even when its whole record is
// still positive (D-026). Its evidence is the window's counts.
func decideDrift(l types.Lesson, v view, r Rule) (Decision, bool) {
	recent := v.t
	if recent.Exposed < r.MinExposed || recent.Withheld < r.MinWithheld/2 {
		return Decision{}, false
	}
	p, bf := recent.PBetter(), recent.BayesFactor()
	if p >= 0.5 || bf < r.Eliminate {
		return Decision{}, false
	}
	e, w := recent.Rate()
	return Decision{Lesson: l, Tally: recent, To: types.StateEliminated, Reason: fmt.Sprintf(
		"credit: stopped helping (drift): over its last %d turns%s it succeeded %.0f%% of %d delivered vs %.0f%% of %d withheld, P(better) %.3f, Bayes factor %.3g",
		recent.Exposed+recent.Withheld, v.where, 100*e, recent.Exposed, 100*w, recent.Withheld, p, bf)}, true
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
