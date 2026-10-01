package bench

import (
	"math"
	"testing"

	"github.com/Nibir1/metabolism/types"
)

func TestScoreAgreementAndKappa(t *testing.T) {
	w, wo, n := types.PreferWith, types.PreferWithout, types.PreferNeither
	perfect := []Labelled{{w, w}, {wo, wo}, {n, n}, {w, w}}
	if a := Score(perfect); a.Rate != 1 || a.Kappa != 1 {
		t.Fatalf("perfect: %+v", a)
	}
	// A judge that always says "with" agrees on every "with" pair and is no
	// better than chance: kappa 0 even though raw agreement is 50%.
	lazy := []Labelled{{w, w}, {w, w}, {wo, w}, {n, w}}
	a := Score(lazy)
	if a.Rate != 0.5 || math.Abs(a.Kappa) > 1e-9 {
		t.Fatalf("always-with judge: rate %v kappa %v", a.Rate, a.Kappa)
	}
	if a.Confused["without→with"] != 1 || a.Confused["neither→with"] != 1 {
		t.Fatalf("confusion %+v", a.Confused)
	}
	if Score(nil).N != 0 {
		t.Fatal("empty")
	}
}
