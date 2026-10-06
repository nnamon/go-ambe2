package quant

import (
	"math"
	"math/rand"
	"testing"

	"github.com/nnamon/go-ambe2/internal/codebook"
)

// searchPRBADirect is the direct (unexpanded) form of the searchPRBA
// criterion, kept as a reference for testing the optimised search.
func searchPRBADirect(Rt *[9]float64, J [4]int, L int, k int) []prbaCand {
	var w [4]float64
	for i := range w {
		w[i] = float64(J[i])
	}
	invL := 1 / float64(L)
	const k2 = 1.0 / (2 * math.Sqrt2)
	best := make([]prbaCand, 0, k+1)
	worst := math.Inf(1)
	var d24 [9]float64
	for i3 := range prbaR24 {
		r3 := &prbaR24[i3]
		for i := 1; i <= 8; i++ {
			d24[i] = Rt[i] - r3[i]
		}
		for i4 := range prbaR58 {
			r4 := &prbaR58[i4]
			e, s := 0.0, 0.0
			for i := 0; i < 4; i++ {
				a := d24[2*i+1] - r4[2*i+1]
				c := d24[2*i+2] - r4[2*i+2]
				c1 := 0.5 * (a + c)
				c2 := k2 * (a - c)
				e += w[i] * (c1*c1 + 2*c2*c2)
				s += w[i] * c1
			}
			e -= s * s * invL
			if len(best) < k || e < worst {
				c := prbaCand{e, uint16(i3), uint16(i4)}
				j := len(best)
				best = append(best, c)
				for j > 0 && best[j-1].err > e {
					best[j] = best[j-1]
					j--
				}
				best[j] = c
				if len(best) > k {
					best = best[:k]
				}
				if len(best) == k {
					worst = best[k-1].err
				}
			}
		}
	}
	return best
}

// TestSearchPRBAMatchesDirect compares the optimised search with the direct
// form on random targets for every block-length layout: errors must agree to
// rounding, and the ranked candidates must be the same except where two
// candidates' errors are equal to within rounding.
func TestSearchPRBAMatchesDirect(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	swaps := 0
	for n := 0; n < 400; n++ {
		L := 9 + r.Intn(MaxL-8)
		J := codebook.BlockLen[L]
		var Rt [9]float64
		scale := 0.2 + 3*r.Float64()
		for i := 1; i <= 8; i++ {
			Rt[i] = scale * r.NormFloat64()
		}
		got := searchPRBA(&Rt, J, L, weightedCandidates)
		want := searchPRBADirect(&Rt, J, L, weightedCandidates)
		for i := range want {
			if math.Abs(got[i].err-want[i].err) > 1e-9*(1+math.Abs(want[i].err)) {
				t.Fatalf("case %d rank %d: error %v vs direct %v", n, i, got[i].err, want[i].err)
			}
			if got[i].b3 != want[i].b3 || got[i].b4 != want[i].b4 {
				swaps++
			}
		}
		if got[0].b3 != want[0].b3 || got[0].b4 != want[0].b4 {
			if math.Abs(got[0].err-want[1].err) > 1e-9*(1+math.Abs(got[0].err)) {
				t.Fatalf("case %d: best pair differs without a near-tie", n)
			}
		}
	}
	t.Logf("400 random targets: %d ranked candidates (of %d) differ in order", swaps, 400*weightedCandidates)
}

func BenchmarkSearchPRBA(b *testing.B) {
	r := rand.New(rand.NewSource(2))
	var Rt [9]float64
	for i := 1; i <= 8; i++ {
		Rt[i] = r.NormFloat64()
	}
	J := codebook.BlockLen[30]
	for i := 0; i < b.N; i++ {
		searchPRBA(&Rt, J, 30, weightedCandidates)
	}
}

func BenchmarkSearchPRBADirect(b *testing.B) {
	r := rand.New(rand.NewSource(2))
	var Rt [9]float64
	for i := 1; i <= 8; i++ {
		Rt[i] = r.NormFloat64()
	}
	J := codebook.BlockLen[30]
	for i := 0; i < b.N; i++ {
		searchPRBADirect(&Rt, J, 30, weightedCandidates)
	}
}
