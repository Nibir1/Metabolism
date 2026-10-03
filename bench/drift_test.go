package bench

import (
	"context"
	"testing"
)

// The Phase 4b acceptance test (D-026): the simulator's correct lessons earn
// acceptance, then the world changes so two of them stop holding. Every
// drifted lesson that was accepted is retired, and no lesson that still
// holds is.
func TestDriftRetiresWhatStoppedHolding(t *testing.T) {
	if testing.Short() {
		t.Skip("plays thousands of simulated turns")
	}
	res, err := EvaluateDrift(context.Background(), []uint64{1, 2, 3}, 3000, 3000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Rows {
		t.Logf("seed %d drifted=%-5v %-8s → %-10s after %4d  %.45s", r.Seed, r.Drifted, r.AtDrift, r.Final, r.After, r.Text)
	}
	if res.DriftedAccepted == 0 {
		t.Fatal("no drifted lesson was accepted before the change: nothing was tested")
	}
	if res.DriftedRetired != res.DriftedAccepted {
		t.Errorf("drifted lessons retired %d/%d", res.DriftedRetired, res.DriftedAccepted)
	}
	if res.FalseRetirements != 0 {
		t.Errorf("lessons that still hold retired %d/%d", res.FalseRetirements, res.ControlsAccepted)
	}
}
