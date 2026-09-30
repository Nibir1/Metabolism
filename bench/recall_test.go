package bench

import (
	"testing"
	"time"

	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/types"
)

func simDataset(seed uint64, n int) Dataset {
	eps, outs := helixsim.Generate(helixsim.Config{Seed: seed, Count: n, Start: t0, Span: 56 * 24 * time.Hour})
	ds := Dataset{Episodes: eps, Outcomes: map[types.ID][]types.Outcome{}}
	for _, o := range outs {
		ds.Outcomes[o.EpisodeID] = append(ds.Outcomes[o.EpisodeID], o)
	}
	return ds
}

// On the simulator, request wording predicts the scenario and the scenario
// largely decides the outcome, so memory must beat the base rate. If this
// fails, retrieval or the evaluation is broken.
func TestRecallBeatsBaseRateWhereThereIsSignal(t *testing.T) {
	r := EvaluateRecall("sim", simDataset(1, 600), RecallConfig{LabelAttr: "scenario"})
	if r.Evaluated != 600-20 {
		t.Fatalf("evaluated %d, want %d", r.Evaluated, 600-20)
	}
	if r.Skill <= 0.2 || r.BrierMemory >= r.BrierBase {
		t.Fatalf("memory should carry signal on the simulator: %+v", r)
	}
	if r.SameTaskPrecision < 0.8 || r.Covered < 0.9 {
		t.Fatalf("recall should find the same task: %+v", r)
	}
}

// With outcomes assigned at random, there is nothing to learn: memory must
// not claim skill it does not have. This is the test that keeps the metric
// honest.
func TestRecallFindsNoSignalInNoise(t *testing.T) {
	ds := simDataset(2, 600)
	ds.Outcomes = map[types.ID][]types.Outcome{}
	for i := range ds.Episodes {
		ep := &ds.Episodes[i]
		// A fixed pseudo-random bit per episode, independent of its text.
		if (i*7919+13)%3 == 0 {
			ep.End = types.RunFailed
		} else {
			ep.End = types.RunDone
		}
	}
	r := EvaluateRecall("noise", ds, RecallConfig{})
	if r.Skill > 0.02 {
		t.Fatalf("memory claimed skill on noise: %+v", r)
	}
}

func TestRecallNeedsHistory(t *testing.T) {
	r := EvaluateRecall("tiny", simDataset(3, 10), RecallConfig{})
	if r.Evaluated != 0 || r.SameTaskPrecision != -1 {
		t.Fatalf("with less history than MinHistory nothing is evaluated: %+v", r)
	}
}
