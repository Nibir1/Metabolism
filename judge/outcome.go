package judge

import (
	"context"
	"fmt"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Outcome judges multi-round replays on what happened before asking a model
// what it thinks (D-024).
//
// A replay that ran its reads ended in one of two measurable ways: it
// answered within its rounds, or it ran out of them with no answer. That
// difference is a fact, and comparing facts adds none of the noise a model
// adds when it confidently prefers one of two equally good answers. On the
// first automated day, 9 to 12 of every 14 baseline-against-baseline pairs
// were decisive under a model judge, far too many for any lesson to clear.
//
//   - Both replays measurable, different ends: the one that answered wins.
//   - Both measurable, same end: a tie. Answer quality on equal outcomes is
//     not judged; the credit ledger makes the same trade (D-020).
//   - Otherwise (a first-plan replay, or one that stopped at a step a replay
//     never runs): the plan is the only evidence, and the model judge reads it.
type Outcome struct {
	Model ports.Judge
}

var _ ports.Judge = Outcome{}

// Name implements ports.Judge.
func (o Outcome) Name() string { return "outcome+" + o.Model.Name() }

// measurable reports whether a replay's end is a fact a judge can compare,
// and whether that fact is "answered".
func measurable(r types.ReplayResult) (ok, answered bool) {
	switch r.End {
	case types.ReplayEndAnswered:
		return true, true
	case types.ReplayEndBudget:
		return true, false
	}
	return false, false
}

// Compare implements ports.Judge.
func (o Outcome) Compare(ctx context.Context, original types.Episode, outcomes []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	okW, ansW := measurable(with)
	okWo, ansWo := measurable(without)
	switch {
	case okW && okWo && ansW != ansWo:
		v := types.Verdict{Prefer: types.PreferWith, Confidence: 0.95,
			Reason: fmt.Sprintf("outcome: with the lesson it answered in %d rounds; without, it ran out after %d", with.Rounds, without.Rounds)}
		if ansWo {
			v = types.Verdict{Prefer: types.PreferWithout, Confidence: 0.95,
				Reason: fmt.Sprintf("outcome: without the lesson it answered in %d rounds; with, it ran out after %d", without.Rounds, with.Rounds)}
		}
		return v, nil
	case okW && okWo:
		end := "answered"
		if !ansW {
			end = "ran out of rounds"
		}
		return types.Verdict{Prefer: types.PreferNeither, Confidence: 1,
			Reason: "outcome: both " + end + "; equal outcomes are a tie"}, nil
	}
	return o.Model.Compare(ctx, original, outcomes, with, without)
}
