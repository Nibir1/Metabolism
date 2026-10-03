package bench

import (
	"context"
	"testing"

	"github.com/Nibir1/metabolism/ledger"
)

// The Phase 4c acceptance test (D-027): every correct lesson is planted
// with its paraphrases and rivals, all good alone. Pair decisions leave no
// two rivals in force, eliminate nothing good and set aside no lesson that
// has no pair; without them, rivals stay in force together and turns fail.
func TestPairsSetAsideRivalsAndNothingElse(t *testing.T) {
	if testing.Short() {
		t.Skip("plays thousands of simulated turns")
	}
	seeds := []uint64{1, 2}
	with, err := EvaluatePairs(context.Background(), seeds, 6000, ledger.Rule{})
	if err != nil {
		t.Fatal(err)
	}
	without, err := EvaluatePairs(context.Background(), seeds, 6000, ledger.Rule{WithoutPairs: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range with.Rows {
		t.Logf("seed %d rival=%-5v %-10s %-10s set aside %-5s at %4d  %.40s", r.Seed, r.Conflict, r.OlderEnd, r.NewerEnd, r.Superseded, r.At, r.Newer)
	}
	if with.RivalsBothInForce != 0 {
		t.Errorf("rivals both in force: %d/%d", with.RivalsBothInForce, with.Rivals)
	}
	if with.GoodEliminated != 0 || with.UnpairedSuperseded != 0 {
		t.Errorf("good lessons eliminated %d, unpaired lessons set aside %d", with.GoodEliminated, with.UnpairedSuperseded)
	}
	if without.RivalsBothInForce == 0 || with.ContradictedTurns >= without.ContradictedTurns {
		t.Errorf("without pair decisions: rivals in force %d, contradicted turns %d (with: %d)",
			without.RivalsBothInForce, without.ContradictedTurns, with.ContradictedTurns)
	}
}
