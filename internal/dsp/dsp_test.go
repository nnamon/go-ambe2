package dsp

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
)

func TestFFTMatchesDFT(t *testing.T) {
	for _, n := range []int{2, 8, 64, 512} {
		f := NewFFT(n)
		r := rand.New(rand.NewSource(int64(n)))
		x := make([]complex128, n)
		for i := range x {
			x[i] = complex(r.NormFloat64(), r.NormFloat64())
		}
		want := make([]complex128, n)
		for k := range want {
			for i, v := range x {
				want[k] += v * cmplx.Rect(1, -2*math.Pi*float64(k*i)/float64(n))
			}
		}
		f.Transform(x)
		for k := range x {
			if cmplx.Abs(x[k]-want[k]) > 1e-9*float64(n) {
				t.Fatalf("n=%d k=%d: %v vs %v", n, k, x[k], want[k])
			}
		}
	}
}

func TestLowpassFIR(t *testing.T) {
	h := LowpassFIR(31, 0.15)
	resp := func(f float64) float64 {
		var s complex128
		for i, v := range h {
			s += complex(v, 0) * cmplx.Rect(1, -2*math.Pi*f*float64(i))
		}
		return cmplx.Abs(s)
	}
	if g := resp(0); math.Abs(g-1) > 1e-12 {
		t.Fatalf("DC gain %g", g)
	}
	if g := resp(0.3); g > 0.01 {
		t.Fatalf("stopband gain %g", g)
	}
}
