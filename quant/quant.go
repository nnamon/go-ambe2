// Package quant implements the AMBE+2 3600x2450 parameter quantizer (and,
// with the DStar codebook, D-STAR's AMBE 3600x2400 one): the
// decoder-side reconstruction of MBE model parameters from b0..b8
// (Predictor.Dequantize) and the encoder-side search that inverts it
// (Predictor.Quantize).  Both share one Predictor so the encoder tracks
// exactly the state a standard decoder will have.
//
// Reconstruction (per the half-rate vocoder description, as implemented by
// mbelib's mbe_decodeAmbe2450Parms):
//
//	log2 M_l = (T_l - mean T) + 0.65*(P_l - mean P) + gamma - 0.5*log2(L)
//	gamma    = Dg[b2] + 0.5*gamma_prev
//
// where P_l is the previous frame's log2 M linearly resampled to the current
// L, and T_l is rebuilt from the PRBA vector (b3, b4) and the higher-order
// DCT coefficients (b5..b8) over four blocks of lengths BlockLen[L].
package quant

import (
	"math"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/codebook"
)

// MaxL is the largest number of harmonics any b0 can signal.
const MaxL = 56

const (
	rho       = 0.65 // log-magnitude prediction coefficient
	gammaLeak = 0.5  // gain prediction coefficient
)

// Kind classifies a frame by its b0 value.
type Kind int

const (
	Voice   Kind = iota // b0 0..119
	Erasure             // b0 120..123
	Silence             // b0 124..125
	Tone                // b0 126..127
)

// SilenceB0 is the b0 value emitted for silence frames.
const SilenceB0 = 124

// KindOf classifies b0.
func KindOf(b0 uint16) Kind {
	switch {
	case b0 < 120:
		return Voice
	case b0 < 124:
		return Erasure
	case b0 < 126:
		return Silence
	default:
		return Tone
	}
}

// Model holds the reconstructed MBE model parameters of one frame.
// Harmonic-indexed arrays use indices 1..L.
type Model struct {
	W0     float64 // fundamental, radians/sample
	L      int
	Voiced [MaxL + 1]bool
	Log2M  [MaxL + 1]float64 // quantized log2 spectral amplitudes (before the unvoiced scale)
	Gamma  float64
}

// Amplitude returns the spectral amplitude a synthesizer would use for
// harmonic l, applying the unvoiced scale 0.2046/sqrt(w0).
func (m *Model) Amplitude(l int) float64 {
	a := math.Exp2(m.Log2M[l])
	if !m.Voiced[l] {
		a *= UnvoicedScale(m.W0)
	}
	return a
}

// UnvoicedScale is the factor a decoder applies to unvoiced amplitudes.
func UnvoicedScale(w0 float64) float64 { return 0.2046 / math.Sqrt(w0) }

// PitchOf returns w0 (radians/sample) and L for an AMBE+2 voice or silence b0.
func PitchOf(b0 uint16) (w0 float64, L int) { return AMBE2.Pitch(b0) }

// BandOf returns the 500 Hz voicing band (0..7) that harmonic l of a
// fundamental of f0 cycles/sample falls in.
func BandOf(l int, f0 float64) int {
	j := int(float64(l) * 16 * f0)
	if j > 7 {
		j = 7
	}
	return j
}

// Predictor is the inter-frame state of the log-magnitude and gain predictors.
//
// Per TIA-102.BABA-1 the state is taken from the last valid voice frame only:
// silence, tone and erasure frames leave it untouched.  (The MD-380 firmware
// decoder was confirmed to behave this way.)  mbelib instead updates the state
// on silence frames and resets it on erasure/tone frames; MbelibCompat
// reproduces that, and exists only for cross-checking against mbelib.
type Predictor struct {
	L            int
	Log2M        [MaxL + 2]float64 // indices 0..L+1 may be read
	Gamma        float64
	MbelibCompat bool

	cb *Codebook
}

// NewPredictor returns an AMBE+2 predictor in its reset state (L=30, all
// log2 M 0, gamma 0).
func NewPredictor() *Predictor { return NewPredictorFor(AMBE2) }

// NewPredictorFor returns a predictor for codebook c in its reset state.
func NewPredictorFor(c *Codebook) *Predictor {
	p := &Predictor{cb: c}
	p.Reset()
	return p
}

// Codebook returns the predictor's codebook.
func (p *Predictor) Codebook() *Codebook {
	if p.cb == nil {
		return AMBE2
	}
	return p.cb
}

// Reset restores the initial state.  (Any constant initial log2 M is
// equivalent, since the prediction is mean-removed; only gamma = 0 matters.)
func (p *Predictor) Reset() {
	*p = Predictor{L: 30, MbelibCompat: p.MbelibCompat, cb: p.cb}
}

// predicted returns P_l (l = 1..L), the previous log2 M resampled to L
// harmonics, and its mean.
func (p *Predictor) predicted(L int) (pl [MaxL + 1]float64, mean float64) {
	prev := p.Log2M
	for l := p.L + 1; l <= L && l <= MaxL+1; l++ {
		prev[l] = prev[p.L]
	}
	prev[0] = prev[1]
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

// dctCos returns cos(pi*(k-1)*(j-0.5)/n) for 1-based j, k <= n, from a
// table built once (the quantizer search evaluates it ~10^5 times a frame).
func dctCos(k, j, n int) float64 {
	return cosTab[(n*cosStride+k)*cosStride+j]
}

// cosTab holds cos(pi*(k-1)*(j-0.5)/n) for every DCT length n the codec uses
// (the block lengths of codebook.BlockLen and the 8-point PRBA transform),
// computed with exactly the expression the direct evaluation would use.
// It is a package-level initializer (not init) so that it is ready before the
// init functions that precompute codebook transforms run.
var cosStride, cosTab = buildCosTab()

func buildCosTab() (int, []float64) {
	maxN := 8
	for _, row := range codebook.BlockLen {
		for _, n := range row {
			if n > maxN {
				maxN = n
			}
		}
	}
	stride := maxN + 1
	tab := make([]float64, stride*stride*stride)
	for n := 1; n <= maxN; n++ {
		for k := 1; k <= n; k++ {
			for j := 1; j <= n; j++ {
				tab[(n*stride+k)*stride+j] = math.Cos(math.Pi * float64(k-1) * (float64(j) - 0.5) / float64(n))
			}
		}
	}
	return stride, tab
}

// residual rebuilds T_l (l = 1..L) from b3..b8.
func (c *Codebook) residual(b frame.Params, L int) (T [MaxL + 1]float64) {
	var G [9]float64
	G[2], G[3], G[4] = c.prba24[b[3]][0], c.prba24[b[3]][1], c.prba24[b[3]][2]
	for i := 0; i < 4; i++ {
		G[5+i] = c.prba58[b[4]][i]
	}
	R := prbaToR(&G)
	J := c.blockLen[L]
	hoc := [4][4]float64{c.hoc[0][b[5]], c.hoc[1][b[6]], c.hoc[2][b[7]], c.hoc[3][b[8]]}
	l := 1
	for i := 0; i < 4; i++ {
		var C [MaxL + 1]float64
		C[1] = 0.5 * (R[2*i+1] + R[2*i+2])
		C[2] = (R[2*i+1] - R[2*i+2]) / (2 * math.Sqrt2)
		for k := 3; k <= J[i] && k <= 6; k++ {
			C[k] = hoc[i][k-3]
		}
		n := J[i]
		for j := 1; j <= n; j++ {
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

// prbaToR is the 8-point inverse DCT taking the PRBA vector G (1-based) to R (1-based).
func prbaToR(G *[9]float64) (R [9]float64) {
	for i := 1; i <= 8; i++ {
		s := 0.0
		for m := 1; m <= 8; m++ {
			a := 2.0
			if m == 1 {
				a = 1
			}
			s += a * G[m] * dctCos(m, i, 8)
		}
		R[i] = s
	}
	return R
}

// Dequantize reconstructs the model for one frame and, for voice frames,
// advances the predictor.  Erasure and tone frames carry no model and return
// a zero Model.
func (p *Predictor) Dequantize(b frame.Params) (m Model, kind Kind) {
	cb := p.Codebook()
	kind = cb.Kind(b[0])
	if kind == Erasure || kind == Tone {
		if p.MbelibCompat {
			p.Reset()
		}
		return m, kind
	}
	m.W0, m.L = cb.Pitch(b[0])
	if kind == Voice {
		f0 := cb.f0[b[0]]
		for l := 1; l <= m.L; l++ {
			m.Voiced[l] = cb.vuv[b[1]][BandOf(l, f0)] == 1
		}
	}
	m.Gamma = cb.dg[b[2]] + gammaLeak*p.Gamma

	T := cb.residual(b, m.L)
	meanT := 0.0
	for l := 1; l <= m.L; l++ {
		meanT += T[l]
	}
	meanT /= float64(m.L)
	pl, meanP := p.predicted(m.L)
	offset := m.Gamma - 0.5*math.Log2(float64(m.L)) - meanT - rho*meanP
	for l := 1; l <= m.L; l++ {
		m.Log2M[l] = T[l] + rho*pl[l] + offset
	}
	if kind == Voice || p.MbelibCompat {
		p.commit(&m)
	}
	return m, kind
}

func (p *Predictor) commit(m *Model) {
	p.L = m.L
	p.Gamma = m.Gamma
	for l := 1; l <= m.L; l++ {
		p.Log2M[l] = m.Log2M[l]
	}
}
