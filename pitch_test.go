package ambe

import (
	"math"
	"math/rand"
	"testing"
)

// TestRangeMinMatchesScan checks the sparse-table window minimum against a
// direct math.Min scan on every pitch window, for random and tie-heavy inputs.
func TestRangeMinMatchesScan(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	var rm rangeMin
	for trial := 0; trial < 200; trial++ {
		var e pitchErr
		for i := range e {
			e[i] = r.Float64()
			if trial%3 == 0 {
				e[i] = math.Round(e[i]*4) / 4 // many ties
			}
		}
		rm.build(&e)
		for i := 0; i < nPitch; i++ {
			lo, hi := winLo[i], winHi[i]
			if lo > hi {
				t.Fatalf("empty window at %d", i)
			}
			want := math.Inf(1)
			for j := lo; j <= hi; j++ {
				want = math.Min(want, e[j])
			}
			if got := rm.min(lo, hi); math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("window %d [%d,%d]: got %v want %v", i, lo, hi, got, want)
			}
		}
	}
}
