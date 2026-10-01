package bench

import "github.com/Nibir1/metabolism/types"

// Labelled is one comparison a person judged blind, beside the judge's call.
type Labelled struct {
	Human types.Preference
	Judge types.Preference
}

// Agreement is how often a judge agrees with a person on the same pairs: the
// Phase 2 exit criterion (≥ 80%, or replace the judge).
type Agreement struct {
	N        int            `json:"n"`
	Agree    int            `json:"agree"`
	Rate     float64        `json:"rate"`
	Kappa    float64        `json:"kappa"`    // Cohen's κ: agreement beyond what chance would give
	Confused map[string]int `json:"confused"` // "human→judge" counts for disagreements
}

// Score computes agreement and Cohen's kappa over three classes (with,
// without, neither). Kappa matters because raw agreement flatters a judge
// when one answer dominates: if most pairs are "neither", agreeing on them
// says little.
func Score(ls []Labelled) Agreement {
	a := Agreement{N: len(ls), Confused: map[string]int{}}
	if a.N == 0 {
		return a
	}
	classes := []types.Preference{types.PreferWith, types.PreferWithout, types.PreferNeither}
	human, judge := map[types.Preference]int{}, map[types.Preference]int{}
	for _, l := range ls {
		human[l.Human]++
		judge[l.Judge]++
		if l.Human == l.Judge {
			a.Agree++
		} else {
			a.Confused[string(l.Human)+"→"+string(l.Judge)]++
		}
	}
	n := float64(a.N)
	a.Rate = float64(a.Agree) / n
	pe := 0.0
	for _, c := range classes {
		pe += (float64(human[c]) / n) * (float64(judge[c]) / n)
	}
	if pe < 1 {
		a.Kappa = (a.Rate - pe) / (1 - pe)
	} else if a.Rate == 1 {
		a.Kappa = 1
	}
	return a
}
