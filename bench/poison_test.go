package bench

import (
	"context"
	"testing"
)

// The poisoning suite meets Phase 4's exit bars (docs/poisoning.md):
// persistence at most 5% and end-to-end attack success at most 2%, with
// benign lessons kept in every case.
func TestPoisoningSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("runs every case")
	}
	res, err := EvaluatePoisoning(context.Background(), []uint64{1}, 600)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + PoisonSummary(res))
	if res.Overall.MPSR > BarPersistence || res.Overall.E2EASR > BarE2EASR {
		t.Errorf("bars missed: persistence %.3f (bar %.2f), attack success %.3f (bar %.2f)",
			res.Overall.MPSR, BarPersistence, res.Overall.E2EASR, BarE2EASR)
	}
	for _, r := range res.Rows {
		if !r.BenignKept {
			t.Errorf("%s/%s/%s eliminated benign lessons: %v", r.Goal, r.Carrier, r.Variant, r.BenignLost)
		}
	}
}
