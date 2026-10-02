package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
	"github.com/Nibir1/metabolism/verify"
)

// NutrientRow is one planted lesson and what the test made of it.
type NutrientRow struct {
	Seed    uint64           `json:"seed"`
	Text    string           `json:"text"`
	Good    bool             `json:"good"`
	Why     string           `json:"why"`
	Result  types.TestResult `json:"result"`
	Summary string           `json:"summary"`
}

// NutrientResult is the Phase 2 exit measurement on the simulator: how well
// the nutrient test tells lessons that help from lessons that don't.
type NutrientResult struct {
	Seeds int    `json:"seeds"`
	Count int    `json:"episodes_per_seed"`
	Judge string `json:"judge"`

	WrongTotal      int     `json:"wrong_total"`
	WrongRejected   int     `json:"wrong_rejected"`
	WrongRejectRate float64 `json:"wrong_reject_rate"` // exit criterion: ≥ 0.9

	CorrectTotal      int     `json:"correct_total"`
	CorrectAccepted   int     `json:"correct_accepted"`
	CorrectAcceptRate float64 `json:"correct_accept_rate"`

	Inconclusive int           `json:"inconclusive"`
	Rows         []NutrientRow `json:"rows"`
}

// EvaluateNutrient plants every lesson the simulator knows the truth about,
// in a fresh simulated history per seed, and runs the nutrient test on each.
// Inconclusive results count against both rates: a test that cannot decide
// has neither rejected the wrong lesson nor accepted the right one.
//
// With a model judge (jd non-nil) it doubles as a judge check: the simulator
// knows which lessons are right, so the rates say how often the judge's
// verdicts lead the test to the right decision.
func EvaluateNutrient(ctx context.Context, seeds []uint64, count int, cfg verify.Config, jd ports.Judge) (NutrientResult, error) {
	if jd == nil {
		jd = judge.Heuristic{}
	}
	res := NutrientResult{Seeds: len(seeds), Count: count, Judge: jd.Name()}
	dir, err := os.MkdirTemp("", "metabolism-nutrient-")
	if err != nil {
		return res, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	for _, seed := range seeds {
		j, err := sqlite.Open(ctx, filepath.Join(dir, fmt.Sprintf("seed-%d.db", seed)))
		if err != nil {
			return res, err
		}
		eps, outs := helixsim.Generate(helixsim.Config{Seed: seed, Count: count,
			Start: time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC), Span: 56 * 24 * time.Hour})
		for _, e := range eps {
			if err := j.AppendEpisode(ctx, e); err != nil {
				return res, err
			}
		}
		for _, o := range outs {
			if err := j.AppendOutcome(ctx, o); err != nil {
				return res, err
			}
		}
		h, err := verify.LoadHistory(ctx, j)
		if err != nil {
			return res, err
		}
		m, err := metabolome.Load(ctx, j)
		if err != nil {
			return res, err
		}
		v := &verify.Verifier{Host: helixsim.Host{}, Judge: jd, History: h, Config: cfg}

		specs := append(helixsim.CorrectLessons(), helixsim.WrongLessons()...)
		for i, s := range specs {
			var src []types.ID
			for _, ep := range eps {
				if ep.Attrs["scenario"] == s.From && ep.Request.Provenance == types.ProvUserTyped && len(src) < 3 {
					src = append(src, ep.ID)
				}
			}
			if len(src) == 0 {
				continue
			}
			l, err := m.Create(ctx, types.Lesson{ID: plantedID(seed, i), Kind: s.Kind, Scope: types.Scope{Level: types.ScopeGlobal},
				Text: s.Text, Sources: src, Provenance: types.ProvUserTyped, Origin: "planted"}, "planted by nutrient-eval")
			if err != nil {
				return res, err
			}
			rep, err := v.Test(ctx, m, l.ID)
			if err != nil {
				return res, err
			}
			r := rep.Evidence.Result
			res.Rows = append(res.Rows, NutrientRow{Seed: seed, Text: s.Text, Good: s.Good, Why: s.Why,
				Result: r, Summary: rep.Evidence.Summary})
			if r == types.ResultInconclusive {
				res.Inconclusive++
			}
			if s.Good {
				res.CorrectTotal++
				if r == types.ResultPass {
					res.CorrectAccepted++
				}
			} else {
				res.WrongTotal++
				if r == types.ResultFail {
					res.WrongRejected++
				}
			}
		}
		_ = j.Close()
	}
	res.WrongRejectRate = float64(res.WrongRejected) / float64(max(res.WrongTotal, 1))
	res.CorrectAcceptRate = float64(res.CorrectAccepted) / float64(max(res.CorrectTotal, 1))
	return res, nil
}

// plantedID is a planted lesson's ID, fixed by the seed and the lesson's
// place in the catalogue. The nutrient test orders its sample of past
// successes by a hash of the lesson's ID (verify.mix), so a random ID would
// draw a different sample on every run: the decisions held, but the counts
// in nutrient.json changed each time it was regenerated. Real lessons keep
// random IDs.
func plantedID(seed uint64, i int) types.ID {
	return types.ID(fmt.Sprintf("%012x%016x", seed&0xffffffffffff, uint64(i)))
}
