package bench

import (
	"math"
	"slices"

	"github.com/Nibir1/metabolism/episodic"
	"github.com/Nibir1/metabolism/types"
)

// RecallConfig controls EvaluateRecall.
type RecallConfig struct {
	// K is how many cases are retrieved per episode (default 5).
	K int
	// MinHistory is how many earlier episodes must exist before an episode
	// is evaluated (default 20): with no past there is nothing to recall.
	MinHistory int
	// LabelAttr, when set, names an episode attribute that identifies the
	// kind of task (helixsim's "scenario"). It enables SameTaskPrecision.
	// Real hosts have no such label, so the metric is absent for them.
	LabelAttr string
	// Options tunes the index under test.
	Options episodic.Options
}

// RecallResult says whether episodic memory knows anything useful.
//
// The question it answers is falsifiable: given the cases recalled for a new
// request, can you predict whether that request will succeed better than by
// knowing only how often requests succeed in general? If memory carries no
// signal, the two Brier scores come out equal and the result says so.
type RecallResult struct {
	Label     string `json:"label"`
	K         int    `json:"k"`
	Episodes  int    `json:"episodes"`
	Evaluated int    `json:"evaluated"`
	// Covered is the share of evaluated episodes for which at least one case
	// was recalled.
	Covered float64 `json:"covered"`

	// Brier scores (mean squared error of the predicted success
	// probability; lower is better, 0.25 is a coin flip).
	BrierMemory float64 `json:"brier_memory"`
	BrierBase   float64 `json:"brier_base"`
	// Skill is 1 − BrierMemory/BrierBase: the fraction of the base rate's
	// error that memory removes. 0 means no signal; negative means memory
	// misleads.
	Skill float64 `json:"skill"`

	AccuracyMemory float64 `json:"accuracy_memory"`
	AccuracyBase   float64 `json:"accuracy_base"`

	// SameTaskPrecision is the share of recalled cases whose LabelAttr
	// matches the query episode's (-1 when there is no label).
	SameTaskPrecision float64 `json:"same_task_precision"`
}

// EvaluateRecall replays ds in time order. For each episode it recalls cases
// from strictly earlier episodes, using only outcomes known when the episode
// started, then scores two predictions of whether it succeeded:
//
//   - base: the success rate of all earlier episodes (Laplace-smoothed);
//   - memory: the score-weighted success rate of the recalled cases, shrunk
//     toward base with weight 1 so a single weak case cannot dominate.
//
// The ground truth uses every outcome the episode ever got, including late
// ones: that is what "succeeded" means.
func EvaluateRecall(label string, ds Dataset, cfg RecallConfig) RecallResult {
	if cfg.K <= 0 {
		cfg.K = 5
	}
	if cfg.MinHistory <= 0 {
		cfg.MinHistory = 20
	}
	eps := slices.Clone(ds.Episodes)
	slices.SortStableFunc(eps, func(a, b types.Episode) int { return a.StartedAt.Compare(b.StartedAt) })

	res := RecallResult{Label: label, K: cfg.K, Episodes: len(eps), SameTaskPrecision: -1}
	ix := episodic.New(cfg.Options)
	var (
		priorN, priorOK            float64
		brierMem, brierBase        float64
		hitsMem, hitsBase, covered int
		sameTask, recalled         int
	)
	for _, ep := range eps {
		truth := types.Succeeded(ep, ds.Outcomes[ep.ID])
		if int(priorN) >= cfg.MinHistory {
			base := (priorOK + 1) / (priorN + 2)
			cases := ix.Retrieve(episodic.Query{
				Text: ep.Request.Text, Scope: ep.Scope, K: cfg.K, AsOf: ep.StartedAt,
			})
			mem := base
			if len(cases) > 0 {
				covered++
				top := cases[0].Score
				var w, wy float64
				for _, c := range cases {
					cw := c.Score / top
					w += cw
					if c.Succeeded {
						wy += cw
					}
					if cfg.LabelAttr != "" {
						recalled++
						if c.Episode.Attrs[cfg.LabelAttr] == ep.Attrs[cfg.LabelAttr] {
							sameTask++
						}
					}
				}
				mem = (wy + base) / (w + 1)
			}
			y := 0.0
			if truth {
				y = 1
			}
			brierMem += (mem - y) * (mem - y)
			brierBase += (base - y) * (base - y)
			if (mem >= 0.5) == truth {
				hitsMem++
			}
			if (base >= 0.5) == truth {
				hitsBase++
			}
			res.Evaluated++
		}

		ix.Add(ep)
		for _, o := range ds.Outcomes[ep.ID] {
			ix.AddOutcome(o)
		}
		priorN++
		if truth {
			priorOK++
		}
	}

	if res.Evaluated == 0 {
		return res
	}
	n := float64(res.Evaluated)
	res.Covered = float64(covered) / n
	res.BrierMemory = brierMem / n
	res.BrierBase = brierBase / n
	if res.BrierBase > 0 {
		res.Skill = 1 - res.BrierMemory/res.BrierBase
	}
	res.AccuracyMemory = float64(hitsMem) / n
	res.AccuracyBase = float64(hitsBase) / n
	if recalled > 0 {
		res.SameTaskPrecision = float64(sameTask) / float64(recalled)
	}
	res.Skill = math.Round(res.Skill*1e6) / 1e6
	return res
}
