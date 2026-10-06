package ambe

import (
	"math"

	"github.com/nnamon/go-ambe2/internal/imbe"
)

// Pitch-period grid for the initial estimate: P = pMin, pMin+pStep, ..., pMax samples.
const (
	pMin    = 21.0
	pMax    = 122.0
	pStep   = 0.5
	nPitch  = int((pMax-pMin)/pStep) + 1
	halfWin = 150 // the pitch window spans [-halfWin, halfWin] around the frame centre
)

func gridP(i int) float64 { return pMin + pStep*float64(i) }

func gridIndex(P float64) int {
	i := int(math.Round((P - pMin) / pStep))
	if i < 0 {
		return 0
	}
	if i >= nPitch {
		return nPitch - 1
	}
	return i
}

// pitchErr is the normalised periodicity error E(P) of one frame on the grid.
// E is ~0 for a signal exactly periodic in P and ~1 for white noise.
type pitchErr [nPitch]float64

// pitchAnalyzer computes E(P), the error of the best fit of the windowed,
// lowpassed signal by a P-periodic sequence (TIA-102.BABA eq. 5-8):
//
//	E(P) = [Σ s²w² − P Σ_{|nP|≤150} r(nP)] / [Σ s²w² (1 − P Σ w⁴)],
//	r(t) = Σ_j s(j)w²(j) s(j+t)w²(j+t),  with Σ w² = 1 (w = w_I).
type pitchAnalyzer struct {
	w2    [2*halfWin + 1]float64
	sumW4 float64
	u     [2*halfWin + 1]float64
	r     [2*halfWin + 2]float64
}

func newPitchAnalyzer() *pitchAnalyzer {
	pa := &pitchAnalyzer{}
	for i, w := range imbe.WI {
		pa.w2[i] = w * w
		pa.sumW4 += pa.w2[i] * pa.w2[i]
	}
	return pa
}

// analyze computes E(P) for the 2*halfWin+1 samples x centred on the frame.
func (pa *pitchAnalyzer) analyze(x []float64) (e pitchErr) {
	n := len(pa.u)
	energy := 0.0
	for i := 0; i < n; i++ {
		pa.u[i] = x[i] * pa.w2[i]
		energy += x[i] * x[i] * pa.w2[i]
	}
	if energy < 1e-3 {
		for i := range e {
			e[i] = 1
		}
		return e
	}
	for t := 0; t < n; t++ {
		s := 0.0
		for j := 0; j+t < n; j++ {
			s += pa.u[j] * pa.u[j+t]
		}
		pa.r[t] = s
	}
	pa.r[n] = 0
	rAt := func(t float64) float64 {
		k := int(t)
		if k >= n {
			return 0
		}
		f := t - float64(k)
		return (1-f)*pa.r[k] + f*pa.r[k+1]
	}
	for i := range e {
		P := gridP(i)
		s := pa.r[0]
		for m := 1; float64(m)*P <= halfWin; m++ {
			s += 2 * rAt(float64(m)*P)
		}
		e[i] = (energy - P*s) / (energy * (1 - P*pa.sumW4))
	}
	return e
}

// pitchTracker turns per-frame E(P) into an initial pitch estimate with
// look-back (previous two decisions) and look-ahead (future frames) tracking.
type pitchTracker struct {
	prevP [2]float64 // P(-1), P(-2)
	prevE [2]float64 // E(-1)(P(-1)), E(-2)(P(-2))
	rm    rangeMin
}

func newPitchTracker() *pitchTracker {
	return &pitchTracker{prevP: [2]float64{100, 100}}
}

// rangeIdx returns the grid index range covering [0.8P, 1.2P].
func rangeIdx(P float64) (lo, hi int) {
	lo = int(math.Ceil((0.8*P - pMin) / pStep))
	hi = int(math.Floor((1.2*P - pMin) / pStep))
	if lo < 0 {
		lo = 0
	}
	if hi >= nPitch {
		hi = nPitch - 1
	}
	return lo, hi
}

// subMultiple returns the grid index of the smallest sub-multiple P/n of
// grid pitch i whose cumulative error satisfies TIA-102.BABA eq. 18-20
// relative to that of P, or i itself if none does.  The ratio's denominator
// is floored at 0.05 (the level eq. 20 treats as a perfect fit): E(P) can come
// out at or below zero at multiples of the period of strongly periodic input,
// which would otherwise make every ratio test fail.
func subMultiple(i int, ce *pitchErr) int {
	for n := int(gridP(i) / pMin); n >= 2; n-- {
		j := gridIndex(gridP(i) / float64(n))
		c, ratio := ce[j], ce[j]/math.Max(ce[i], 0.05)
		if (c <= 0.85 && ratio <= 1.7) || (c <= 0.4 && ratio <= 3.5) || c <= 0.05 {
			return j
		}
	}
	return i
}

// winLo/winHi give, for each grid pitch, the grid range [0.8P, 1.2P].
var winLo, winHi [nPitch]int

func init() {
	for i := range winLo {
		winLo[i], winHi[i] = rangeIdx(gridP(i))
	}
}

// rangeMin answers min(e[lo..hi]) for many windows via a sparse table.
// math.Min is used throughout, so the result is exactly what a linear scan
// with math.Min returns.
type rangeMin struct {
	t [8][nPitch]float64 // t[k][i] = min(e[i .. i+2^k-1])
}

func (r *rangeMin) build(e *pitchErr) {
	r.t[0] = *e
	for k := 1; k < len(r.t); k++ {
		h := 1 << uint(k-1)
		for i := 0; i+2*h <= nPitch; i++ {
			r.t[k][i] = math.Min(r.t[k-1][i], r.t[k-1][i+h])
		}
	}
}

func (r *rangeMin) min(lo, hi int) float64 {
	n := hi - lo + 1
	k := 0
	for 2<<uint(k) <= n {
		k++
	}
	return math.Min(r.t[k][lo], r.t[k][hi-(1<<uint(k))+1])
}

// track picks the initial pitch P_I for the frame with error e0, given the
// errors of up to two following frames, and returns it with E(P_I).
func (pt *pitchTracker) track(e0 *pitchErr, future []*pitchErr) (float64, float64) {
	// Look-back: best P near the previous decision.
	lo, hi := rangeIdx(pt.prevP[0])
	iB := lo
	for i := lo; i <= hi; i++ {
		if e0[i] < e0[iB] {
			iB = i
		}
	}
	ceB := e0[iB] + pt.prevE[0] + pt.prevE[1]

	// Look-ahead: CE_F(P0) = E0(P0) + min over a smooth path through the future frames,
	// rescaled to a three-frame sum.
	var ceF pitchErr
	switch len(future) {
	case 0:
		for i := range ceF {
			ceF[i] = 3 * e0[i]
		}
	case 1:
		pt.rm.build(future[0])
		for i := range ceF {
			ceF[i] = 1.5 * (e0[i] + pt.rm.min(winLo[i], winHi[i]))
		}
	default:
		e1 := future[0]
		pt.rm.build(future[1])
		var m2 pitchErr
		for j := range m2 {
			m2[j] = e1[j] + pt.rm.min(winLo[j], winHi[j])
		}
		pt.rm.build(&m2)
		for i := range ceF {
			ceF[i] = e0[i] + pt.rm.min(winLo[i], winHi[i])
		}
	}
	iF := 0
	for i := range ceF {
		if ceF[i] < ceF[iF] {
			iF = i
		}
	}
	// Prefer the smallest sub-multiple of the look-ahead estimate that is
	// nearly as good, to avoid period doubling (TIA-102.BABA eq. 18-20).
	best := subMultiple(iF, &ceF)

	choose := iB
	if ceB > 0.48 && ceB > ceF[best] {
		choose = best
	}
	// Deviation from TIA-102.BABA: the look-back estimate gets the same
	// sub-multiple test.  For strongly periodic input E(P) is ~0 at every
	// multiple of the period, and without this the look-back tracker can lock
	// onto a multiple (e.g. from its initial P = 100) indefinitely.
	if choose == iB {
		choose = subMultiple(iB, &ceF)
	}
	P := gridP(choose)
	pt.prevP[1], pt.prevE[1] = pt.prevP[0], pt.prevE[0]
	pt.prevP[0], pt.prevE[0] = P, e0[choose]
	return P, e0[choose]
}
