package p25full

import (
	"math"

	"github.com/nnamon/mbevoc/internal/codebook"
)

// model is a frame's reconstructed MBE parameters (before enhancement).
type model struct {
	w0     float64
	L, K   int
	voiced [MaxL + 1]bool
	log2M  [MaxL + 1]float64 // log2 M̃_l, l = 1..L
}

// predictor is the inter-frame state of the spectral amplitude predictor:
// the previous frame's L̃ and log2 M̃_l (§6.3-6.4).  The encoder runs the same
// state as the decoder, so its prediction residuals match the decoder's.
type predictor struct {
	L     int
	log2M [MaxL + 2]float64
}

func newPredictor() predictor { return predictor{L: 30} } // M̃_l(−1) = 1, L̃(−1) = 30

// rho is the prediction coefficient of eq. 55.
func rho(L int) float64 {
	switch {
	case L <= 15:
		return 0.4
	case L <= 24:
		return 0.03*float64(L) - 0.05
	default:
		return 0.7
	}
}

// predicted returns the interpolated previous log2 amplitudes for each l of
// the current frame, (1−δ_l) log2 M̃_⌊k_l⌋(−1) + δ_l log2 M̃_⌊k_l⌋+1(−1)
// (eq. 75-79), and their mean.
func (p *predictor) predicted(L int) (pl [MaxL + 1]float64, mean float64) {
	prev := p.log2M
	for l := p.L + 1; l <= L+1 && l <= MaxL+1; l++ {
		prev[l] = prev[p.L]
	}
	prev[0] = 0 // M̃_0(−1) = 1
	ratio := float64(p.L) / float64(L)
	for l := 1; l <= L; l++ {
		fk := ratio * float64(l)
		k := int(fk)
		d := fk - float64(k)
		v := (1 - d) * prev[k]
		if d != 0 {
			v += d * prev[k+1]
		}
		pl[l] = v
		mean += v
	}
	return pl, mean / float64(L)
}

func (p *predictor) commit(m *model) {
	p.L = m.L
	for l := 1; l <= m.L; l++ {
		p.log2M[l] = m.log2M[l]
	}
}

// dctCos is cos(π(k−1)(j−½)/n).
func dctCos(k, j, n int) float64 {
	return math.Cos(math.Pi * float64(k-1) * (float64(j) - 0.5) / float64(n))
}

// uniform reconstructs a uniform quantizer value (eq. 68 and 71).
func uniform(b uint16, bits int, step float64) float64 {
	if bits == 0 {
		return 0
	}
	return step * (float64(b) - math.Exp2(float64(bits-1)) + 0.5)
}

// uniformIndex quantizes x (eq. 62 and 63).
func uniformIndex(x float64, bits int, step float64) uint16 {
	if bits == 0 {
		return 0
	}
	half := 1 << uint(bits-1)
	v := int(math.Floor(x/step)) + half
	if v < 0 {
		v = 0
	}
	if v > 2*half-1 {
		v = 2*half - 1
	}
	return uint16(v)
}

// hocStep is the step size of higher-order coefficient C_i,k with the given
// bits (Tables 3 and 4).
func hocStep(bits, k int) float64 {
	if bits == 0 {
		return 0
	}
	return codebook.IMBEStep[bits] * codebook.IMBESigma[k]
}

// residual rebuilds T̃_l, l = 1..L, from b2..b_{L+1} (§6.4.1-6.4.2).
func residual(b *Params, L int) (T [MaxL + 1]float64) {
	var G [7]float64
	G[1] = codebook.IMBEGain[b[2]&63]
	for m := 2; m <= 6; m++ {
		G[m] = uniform(b[m+1], codebook.IMBEGainBits[L][m-2], codebook.IMBEGainStep[L][m-2])
	}
	J := codebook.IMBEBlockLen[L]
	hocBits := codebook.IMBEHOCBits[L]
	mi := 8
	l := 1
	for i := 1; i <= 6; i++ {
		var C [11]float64
		for m := 1; m <= 6; m++ { // R̃_i, eq. 69-70
			a := 2.0
			if m == 1 {
				a = 1
			}
			C[1] += a * G[m] * dctCos(m, i, 6)
		}
		n := J[i-1]
		for k := 2; k <= n; k++ {
			bits := hocBits[mi-8]
			C[k] = uniform(b[mi], bits, hocStep(bits, k))
			mi++
		}
		for j := 1; j <= n; j++ { // eq. 73-74
			s := 0.0
			for k := 1; k <= n; k++ {
				a := 2.0
				if k == 1 {
					a = 1
				}
				s += a * C[k] * dctCos(k, j, n)
			}
			T[l] = s
			l++
		}
	}
	return T
}

// dequantize reconstructs the model of a frame with valid b0 and advances
// the predictor (§6.1-6.4).
func (p *predictor) dequantize(b *Params) model {
	var m model
	m.w0, m.L, m.K, _ = Pitch(b[0])
	for l := 1; l <= m.L; l++ {
		k := bandOf(l)
		m.voiced[l] = b[1]>>uint(m.K-k)&1 == 1 // eq. 50-51
	}
	T := residual(b, m.L)
	pl, mean := p.predicted(m.L)
	r := rho(m.L)
	for l := 1; l <= m.L; l++ {
		m.log2M[l] = T[l] + r*pl[l] - r*mean // eq. 77
	}
	p.commit(&m)
	return m
}

// bandOf is the V/UV band of harmonic l (eq. 50).
func bandOf(l int) int {
	if l <= 36 {
		return (l + 2) / 3
	}
	return 12
}

// quantize encodes the target log2 amplitudes (l = 1..L of b0) into
// b2..b_{L+1} (§6.3), then reconstructs the frame exactly as the decoder
// will and advances the predictor.  b[0] and b[1] must already be set.
func (p *predictor) quantize(b *Params, target *[MaxL + 1]float64) model {
	_, L, _, _ := Pitch(b[0])
	pl, mean := p.predicted(L)
	r := rho(L)
	var T [MaxL + 1]float64
	for l := 1; l <= L; l++ {
		T[l] = target[l] - r*pl[l] + r*mean // eq. 54
	}
	J := codebook.IMBEBlockLen[L]
	hocBits := codebook.IMBEHOCBits[L]
	var R [7]float64
	var C [7][11]float64
	l0 := 1
	for i := 1; i <= 6; i++ {
		n := J[i-1]
		for k := 1; k <= n; k++ { // eq. 60
			s := 0.0
			for j := 1; j <= n; j++ {
				s += T[l0+j-1] * dctCos(k, j, n)
			}
			C[i][k] = s / float64(n)
		}
		R[i] = C[i][1]
		l0 += n
	}
	var G [7]float64
	for m := 1; m <= 6; m++ { // eq. 61
		s := 0.0
		for i := 1; i <= 6; i++ {
			s += R[i] * dctCos(m, i, 6)
		}
		G[m] = s / 6
	}
	best := math.Inf(1)
	for i, v := range codebook.IMBEGain {
		if d := math.Abs(G[1] - v); d < best {
			best, b[2] = d, uint16(i)
		}
	}
	for m := 2; m <= 6; m++ {
		b[m+1] = uniformIndex(G[m], codebook.IMBEGainBits[L][m-2], codebook.IMBEGainStep[L][m-2])
	}
	mi := 8
	for i := 1; i <= 6; i++ {
		for k := 2; k <= J[i-1]; k++ {
			bits := hocBits[mi-8]
			b[mi] = uniformIndex(C[i][k], bits, hocStep(bits, k))
			mi++
		}
	}
	return p.dequantize(b)
}
