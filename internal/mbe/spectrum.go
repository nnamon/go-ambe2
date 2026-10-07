package mbe

import (
	"math"

	"github.com/nnamon/mbevoc/internal/dsp"
)

const (
	DFTN         = 256   // analysis DFT size (TIA-102.BABA eq. 29)
	wrN          = 16384 // window-response DFT size (eq. 30)
	wrOver       = wrN / DFTN
	WRHalf       = 110 // w_R spans n = -110..110
	RefineMinBin = 50  // first DFT bin in the pitch-refinement error (eq. 24)
)

// Spectrum holds S_w(m), the 256-point DFT of one frame of s(n)·w_R(n), and
// the machinery to fit harmonic models to it.
type Spectrum struct {
	fft  *dsp.FFT
	buf  [DFTN]complex128
	S    [DFTN/2 + 1]complex128
	P    [DFTN/2 + 1]float64 // |S_w(m)|²
	wr   [wrN/2 + 1]float64  // W_R on the 16384-point grid (real, even)
	wr0  float64             // W_R(0) = Σ w_R(n)
	sumW float64             // Σ w_R(n)
}

// NewSpectrum returns an analyser for 256-point DFTs of w_R-windowed frames.
func NewSpectrum() *Spectrum {
	sp := &Spectrum{fft: dsp.NewFFT(DFTN)}
	for k := range sp.wr {
		v := WR[WRHalf]
		for n := 1; n <= WRHalf; n++ {
			v += 2 * WR[WRHalf+n] * math.Cos(2*math.Pi*float64(k*n)/wrN)
		}
		sp.wr[k] = v
	}
	sp.wr0 = sp.wr[0]
	sp.sumW = sp.wr0
	return sp
}

// Analyze computes S_w(m) for the 221 samples x (n = -110..110) centred on the frame.
func (sp *Spectrum) Analyze(x []float64) {
	for i := range sp.buf {
		sp.buf[i] = 0
	}
	for n := -WRHalf; n <= WRHalf; n++ {
		sp.buf[(n+DFTN)%DFTN] = complex(x[n+WRHalf]*WR[n+WRHalf], 0)
	}
	sp.fft.Transform(sp.buf[:])
	for m := range sp.S {
		sp.S[m] = sp.buf[m]
		re, im := real(sp.buf[m]), imag(sp.buf[m])
		sp.P[m] = re*re + im*im
	}
}

// W returns W_R(⌊64m − 16384/(2π)·f + 0.5⌋) for harmonic frequency f (radians).
func (sp *Spectrum) W(m int, f float64) float64 {
	k := int(math.Floor(float64(wrOver*m) - wrN/(2*math.Pi)*f + 0.5))
	if k < 0 {
		k = -k
	}
	k %= wrN
	if k > wrN/2 {
		k = wrN - k
	}
	return sp.wr[k]
}

// BandBins returns the DFT bins [lo, hi) of harmonic l: ⌈a_l⌉ .. ⌈b_l⌉−1.
func BandBins(l int, w0 float64) (lo, hi int) {
	lo = int(math.Ceil(DFTN / (2 * math.Pi) * (float64(l) - 0.5) * w0))
	hi = int(math.Ceil(DFTN / (2 * math.Pi) * (float64(l) + 0.5) * w0))
	if lo < 0 {
		lo = 0
	}
	if hi > DFTN/2+1 {
		hi = DFTN/2 + 1
	}
	return lo, hi
}

// HarmonicFit describes how well harmonic l of a candidate fundamental fits S_w.
type HarmonicFit struct {
	Lo, Hi int     // DFT bins of the harmonic's band
	Energy float64 // Σ |S_w(m)|² over the band
	Err    float64 // Σ |S_w(m) − A_l W_R(...)|² over the band
	Wsum2  float64 // Σ W_R(...)² over the band
}

// Fit computes per-harmonic fits for l = 1..L of fundamental w0 (eq. 25-28).
func (sp *Spectrum) Fit(w0 float64, L int, out []HarmonicFit) {
	for l := 1; l <= L; l++ {
		lo, hi := BandBins(l, w0)
		f := float64(l) * w0
		var num complex128
		var den, e float64
		for m := lo; m < hi; m++ {
			wv := sp.W(m, f)
			num += sp.S[m] * complex(wv, 0)
			den += wv * wv
			e += sp.P[m]
		}
		h := HarmonicFit{Lo: lo, Hi: hi, Energy: e, Wsum2: den}
		if den > 0 {
			// |S − A W|² summed = Σ|S|² − |Σ S W|²/Σ W² at the optimum A.
			h.Err = e - (real(num)*real(num)+imag(num)*imag(num))/den
		} else {
			h.Err = e
		}
		if h.Err < 0 {
			h.Err = 0
		}
		out[l] = h
	}
}

// ImbeL is the IMBE harmonic count for fundamental w0 (eq. 31).
func ImbeL(w0 float64) int {
	return int(0.9254 * math.Floor(math.Pi/w0+0.25))
}

// RefineError is E_R(w0) (eq. 24): the harmonic-model fit error summed from
// bin RefineMinBin up to the band edge of the last harmonic.
func (sp *Spectrum) RefineError(w0 float64, minBin int, scratch []HarmonicFit) float64 {
	L := ImbeL(w0)
	if L > MaxL {
		L = MaxL
	}
	sp.Fit(w0, L, scratch)
	e := 0.0
	for l := 1; l <= L; l++ {
		h := scratch[l]
		if h.Hi <= minBin {
			continue
		}
		if h.Lo >= minBin {
			e += h.Err
			continue
		}
		// Partial band: compute the residual only over bins ≥ minBin.
		f := float64(l) * w0
		var num complex128
		var den float64
		for m := h.Lo; m < h.Hi; m++ {
			wv := sp.W(m, f)
			num += sp.S[m] * complex(wv, 0)
			den += wv * wv
		}
		A := num / complex(den, 0)
		for m := minBin; m < h.Hi; m++ {
			d := sp.S[m] - A*complex(sp.W(m, f), 0)
			e += real(d)*real(d) + imag(d)*imag(d)
		}
	}
	return e
}

// VoicedAmp and UnvoicedAmp are the IMBE spectral amplitude estimates
// (eq. 43 and 44) for a harmonic band.
func (sp *Spectrum) VoicedAmp(h HarmonicFit) float64 {
	if h.Wsum2 <= 0 {
		return 0
	}
	return math.Sqrt(h.Energy / h.Wsum2)
}

// UnvoicedAmp is the unvoiced estimate (eq. 44); see VoicedAmp.
func (sp *Spectrum) UnvoicedAmp(h HarmonicFit) float64 {
	n := h.Hi - h.Lo
	if n <= 0 {
		return 0
	}
	return math.Sqrt(h.Energy/float64(n)) / sp.sumW
}

// Energies returns ξ_LF and ξ_HF (eq. 38-39).
func (sp *Spectrum) Energies() (lf, hf float64) {
	n := sp.wr0 * sp.wr0
	for m := 0; m <= 63; m++ {
		lf += sp.P[m]
	}
	for m := 64; m <= DFTN/2; m++ {
		hf += sp.P[m]
	}
	return lf / n, hf / n
}
