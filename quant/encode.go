package quant

import (
	"math"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/codebook"
)

// Target is the analysis result the quantizer encodes for one frame.
type Target struct {
	// B0 is the pitch index (0..119) or SilenceB0.
	B0 uint16
	// LogV and LogU are the log2 amplitude targets for harmonics 1..L of B0
	// if the harmonic ends up voiced or unvoiced respectively (LogU already
	// has the decoder's unvoiced scale removed).
	LogV, LogU [MaxL + 1]float64
	// Voicing is the per-500Hz-band probability (0..1) that the band is voiced,
	// and BandWeight how much a wrong decision in that band costs.
	Voicing, BandWeight [8]float64
	// WeightPower selects the spectral amplitude error criterion.  Zero
	// minimises the plain log-magnitude error (exact search).  A positive value
	// weights harmonic l by (M_l/max M)^WeightPower, favouring spectral peaks
	// over valleys, and also chooses the gain under that weighting.
	WeightPower float64
}

// VUVCandidates are the b1 codewords the encoder may choose.  It omits
// entries whose published table rows duplicate another entry (1, 3, 13, 15,
// 17..31): reference decoders render those as partially or softly voiced
// rather than exactly as tabulated, so only codewords with unambiguous
// meaning are used.
var VUVCandidates = []uint16{0, 2, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16}

// Precomputed PRBA codebook contributions to R (index 1..8).
var prbaR24 [512][9]float64
var prbaR58 [128][9]float64

func init() {
	for b := range codebook.PRBA24 {
		var G [9]float64
		copy(G[2:5], codebook.PRBA24[b][:])
		prbaR24[b] = prbaToR(&G)
	}
	for b := range codebook.PRBA58 {
		var G [9]float64
		copy(G[5:9], codebook.PRBA58[b][:])
		prbaR58[b] = prbaToR(&G)
	}
}

// chooseVUV picks b1 minimising the weighted voicing mismatch over the bands
// that contain harmonics.
func chooseVUV(t *Target, f0 float64, L int) uint16 {
	top := BandOf(L, f0)
	best, bestErr := VUVCandidates[0], math.Inf(1)
	for _, c := range VUVCandidates {
		e := 0.0
		for j := 0; j <= top; j++ {
			if codebook.VUV[c][j] == 1 {
				e += t.BandWeight[j] * (1 - t.Voicing[j])
			} else {
				e += t.BandWeight[j] * t.Voicing[j]
			}
		}
		if e < bestErr {
			best, bestErr = c, e
		}
	}
	return best
}

// Quantize chooses b0..b8 for t, advances the predictor exactly as a decoder
// would, and returns the parameters with the reconstructed model.
func (p *Predictor) Quantize(t *Target) (frame.Params, Model) {
	var b frame.Params
	b[0] = t.B0
	kind := KindOf(t.B0)
	w0, L := PitchOf(t.B0)
	f0 := w0 / (2 * math.Pi)

	// Voicing, then the log2 amplitude target it implies.
	var lt [MaxL + 1]float64
	if kind == Voice {
		b[1] = chooseVUV(t, f0, L)
		for l := 1; l <= L; l++ {
			if codebook.VUV[b[1]][BandOf(l, f0)] == 1 {
				lt[l] = t.LogV[l]
			} else {
				lt[l] = t.LogU[l]
			}
		}
	} else {
		b[1] = 16
		copy(lt[1:L+1], t.LogU[1:L+1])
	}

	if t.WeightPower > 0 {
		p.quantizeWeighted(t, &b, &lt, L)
		m, _ := p.Dequantize(b)
		return b, m
	}

	// Gain: mean log2 M = gamma - 0.5*log2(L).
	mean := 0.0
	for l := 1; l <= L; l++ {
		mean += lt[l]
	}
	mean /= float64(L)
	gammaT := mean + 0.5*math.Log2(float64(L))
	dg := gammaT - gammaLeak*p.Gamma
	best := math.Inf(1)
	for i, v := range codebook.Dg {
		if e := math.Abs(dg - v); e < best {
			best, b[2] = e, uint16(i)
		}
	}

	// Prediction residual target X_l; its mean is irrelevant to the decoder.
	pl, _ := p.predicted(L)
	var X [MaxL + 1]float64
	for l := 1; l <= L; l++ {
		X[l] = lt[l] - rho*pl[l]
	}

	// Per-block DCT coefficients C[i][k] (k 1-based) and the R targets.
	J := codebook.BlockLen[L]
	var C [4][MaxL + 1]float64
	var Rt [9]float64
	l0 := 1
	for i := 0; i < 4; i++ {
		n := J[i]
		for k := 1; k <= n; k++ {
			s := 0.0
			for j := 1; j <= n; j++ {
				s += X[l0+j-1] * dctCos(k, j, n)
			}
			C[i][k] = s / float64(n)
		}
		Rt[2*i+1] = C[i][1] + math.Sqrt2*C[i][2]
		Rt[2*i+2] = C[i][1] - math.Sqrt2*C[i][2]
		l0 += n
	}
	cand := searchPRBA(&Rt, J, L, 1)
	b[3], b[4] = cand[0].b3, cand[0].b4

	// Higher-order coefficients, block by block.
	hoc := [4][][4]float64{codebook.HOC1[:], codebook.HOC2[:], codebook.HOC3[:], codebook.HOC4[:]}
	for i := 0; i < 4; i++ {
		kmax := J[i]
		if kmax > 6 {
			kmax = 6
		}
		bi, be := 0, math.Inf(1)
		for c, v := range hoc[i] {
			e := 0.0
			for k := 3; k <= kmax; k++ {
				d := C[i][k] - v[k-3]
				e += d * d
			}
			if e < be {
				bi, be = c, e
			}
		}
		b[5+i] = uint16(bi)
	}

	m, _ := p.Dequantize(b)
	return b, m
}

// prbaCand is a (b3, b4) pair with its unweighted shape error.
type prbaCand struct {
	err    float64
	b3, b4 uint16
}

// prbaQ holds each PRBA58 codevector's contribution to R (elements 1..8)
// contiguously, for the inner loop of searchPRBA.
var prbaQ [len(prbaR58)][8]float64

func init() {
	for i := range prbaR58 {
		copy(prbaQ[i][:], prbaR58[i][1:9])
	}
}

// searchPRBA jointly searches b3, b4 minimising the mean-removed squared
// error in the log-magnitude domain contributed by C[i][1], C[i][2]:
//
//	sum_i J_i (dC_i1^2 + 2 dC_i2^2) - (sum_i J_i dC_i1)^2 / L
//
// and returns the k best pairs, best first.
//
// With dC_i1 = (a+c)/2 and dC_i2 = (a-c)/(2*sqrt 2) for the block's two R
// errors a, c, this equals, with weights v_m = J_block(m)/2 over the eight R
// elements, d = Rt - R(b3) and q = R(b4):
//
//	sum_m v_m (d_m - q_m)^2 - (sum_m v_m (d_m - q_m))^2 / L
//	  = A(b3) + Q(b4) - 2 sum_m v_m d_m q_m - (B(b3) - P(b4))^2 / L
//
// so after per-b3 and per-b4 precomputation each of the 65,536 pairs costs
// one eight-term dot product.
func searchPRBA(Rt *[9]float64, J [4]int, L int, k int) []prbaCand {
	var v [8]float64
	for m := range v {
		v[m] = 0.5 * float64(J[m/2])
	}
	invL := 1 / float64(L)
	var Q, P [len(prbaR58)]float64
	for i4 := range prbaQ {
		q := &prbaQ[i4]
		for m := range v {
			Q[i4] += v[m] * q[m] * q[m]
			P[i4] += v[m] * q[m]
		}
	}
	best := make([]prbaCand, 0, k+1)
	worst := math.Inf(1)
	var vd [8]float64
	for i3 := range prbaR24 {
		r3 := &prbaR24[i3]
		A, B := 0.0, 0.0
		for m := range vd {
			d := Rt[m+1] - r3[m+1]
			vd[m] = v[m] * d
			A += vd[m] * d
			B += vd[m]
		}
		for i4 := range prbaQ {
			q := &prbaQ[i4]
			dot := vd[0]*q[0] + vd[1]*q[1] + vd[2]*q[2] + vd[3]*q[3] +
				vd[4]*q[4] + vd[5]*q[5] + vd[6]*q[6] + vd[7]*q[7]
			s := B - P[i4]
			e := A + Q[i4] - 2*dot - s*s*invL
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

// weightedCandidates is how many of the best unweighted PRBA pairs the
// weighted search re-evaluates.
const weightedCandidates = 32

// quantizeWeighted fills b[2..8] minimising Σ_l w_l (Λ̃_l − lt_l)², where
// w_l = (M_l/max M)^WeightPower.  The decoder's reconstruction is
//
//	Λ̃_l = T_l − mean(T) + ρ(P_l − mean(P)) + g,   g = γ̃ − 0.5 log2 L,
//
// so with Y_l = lt_l − ρ(P_l − mean P) the error is Σ w (T_l − T̄ + g − Y_l)².
// Candidates are the best unweighted PRBA pairs; for each, HOC vectors are
// chosen per block and the gain is re-optimised and quantized.
func (p *Predictor) quantizeWeighted(t *Target, b *frame.Params, lt *[MaxL + 1]float64, L int) {
	var w, Y [MaxL + 1]float64
	mx := math.Inf(-1)
	for l := 1; l <= L; l++ {
		mx = math.Max(mx, lt[l])
	}
	sw := 0.0
	for l := 1; l <= L; l++ {
		w[l] = math.Exp2(t.WeightPower * (lt[l] - mx))
		sw += w[l]
	}
	pl, pm := p.predicted(L)
	var X [MaxL + 1]float64
	for l := 1; l <= L; l++ {
		Y[l] = lt[l] - rho*(pl[l]-pm)
		X[l] = lt[l] - rho*pl[l]
	}

	J := codebook.BlockLen[L]
	var Rt [9]float64
	start := [4]int{}
	l0 := 1
	for i := 0; i < 4; i++ {
		n := J[i]
		start[i] = l0
		var c1, c2 float64
		for j := 1; j <= n; j++ {
			c1 += X[l0+j-1]
			c2 += X[l0+j-1] * dctCos(2, j, n)
		}
		c1 /= float64(n)
		c2 /= float64(n)
		Rt[2*i+1] = c1 + math.Sqrt2*c2
		Rt[2*i+2] = c1 - math.Sqrt2*c2
		l0 += n
	}
	hoc := [4][][4]float64{codebook.HOC1[:], codebook.HOC2[:], codebook.HOC3[:], codebook.HOC4[:]}
	halfLogL := 0.5 * math.Log2(float64(L))

	bestErr := math.Inf(1)
	for _, c := range searchPRBA(&Rt, J, L, weightedCandidates) {
		var G [9]float64
		copy(G[2:5], codebook.PRBA24[c.b3][:])
		copy(G[5:9], codebook.PRBA58[c.b4][:])
		R := prbaToR(&G)
		var T [MaxL + 1]float64
		Tbar := 0.0
		for i := 0; i < 4; i++ {
			n := J[i]
			ci1 := 0.5 * (R[2*i+1] + R[2*i+2])
			ci2 := (R[2*i+1] - R[2*i+2]) / (2 * math.Sqrt2)
			for j := 1; j <= n; j++ {
				T[start[i]+j-1] = ci1 + 2*ci2*dctCos(2, j, n)
			}
			Tbar += float64(n) * ci1
		}
		Tbar /= float64(L)
		g0 := 0.0
		for l := 1; l <= L; l++ {
			g0 += w[l] * (Y[l] - T[l] + Tbar)
		}
		g0 /= sw

		var hb [4]uint16
		for i := 0; i < 4; i++ {
			n := J[i]
			kmax := n
			if kmax > 6 {
				kmax = 6
			}
			bi, be := 0, math.Inf(1)
			for ci, v := range hoc[i] {
				e := 0.0
				for j := 1; j <= n; j++ {
					tj := T[start[i]+j-1]
					for k := 3; k <= kmax; k++ {
						tj += 2 * v[k-3] * dctCos(k, j, n)
					}
					d := tj - Tbar + g0 - Y[start[i]+j-1]
					e += w[start[i]+j-1] * d * d
				}
				if e < be {
					bi, be = ci, e
				}
			}
			hb[i] = uint16(bi)
			v := hoc[i][bi]
			for j := 1; j <= n; j++ {
				for k := 3; k <= kmax; k++ {
					T[start[i]+j-1] += 2 * v[k-3] * dctCos(k, j, n)
				}
			}
		}

		// Optimal continuous gain for this shape, then the nearest gain level.
		gs := 0.0
		for l := 1; l <= L; l++ {
			gs += w[l] * (Y[l] - T[l] + Tbar)
		}
		gs /= sw
		dg := gs + halfLogL - gammaLeak*p.Gamma
		b2, bd := 0, math.Inf(1)
		for i, v := range codebook.Dg {
			if d := math.Abs(dg - v); d < bd {
				b2, bd = i, d
			}
		}
		gq := codebook.Dg[b2] + gammaLeak*p.Gamma - halfLogL
		e := 0.0
		for l := 1; l <= L; l++ {
			d := T[l] - Tbar + gq - Y[l]
			e += w[l] * d * d
		}
		if e < bestErr {
			bestErr = e
			b[2], b[3], b[4] = uint16(b2), c.b3, c.b4
			b[5], b[6], b[7], b[8] = hb[0], hb[1], hb[2], hb[3]
		}
	}
}
