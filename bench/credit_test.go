package bench

import (
	"context"
	"testing"

	"github.com/Nibir1/metabolism/ledger"
)

// The Phase 3 acceptance test for the credit ledger (D-020), on the
// simulator: every lesson the simulator knows the truth about is planted in
// trial, delivered by coin flip over 3000 turns, and settled every 100 turns,
// the frequent looking that inflates false decisions under a p-value rule.
// What helps should be accepted, what harms always eliminated, and what
// changes no outcome never moved, at least not with these seeds. Crediting
// only the turns that bear on a lesson (D-026) took acceptance from 19 to 25
// of 25; over every turn the help is diluted.
func TestCreditLedgerOnTheSimulator(t *testing.T) {
	if testing.Short() {
		t.Skip("plays thousands of simulated turns")
	}
	res, err := EvaluateCredit(context.Background(), []uint64{1, 2, 3, 4, 5}, 3000, 100, ledger.Rule{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Rows {
		e, w := r.Tally.Rate()
		t.Logf("seed %d %-9s → %-11s at %4d  %3.0f%% of %4d vs %3.0f%% of %4d  %.45s",
			r.Seed, r.Expect, r.Final, r.At, 100*e, r.Tally.Exposed, 100*w, r.Tally.Withheld, r.Text)
	}
	t.Logf("help accepted %d/%d, harm eliminated %d/%d, neutral moved %d/%d",
		res.HelpAccepted, res.HelpTotal, res.HarmEliminated, res.HarmTotal, res.NeutralWrongMoves, res.NeutralTotal)
	if res.HarmEliminated != res.HarmTotal {
		t.Errorf("harmful lessons eliminated %d/%d", res.HarmEliminated, res.HarmTotal)
	}
	if res.NeutralWrongMoves != 0 {
		t.Errorf("lessons that change no outcome moved %d/%d", res.NeutralWrongMoves, res.NeutralTotal)
	}
	if res.HelpAcceptRate < 0.9 {
		t.Errorf("helpful lessons accepted %d/%d", res.HelpAccepted, res.HelpTotal)
	}
	// Cost (D-030): what only adds work is always eliminated; what only
	// saves work is mostly accepted (the success guard can hold one back
	// when other lessons make its turns fail).
	if res.CostlierEliminated != res.CostlierTotal || res.CostlierTotal == 0 {
		t.Errorf("costlier lessons eliminated %d/%d", res.CostlierEliminated, res.CostlierTotal)
	}
	if res.CheaperAccepted*10 < res.CheaperTotal*6 {
		t.Errorf("cheaper lessons accepted %d/%d", res.CheaperAccepted, res.CheaperTotal)
	}
}
