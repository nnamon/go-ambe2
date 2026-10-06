// Package ambe is a from-scratch AMBE+2 3600x2450 (DMR / NXDN / P25 Phase 2
// half-rate) speech encoder.
//
// Analysis follows the MBE analysis of TIA-102.BABA chapter 5 (pitch
// estimation with look-back/look-ahead tracking, pitch refinement, V/UV
// determination, spectral amplitude estimation) and quantization follows the
// half-rate vocoder description of TIA-102.BABA-1 chapter 4.  The output is
// the 49-bit voice parameter frame (see package frame); FEC and interleaving
// for a particular air interface are separate.
package ambe

import (
	"math"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/codebook"
	"github.com/nnamon/go-ambe2/internal/imbe"
	"github.com/nnamon/go-ambe2/quant"
)

// FrameSamples is the number of 8 kHz samples per 20 ms frame.
const FrameSamples = 160

// Config tunes the encoder; DefaultConfig gives the standard behaviour.
type Config struct {
	// Lookahead is the number of future frames (0..2) used for pitch
	// tracking.  2 is the TIA-102.BABA algorithm; fewer frames lower the delay.
	Lookahead int
	// RefineMinBin is the first 256-point DFT bin used in the pitch
	// refinement error (TIA-102.BABA eq. 24 uses 50, about 1.56 kHz).
	RefineMinBin int
	// MatchedAmplitudes, if set, estimates each harmonic's amplitude with the
	// voiced or unvoiced formula according to its final (quantized) voicing,
	// rather than the analysis decision as TIA-102.BABA-1 eq. 8 does.
	MatchedAmplitudes bool
	// Silence enables silence frames (b0 = 124) for frames the voice activity
	// detector classes as non-speech.
	Silence bool
	// SilenceAttenuation (dB) lowers the background level carried by silence
	// frames, i.e. comfort noise below the actual background.
	SilenceAttenuation float64
	// GainOffset (log2 units) is added to every encoded log amplitude.
	GainOffset float64
	// VoicingScale multiplies the V/UV thresholds of TIA-102.BABA eq. 37
	// (> 1 declares more bands voiced).  Zero means 1.
	VoicingScale float64
	// WeightPower selects the quantizer's error weighting (see quant.Target).
	WeightPower float64
}

// DefaultConfig returns the default encoder configuration.
func DefaultConfig() Config {
	return Config{
		Lookahead:         2,
		RefineMinBin:      refineM,
		MatchedAmplitudes: true,
		Silence:           true,
		WeightPower:       0.5,
	}
}

const (
	lpfHalf = 10
	histLen = 1024
)

// Encoder converts 8 kHz 16-bit PCM into AMBE+2 3600x2450 frames.
// It is not safe for concurrent use.
type Encoder struct {
	cfg    Config
	vscale float64

	hist       [histLen]float64 // high-pass filtered input, newest at the end
	hpfX, hpfY float64

	pa  *pitchAnalyzer
	pt  *pitchTracker
	sp  *spectrum
	q   *quant.Predictor
	vad *vad

	errs  [3]pitchErr // E(P) for frames t .. t+Lookahead
	nErrs int

	xiMax float64
	prevV [13]bool // previous frame's V/UV decision per IMBE band (1..12)

	fits [quant.MaxL + 1]harmonicFit
	lpf  [2*halfWin + 1]float64

	// Diagnostics for the most recent frame.
	Last Analysis
}

// Analysis exposes the per-frame analysis results, for tuning and tests.
type Analysis struct {
	PInit   float64 // initial pitch estimate (samples)
	EInit   float64 // E(P_I)
	W0      float64 // refined fundamental (radians/sample)
	Xi0     float64 // frame energy ξ0
	Silence bool
	Params  frame.Params
	Target  quant.Target // what the quantizer was asked to encode
	Model   quant.Model  // what a decoder reconstructs
}

// NewEncoder returns an encoder with DefaultConfig.
func NewEncoder() *Encoder { return NewEncoderConfig(DefaultConfig()) }

// NewEncoderConfig returns an encoder with the given configuration.
func NewEncoderConfig(cfg Config) *Encoder {
	if cfg.Lookahead < 0 {
		cfg.Lookahead = 0
	}
	if cfg.Lookahead > 2 {
		cfg.Lookahead = 2
	}
	if cfg.RefineMinBin <= 0 {
		cfg.RefineMinBin = 1
	}
	vs := cfg.VoicingScale
	if vs == 0 {
		vs = 1
	}
	e := &Encoder{
		cfg:    cfg,
		vscale: vs,
		pa:     newPitchAnalyzer(),
		pt:     newPitchTracker(),
		sp:     newSpectrum(),
		q:      quant.NewPredictor(),
		vad:    newVAD(),
		xiMax:  20000,
	}
	// Pretend Lookahead frames of silence preceded the input so that every
	// call can emit a frame.
	for i := 0; i < cfg.Lookahead; i++ {
		for j := range e.errs[i] {
			e.errs[i][j] = 1
		}
	}
	e.nErrs = cfg.Lookahead
	return e
}

// Delay is the encoder's algorithmic delay in samples: the frame returned by
// Encode is centred this many samples before the end of the input so far.
func (e *Encoder) Delay() int {
	return halfWin + lpfHalf + FrameSamples*e.cfg.Lookahead
}

// Encode consumes the next 20 ms of audio and returns one 49-bit frame.
func (e *Encoder) Encode(pcm *[FrameSamples]int16) frame.Bits {
	// High-pass filter H(z) = (1 − z⁻¹)/(1 − 0.99 z⁻¹) into the history.
	copy(e.hist[:], e.hist[FrameSamples:])
	base := histLen - FrameSamples
	for i, v := range pcm {
		x := float64(v)
		y := x - e.hpfX + 0.99*e.hpfY
		e.hpfX, e.hpfY = x, y
		e.hist[base+i] = y
	}

	// E(P) for the newest frame whose pitch window (plus LPF taps) is complete.
	cE := histLen - 1 - halfWin - lpfHalf
	for n := -halfWin; n <= halfWin; n++ {
		s := 0.0
		for j := -lpfHalf; j <= lpfHalf; j++ {
			s += e.hist[cE+n-j] * imbe.LPF[j+lpfHalf]
		}
		e.lpf[n+halfWin] = s
	}
	e.errs[e.nErrs] = e.pa.analyze(e.lpf[:])
	e.nErrs++

	// Frame to encode now is Lookahead frames older.
	c := cE - FrameSamples*e.cfg.Lookahead
	future := make([]*pitchErr, 0, 2)
	for i := 1; i < e.nErrs; i++ {
		future = append(future, &e.errs[i])
	}
	PI, EI := e.pt.track(&e.errs[0], future)
	copy(e.errs[:], e.errs[1:])
	e.nErrs--

	e.sp.analyze(e.hist[c-wrHalf : c+wrHalf+1])
	silent := e.cfg.Silence && e.vad.silent(meanSquare(e.hist[c-80:c+80]), meanSquare(e.hist[c+80:c+240]))
	b := e.encodeFrame(PI, EI, silent)
	return b.Bits()
}

// encodeFrame turns the analysed frame into quantizer values.
func (e *Encoder) encodeFrame(PI, EI float64, silent bool) frame.Params {
	sp := e.sp
	lf, hf := sp.energies()
	xi0 := lf + hf
	if xi0 > e.xiMax {
		e.xiMax = 0.5*e.xiMax + 0.5*xi0
	} else if v := 0.99*e.xiMax + 0.01*xi0; v > 20000 {
		e.xiMax = v
	} else {
		e.xiMax = 20000
	}
	e.Last = Analysis{PInit: PI, EInit: EI, Xi0: xi0}

	var t quant.Target
	t.WeightPower = e.cfg.WeightPower
	if silent {
		e.Last.Silence = true
		t.B0 = quant.SilenceB0
		w0, L := quant.PitchOf(t.B0)
		sp.fit(w0, L, e.fits[:])
		att := e.cfg.SilenceAttenuation / (20 * math.Log10(2))
		for l := 1; l <= L; l++ {
			t.LogU[l] = log2Amp(sp.unvoicedAmp(e.fits[l])) + 0.5*math.Log2(w0) + unvoicedOffset - att + e.cfg.GainOffset
		}
		for k := range e.prevV {
			e.prevV[k] = false
		}
		p, m := e.q.Quantize(&t)
		e.Last.Params, e.Last.Target, e.Last.Model = p, t, m
		return p
	}

	// Pitch refinement: ten quarter-sample candidates around P_I (eq. 24).
	bestW, bestE := 0.0, math.Inf(1)
	for i := 0; i < 10; i++ {
		P := PI - 9.0/8 + float64(i)/4
		w0 := 2 * math.Pi / P
		if er := sp.refineError(w0, e.cfg.RefineMinBin, e.fits[:]); er < bestE {
			bestW, bestE = w0, er
		}
	}
	e.Last.W0 = bestW

	// Quantize the fundamental (BABA-1 §4.1).  The spectrum is then analysed
	// at the refined fundamental (as TIA-102.BABA does) but for the decoder's
	// number of harmonics L, which BABA-1 requires the encoder to use.
	f0 := bestW / (2 * math.Pi)
	b0, bd := 0, math.Inf(1)
	for i, v := range codebook.W0 {
		if d := math.Abs(v - f0); d < bd {
			b0, bd = i, d
		}
	}
	t.B0 = uint16(b0)
	w0, L := quant.PitchOf(t.B0)
	wa := bestW
	sp.fit(wa, L, e.fits[:])

	// V/UV determination over K bands of three harmonics (eq. 34-42).
	K := 12
	if L <= 36 {
		K = (L + 2) / 3
	}
	M := (0.0025*e.xiMax + xi0) / (0.01*e.xiMax + xi0)
	if lf < 5*hf {
		M *= math.Sqrt(lf / (5 * hf))
	}
	var v [13]bool
	for k := 1; k <= K; k++ {
		lHi := 3 * k
		if k == K {
			lHi = L
		}
		var num, den float64
		for l := 3*k - 2; l <= lHi; l++ {
			num += e.fits[l].err
			den += e.fits[l].energy
		}
		theta := 0.0
		if !(EI > 0.5 && k >= 2) {
			base := 0.45
			if e.prevV[k] {
				base = 0.5625
			}
			theta = base * (1 - 0.3096*float64(k-1)*wa) * M * e.vscale
		}
		v[k] = den > 0 && num/den < theta
	}
	e.prevV = v

	// Spectral amplitudes (eq. 43-44) and the voicing summary per 500 Hz band
	// for the b1 search (BABA-1 eq. 4, weights |M_l|²).
	var mv, mu [quant.MaxL + 1]float64
	for l := 1; l <= L; l++ {
		mv[l] = sp.voicedAmp(e.fits[l])
		mu[l] = sp.unvoicedAmp(e.fits[l])
		kl := 12
		if l <= 36 {
			kl = (l + 2) / 3
		}
		m := mu[l]
		if v[kl] {
			m = mv[l]
		}
		j := quant.BandOf(l, f0Of(t.B0))
		w := m * m
		t.BandWeight[j] += w
		if v[kl] {
			t.Voicing[j] += w
		}
		if !e.cfg.MatchedAmplitudes {
			mv[l], mu[l] = m, m
		}
	}
	for j := range t.Voicing {
		if t.BandWeight[j] > 0 {
			t.Voicing[j] /= t.BandWeight[j]
		}
	}
	for l := 1; l <= L; l++ {
		t.LogV[l] = log2Amp(mv[l]) + e.cfg.GainOffset
		t.LogU[l] = log2Amp(mu[l]) + 0.5*math.Log2(w0) + unvoicedOffset + e.cfg.GainOffset
	}
	p, m := e.q.Quantize(&t)
	e.Last.Params, e.Last.Target, e.Last.Model = p, t, m
	return p
}

// unvoicedOffset is −log2(0.2046): the decoder scales unvoiced amplitudes by
// 0.2046/sqrt(w0) (BABA-1 eq. 8 and 46).
var unvoicedOffset = -math.Log2(0.2046)

func f0Of(b0 uint16) float64 { return codebook.W0[b0] }

func meanSquare(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		s += v * v
	}
	return s / float64(len(x))
}

func log2Amp(a float64) float64 {
	const floor = 1e-3
	if a < floor {
		a = floor
	}
	return math.Log2(a)
}
