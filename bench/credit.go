package bench

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/deliver"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// CreditRow is one planted lesson and where live credit took it.
type CreditRow struct {
	Seed uint64 `json:"seed"`
	Text string `json:"text"`
	// Expect is what a sound ledger should do: "accept" a lesson that turns
	// failures into successes, "eliminate" one that causes failures, "keep"
	// one that changes neither outcome nor cost; with cost in the rule
	// (D-030), "accept-cost" a lesson that only saves work (go-module) and
	// "eliminate-cost" one that only adds it. Without cost those are "keep".
	Expect string            `json:"expect"`
	Final  types.LessonState `json:"final"`
	Tally  ledger.Tally      `json:"tally"`
	// At is the turn on which the lesson left trial, 0 if it did not.
	At int `json:"at,omitempty"`
}

// CreditResult is the Phase 3 check on the simulator: does the credit
// ledger, fed by randomised delivery, accept what helps, eliminate what
// harms, and leave alone what changes nothing?
type CreditResult struct {
	Seeds       int `json:"seeds"`
	Turns       int `json:"turns_per_seed"`
	SettleEvery int `json:"settle_every"`

	HelpTotal, HelpAccepted         int `json:"-"`
	HarmTotal, HarmEliminated       int `json:"-"`
	NeutralTotal, NeutralWrongMoves int `json:"-"`
	// Cost-only lessons (D-030).
	CheaperTotal       int `json:"cheaper_total"`
	CheaperAccepted    int `json:"cheaper_accepted"`
	CostlierTotal      int `json:"costlier_total"`
	CostlierEliminated int `json:"costlier_eliminated"`

	HelpAcceptRate    float64     `json:"help_accept_rate"`
	HarmEliminateRate float64     `json:"harm_eliminate_rate"`
	NeutralMovedRate  float64     `json:"neutral_moved_rate"` // accepted or eliminated: both are wrong
	NeutralEliminated int         `json:"neutral_eliminated"` // the permanent mistake
	Rows              []CreditRow `json:"rows"`
}

// expect classifies a simulator lesson for the credit ledger.
func expect(s helixsim.LessonSpec, rule ledger.Rule) string {
	switch {
	case !s.Good && strings.HasPrefix(s.Why, "irrelevant"):
		return "keep"
	case strings.HasPrefix(s.Why, "costly"):
		if rule.WithoutCost {
			return "keep"
		}
		return "eliminate-cost"
	case !s.Good:
		return "eliminate"
	case s.From == "missing-package-json":
		// It saves model calls; the outcome was already a success.
		if rule.WithoutCost {
			return "keep"
		}
		return "accept-cost"
	}
	return "accept"
}

// EvaluateCredit plants every simulator lesson in trial, then plays turns
// in time order. Each turn the lessons are selected as a host would
// (deliver.Select: scope, budget, a coin per lesson), the turn's outcome is
// what the simulator says it would have been with the delivered lessons
// (helixsim.Live), and every settleEvery turns the ledger settles with the
// default rule, as a user running `metabolism credit -apply` would.
//
// The lessons skip the replay test (their evidence says so): this measures
// the ledger, not the nutrient test.
func EvaluateCredit(ctx context.Context, seeds []uint64, turns, settleEvery int, rule ledger.Rule) (CreditResult, error) {
	rule = rule.Defaults()
	res := CreditResult{Seeds: len(seeds), Turns: turns, SettleEvery: settleEvery}
	dir, err := os.MkdirTemp("", "metabolism-credit-")
	if err != nil {
		return res, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	budget := helixsim.Host{}.Describe().Budget

	for _, seed := range seeds {
		rows, err := creditSeed(ctx, filepath.Join(dir, fmt.Sprintf("seed-%d.db", seed)), seed, turns, settleEvery, rule, budget)
		if err != nil {
			return res, err
		}
		res.Rows = append(res.Rows, rows...)
	}
	for _, r := range res.Rows {
		switch r.Expect {
		case "accept":
			res.HelpTotal++
			if r.Final == types.StateAccepted {
				res.HelpAccepted++
			}
		case "eliminate":
			res.HarmTotal++
			if r.Final == types.StateEliminated {
				res.HarmEliminated++
			}
		case "accept-cost":
			res.CheaperTotal++
			if r.Final == types.StateAccepted {
				res.CheaperAccepted++
			}
		case "eliminate-cost":
			res.CostlierTotal++
			if r.Final == types.StateEliminated {
				res.CostlierEliminated++
			}
		default:
			res.NeutralTotal++
			if r.Final != types.StateTrial {
				res.NeutralWrongMoves++
			}
			if r.Final == types.StateEliminated {
				res.NeutralEliminated++
			}
		}
	}
	res.HelpAcceptRate = float64(res.HelpAccepted) / float64(max(res.HelpTotal, 1))
	res.HarmEliminateRate = float64(res.HarmEliminated) / float64(max(res.HarmTotal, 1))
	res.NeutralMovedRate = float64(res.NeutralWrongMoves) / float64(max(res.NeutralTotal, 1))
	return res, nil
}

func creditSeed(ctx context.Context, path string, seed uint64, turns, settleEvery int, rule ledger.Rule, budget types.Budget) ([]CreditRow, error) {
	j, err := sqlite.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = j.Close() }()
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	span := 56 * 24 * time.Hour
	eps, outs := helixsim.Generate(helixsim.Config{Seed: seed, Count: turns, Start: start, Span: span})
	byEp := map[types.ID][]types.Outcome{}
	for _, o := range outs {
		byEp[o.EpisodeID] = append(byEp[o.EpisodeID], o)
	}

	// The lessons need source episodes to exist; a short history before the
	// experiment provides them.
	hist, histOuts := helixsim.Generate(helixsim.Config{Seed: seed ^ 0xabcdef, Count: 200,
		Start: start.Add(-14 * 24 * time.Hour), Span: 13 * 24 * time.Hour})
	for _, e := range hist {
		if err := j.AppendEpisode(ctx, e); err != nil {
			return nil, err
		}
	}
	for _, o := range histOuts {
		if err := j.AppendOutcome(ctx, o); err != nil {
			return nil, err
		}
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return nil, err
	}
	m.SetClock(func() time.Time { return start })
	specs := append(append(helixsim.CorrectLessons(), helixsim.WrongLessons()...), helixsim.CostlyLessons()...)
	rows := make([]CreditRow, 0, len(specs))
	ids := make([]types.ID, 0, len(specs))
	for i, s := range specs {
		var src []types.ID
		for _, e := range hist {
			if e.Attrs["scenario"] == s.From && len(src) < 3 {
				src = append(src, e.ID)
			}
		}
		if len(src) == 0 {
			src = []types.ID{hist[0].ID}
		}
		l, err := m.Create(ctx, types.Lesson{ID: plantedID(seed, i), Kind: s.Kind, Scope: types.Scope{Level: types.ScopeGlobal},
			Text: s.Text, Sources: src, Provenance: types.ProvUserTyped, Origin: "planted"}, "planted by credit-eval")
		if err != nil {
			return nil, err
		}
		ev, err := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
			Result: types.ResultPass, Summary: "planted by credit-eval: the replay test is skipped, the ledger is under test",
			Compared: 2, Wins: 2})
		if err != nil {
			return nil, err
		}
		if err := m.Move(ctx, l.ID, types.StateTrial, "planted by credit-eval", ev.ID); err != nil {
			return nil, err
		}
		rows = append(rows, CreditRow{Seed: seed, Text: s.Text, Expect: expect(s, rule)})
		ids = append(ids, l.ID)
	}

	r := rand.New(rand.NewPCG(seed, seed^0x51ed270b))
	onSettle := func(turn int, ds []ledger.Decision) {
		for _, d := range ds {
			if d.To == "" {
				continue
			}
			for i, id := range ids {
				if id == d.Lesson.ID && rows[i].At == 0 {
					rows[i].At = turn
				}
			}
		}
	}
	if err := playTurns(ctx, j, m, eps, byEp, settleEvery, rule, budget, r, start, start.Add(span+24*time.Hour), nil, onSettle, nil); err != nil {
		return nil, err
	}
	tallies, err := ledger.Credit(ctx, j, m, start.Add(span+24*time.Hour), rule, nil)
	if err != nil {
		return nil, err
	}
	for i, id := range ids {
		e, _ := m.Get(id)
		rows[i].Final = e.Lesson.State
		if t := tallies.Lessons[id]; t != nil {
			rows[i].Tally = *t
		}
	}
	return rows, nil
}

// playTurns plays eps as live turns in time order, as a host with the
// metabolome's lessons would: each turn's lessons are selected by
// deliver.Select (scope, budget, a coin per lesson), its outcome is what the
// simulator says it would have been with the delivered lessons
// (helixsim.LiveIn, in a world drifts may have changed), and the ledger settles every settleEvery turns and once
// more at end. onTurn, if set, sees each turn and what was delivered into it;
// onSettle, if set, sees each settle's decisions.
func playTurns(ctx context.Context, j ports.Journal, m *metabolome.Metabolome, eps []types.Episode,
	byEp map[types.ID][]types.Outcome, settleEvery int, rule ledger.Rule, budget types.Budget, r *rand.Rand,
	start, end time.Time, onTurn func(ep types.Episode, delivered []types.Lesson),
	onSettle func(turn int, ds []ledger.Decision), drifts []helixsim.Drift) error {
	rel := ledger.NewRelevance()
	settle := func(now time.Time, turn int) error {
		tallies, err := ledger.Credit(ctx, j, m, now, rule, rel)
		if err != nil {
			return err
		}
		m.SetClock(func() time.Time { return now })
		ds, err := ledger.Settle(ctx, m, tallies, rule, false)
		if err != nil {
			return err
		}
		if onSettle != nil {
			onSettle(turn, ds)
		}
		return nil
	}
	file := deliver.Build(m, nil, start)
	for i, ep := range eps {
		sel := deliver.Select(file, ep, budget, r)
		var delivered []types.Lesson
		for _, id := range sel.Delivered {
			e, _ := m.Get(id)
			delivered = append(delivered, e.Lesson)
		}
		if onTurn != nil {
			onTurn(ep, delivered)
		}
		live, liveOuts, err := helixsim.LiveIn(drifts, ep, byEp[ep.ID], delivered)
		if err != nil {
			return err
		}
		live.Exposure, live.Withheld = sel.Delivered, sel.Withheld
		if err := j.AppendEpisode(ctx, live); err != nil {
			return err
		}
		for _, o := range liveOuts {
			if err := j.AppendOutcome(ctx, o); err != nil {
				return err
			}
		}
		if settleEvery > 0 && (i+1)%settleEvery == 0 {
			if err := settle(ep.EndedAt.Add(2*time.Hour), i+1); err != nil {
				return err
			}
			file = deliver.Build(m, nil, ep.EndedAt)
		}
	}
	return settle(end, len(eps))
}
