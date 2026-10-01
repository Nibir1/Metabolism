// Package judge holds host-agnostic judges for the nutrient test.
package judge

import (
	"context"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Heuristic prefers the replay with fewer failed steps, then the one with
// fewer model calls. It is deterministic and needs no model, which makes it
// the baseline every smarter judge is checked against. It cannot tell a
// correct answer from a confident wrong one; that is what model judges are for.
type Heuristic struct{}

var _ ports.Judge = Heuristic{}

// Name implements ports.Judge.
func (Heuristic) Name() string { return "heuristic" }

// Compare implements ports.Judge.
func (Heuristic) Compare(_ context.Context, _ types.Episode, _ []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	fw, fo := failed(with.Steps), failed(without.Steps)
	switch {
	case fw < fo:
		return types.Verdict{Prefer: types.PreferWith, Confidence: 0.9, Reason: "fewer failed steps"}, nil
	case fw > fo:
		return types.Verdict{Prefer: types.PreferWithout, Confidence: 0.9, Reason: "more failed steps"}, nil
	case with.Usage.ModelCalls < without.Usage.ModelCalls:
		return types.Verdict{Prefer: types.PreferWith, Confidence: 0.7, Reason: "fewer model calls"}, nil
	case with.Usage.ModelCalls > without.Usage.ModelCalls:
		return types.Verdict{Prefer: types.PreferWithout, Confidence: 0.7, Reason: "more model calls"}, nil
	}
	return types.Verdict{Prefer: types.PreferNeither, Confidence: 0.5, Reason: "equivalent"}, nil
}

func failed(steps []types.Step) int {
	n := 0
	for _, s := range steps {
		if !s.OK {
			n++
		}
	}
	return n
}
