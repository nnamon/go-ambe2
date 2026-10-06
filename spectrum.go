package ambe

import (
	"math"

	"github.com/nnamon/go-ambe2/internal/dsp"
	"github.com/nnamon/go-ambe2/internal/imbe"
	"github.com/nnamon/go-ambe2/quant"
)

const (
	dftN    = 256   // analysis DFT size (TIA-102.BABA eq. 29)
	wrN     = 16384 // window-response DFT size (eq. 30)
	wrOver  = wrN / dftN
	wrHalf  = 110 // w_R spans n = -110..110
	refineM = 50  // first DFT bin in the pitch-refinement error (eq. 24)
)

// spectrum holds S_w(m), the 256-point DFT of one frame of s(n)·w_R(n), and
// the machinery to fit harmonic models to it.
type spectrum struct {
	fft  *dsp.FFT
	buf  [dftN]complex128
	S    [dftN/2 + 1]complex128
	P    [dftN/2 + 1]float64 // |S_w(m)|²
	wr   [wrN/2 + 1]float64  // W_R on the 16384-point grid (real, even)
	wr0  float64             // W_R(0) = Σ w_R(n)
	sumW float64             // Σ w_R(n)
}

func newSpectrum() *spectrum {
	sp := &spectrum{fft: dsp.NewFFT(dftN)}
	for k := range sp.wr {
		v := imbe.WR[wrHalf]
		for n := 1; n <= wrHalf; n++ {
			v += 2 * imbe.WR[wrHalf+n] * math.Cos(2*math.Pi*float64(k*n)/wrN)
		}
		sp.wr[k] = v
	}
	sp.wr0 = sp.wr[0]
	sp.sumW = sp.wr0
	return sp
}

// analyze computes S_w(m) for the 221 samples x (n = -110..110) centred on the frame.
func (sp *spectrum) analyze(x []float64) {
	for i := range sp.buf {
		sp.buf[i] = 0
	}
	for n := -wrHalf; n <= wrHalf; n++ {
		sp.buf[(n+dftN)%dftN] = complex(x[n+wrHalf]*imbe.WR[n+wrHalf], 0)
	}
	sp.fft.Transform(sp.buf[:])
	for m := range sp.S {
		sp.S[m] = sp.buf[m]
		re, im := real(sp.buf[m]), imag(sp.buf[m])
		sp.P[m] = re*re + im*im
	}
}

// W returns W_R(⌊64m − 16384/(2π)·f + 0.5⌋) for harmonic frequency f (radians).
func (sp *spectrum) W(m int, f float64) float64 {
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

// bandBins returns the DFT bins [lo, hi) of harmonic l: ⌈a_l⌉ .. ⌈b_l⌉−1.
func bandBins(l int, w0 float64) (lo, hi int) {
	lo = int(math.Ceil(dftN / (2 * math.Pi) * (float64(l) - 0.5) * w0))
	hi = int(math.Ceil(dftN / (2 * math.Pi) * (float64(l) + 0.5) * w0))
	if lo < 0 {
		lo = 0
	}
	if hi > dftN/2+1 {
		hi = dftN/2 + 1
	}
	return lo, hi
}

// harmonicFit describes how well harmonic l of a candidate fundamental fits S_w.
type harmonicFit struct {
	lo, hi int     // DFT bins of the harmonic's band
	energy float64 // Σ |S_w(m)|² over the band
	err    float64 // Σ |S_w(m) − A_l W_R(...)|² over the band
	wsum2  float64 // Σ W_R(...)² over the band
}

// fit computes per-harmonic fits for l = 1..L of fundamental w0 (eq. 25-28).
func (sp *spectrum) fit(w0 float64, L int, out []harmonicFit) {
	for l := 1; l <= L; l++ {
		lo, hi := bandBins(l, w0)
		f := float64(l) * w0
		var num complex128
		var den, e float64
		for m := lo; m < hi; m++ {
			wv := sp.W(m, f)
			num += sp.S[m] * complex(wv, 0)
			den += wv * wv
			e += sp.P[m]
		}
		h := harmonicFit{lo: lo, hi: hi, energy: e, wsum2: den}
		if den > 0 {
			// |S − A W|² summed = Σ|S|² − |Σ S W|²/Σ W² at the optimum A.
			h.err = e - (real(num)*real(num)+imag(num)*imag(num))/den
		} else {
			h.err = e
		}
		if h.err < 0 {
			h.err = 0
		}
		out[l] = h
	}
}

// imbeL is the IMBE harmonic count for fundamental w0 (eq. 31).
func imbeL(w0 float64) int {
	return int(0.9254 * math.Floor(math.Pi/w0+0.25))
}

// refineError is E_R(w0) (eq. 24): the harmonic-model fit error summed from
// bin refineM up to the band edge of the last harmonic.
func (sp *spectrum) refineError(w0 float64, minBin int, scratch []harmonicFit) float64 {
	L := imbeL(w0)
	if L > quant.MaxL {
		L = quant.MaxL
	}
	sp.fit(w0, L, scratch)
	e := 0.0
	for l := 1; l <= L; l++ {
		h := scratch[l]
		if h.hi <= minBin {
			continue
		}
		if h.lo >= minBin {
			e += h.err
			continue
		}
		// Partial band: compute the residual only over bins ≥ minBin.
		f := float64(l) * w0
		var num complex128
		var den float64
		for m := h.lo; m < h.hi; m++ {
			wv := sp.W(m, f)
			num += sp.S[m] * complex(wv, 0)
			den += wv * wv
		}
		A := num / complex(den, 0)
		for m := minBin; m < h.hi; m++ {
			d := sp.S[m] - A*complex(sp.W(m, f), 0)
			e += real(d)*real(d) + imag(d)*imag(d)
		}
	}
	return e
}

// voicedAmp and unvoicedAmp are the IMBE spectral amplitude estimates
// (eq. 43 and 44) for a harmonic band.
func (sp *spectrum) voicedAmp(h harmonicFit) float64 {
	if h.wsum2 <= 0 {
		return 0
	}
	return math.Sqrt(h.energy / h.wsum2)
}

func (sp *spectrum) unvoicedAmp(h harmonicFit) float64 {
	n := h.hi - h.lo
	if n <= 0 {
		return 0
	}
	return math.Sqrt(h.energy/float64(n)) / sp.sumW
}

// energies returns ξ_LF and ξ_HF (eq. 38-39).
func (sp *spectrum) energies() (lf, hf float64) {
	n := sp.wr0 * sp.wr0
	for m := 0; m <= 63; m++ {
		lf += sp.P[m]
	}
	for m := 64; m <= dftN/2; m++ {
		hf += sp.P[m]
	}
	return lf / n, hf / n
}
