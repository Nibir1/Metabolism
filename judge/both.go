package judge

import (
	"context"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// PairJudge judges a pair as presented (judge.LLM, the Jev adapter).
type PairJudge interface {
	Name() string
	ComparePair(ctx context.Context, pair Pair) (types.Verdict, error)
}

// Both asks its inner judge twice, with the plans in both orders, and calls
// a winner only when the two answers agree; otherwise the pair is a tie.
//
// A model judge reading two answers finds a difference almost every time:
// under D-033 it was decisive on 18 to 23 of 26 baseline-against-baseline
// pairs, two replays of the same plan. A preference that flips when the
// plans swap places was the order, or noise, not the plans. It costs two
// calls per pair.
type Both struct {
	Inner PairJudge
}

var _ ports.Judge = Both{}

// Name implements ports.Judge.
func (b Both) Name() string { return "both+" + b.Inner.Name() }

// Compare implements ports.Judge.
func (b Both) Compare(ctx context.Context, original types.Episode, outcomes []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	return b.ComparePair(ctx, NewPair(original, outcomes, with, without))
}

// ComparePair judges the pair as presented, and again swapped.
func (b Both) ComparePair(ctx context.Context, pair Pair) (types.Verdict, error) {
	first, err := b.Inner.ComparePair(ctx, pair)
	if err != nil {
		return types.Verdict{}, err
	}
	swapped := pair
	swapped.PlanA, swapped.PlanB = pair.PlanB, pair.PlanA
	swapped.WithIsA = !pair.WithIsA
	second, err := b.Inner.ComparePair(ctx, swapped)
	if err != nil {
		return types.Verdict{}, err
	}
	if first.Prefer != second.Prefer {
		return types.Verdict{Prefer: types.PreferNeither, Confidence: 1,
			Reason: "the two orders disagree (" + string(first.Prefer) + ", then " + string(second.Prefer) + "): " + first.Reason}, nil
	}
	return types.Verdict{Prefer: first.Prefer, Confidence: min(first.Confidence, second.Confidence),
		Reason: "both orders: " + first.Reason}, nil
}
