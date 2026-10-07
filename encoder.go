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
	"github.com/nnamon/go-ambe2/internal/mbe"
	"github.com/nnamon/go-ambe2/quant"
)

// FrameSamples is the number of 8 kHz samples per 20 ms frame.
const FrameSamples = mbe.FrameSamples

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
		RefineMinBin:      mbe.RefineMinBin,
		MatchedAmplitudes: true,
		Silence:           true,
		WeightPower:       0.5,
	}
}

// Encoder converts 8 kHz 16-bit PCM into AMBE+2 3600x2450 frames.
// It is not safe for concurrent use.
type Encoder struct {
	cfg Config

	fe  *mbe.Frontend
	vuv *mbe.Voicing
	q   *quant.Predictor
	vad *mbe.VAD

	fits [mbe.MaxL + 1]mbe.HarmonicFit

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
	e := &Encoder{
		cfg: cfg,
		fe:  mbe.NewFrontend(cfg.Lookahead),
		vuv: mbe.NewVoicing(),
		q:   quant.NewPredictor(),
		vad: mbe.NewVAD(),
	}
	if cfg.VoicingScale != 0 {
		e.vuv.Scale = cfg.VoicingScale
	}
	return e
}

// Delay is the encoder's algorithmic delay in samples: the frame returned by
// Encode is centred this many samples before the end of the input so far.
func (e *Encoder) Delay() int { return e.fe.Delay() }

// Encode consumes the next 20 ms of audio and returns one 49-bit frame.
func (e *Encoder) Encode(pcm *[FrameSamples]int16) frame.Bits {
	PI, EI := e.fe.Push(pcm)
	silent := e.cfg.Silence && e.vad.Silent(e.fe.Energies())
	b := e.encodeFrame(PI, EI, silent)
	return b.Bits()
}

// encodeFrame turns the analysed frame into quantizer values.
func (e *Encoder) encodeFrame(PI, EI float64, silent bool) frame.Params {
	sp := e.fe.Sp
	lf, hf, xi0 := e.vuv.Track(sp)
	e.Last = Analysis{PInit: PI, EInit: EI, Xi0: xi0}

	var t quant.Target
	t.WeightPower = e.cfg.WeightPower
	if silent {
		e.Last.Silence = true
		t.B0 = quant.SilenceB0
		w0, L := quant.PitchOf(t.B0)
		sp.Fit(w0, L, e.fits[:])
		att := e.cfg.SilenceAttenuation / (20 * math.Log10(2))
		for l := 1; l <= L; l++ {
			t.LogU[l] = log2Amp(sp.UnvoicedAmp(e.fits[l])) + 0.5*math.Log2(w0) + unvoicedOffset - att + e.cfg.GainOffset
		}
		e.vuv.Reset()
		p, m := e.q.Quantize(&t)
		e.Last.Params, e.Last.Target, e.Last.Model = p, t, m
		return p
	}

	// Pitch refinement: ten quarter-sample candidates around P_I (eq. 24).
	bestW := sp.RefinePitch(PI, e.cfg.RefineMinBin, e.fits[:])
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
	sp.Fit(wa, L, e.fits[:])

	// V/UV determination over K bands of three harmonics (eq. 34-42).
	v := e.vuv.Decide(e.fits[:], L, wa, EI, lf, hf, xi0)

	// Spectral amplitudes (eq. 43-44) and the voicing summary per 500 Hz band
	// for the b1 search (BABA-1 eq. 4, weights |M_l|²).
	var mv, mu [quant.MaxL + 1]float64
	for l := 1; l <= L; l++ {
		mv[l] = sp.VoicedAmp(e.fits[l])
		mu[l] = sp.UnvoicedAmp(e.fits[l])
		kl := mbe.BandOfHarmonic(l)
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

func log2Amp(a float64) float64 {
	const floor = 1e-3
	if a < floor {
		a = floor
	}
	return math.Log2(a)
}
