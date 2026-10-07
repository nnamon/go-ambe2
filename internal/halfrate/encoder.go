// Package halfrate is the encoder and decoder engine of the half-rate MBE
// vocoder family: AMBE+2 3600x2450 (package ambe) and D-STAR's AMBE
// 3600x2400 (package dstar).  They share the analysis, quantizer structure
// and synthesis and differ in their codebooks (quant.Codebook) and frame
// formats, which the public packages handle.
package halfrate

import (
	"math"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/mbe"
	"github.com/nnamon/go-ambe2/quant"
)

// Config tunes the encoder (see ambe.Config for the meaning of each field).
type Config struct {
	Lookahead          int
	RefineMinBin       int
	MatchedAmplitudes  bool
	Silence            bool
	SilenceAttenuation float64
	GainOffset         float64
	VoicingScale       float64
	WeightPower        float64
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

// Encoder converts 8 kHz 16-bit PCM into quantizer values b0..b8 for its
// codebook.  It is not safe for concurrent use.
type Encoder struct {
	cfg Config
	cb  *quant.Codebook

	fe  *mbe.Frontend
	vuv *mbe.Voicing
	q   *quant.Predictor
	vad *mbe.VAD

	fits [mbe.MaxL + 1]mbe.HarmonicFit

	// Last holds the analysis of the most recent frame.
	Last Analysis
}

// NewEncoder returns an encoder for codebook cb.  Silence frames are only
// sent if the codebook has them.
func NewEncoder(cfg Config, cb *quant.Codebook) *Encoder {
	if cfg.Lookahead < 0 {
		cfg.Lookahead = 0
	}
	if cfg.Lookahead > 2 {
		cfg.Lookahead = 2
	}
	if cfg.RefineMinBin <= 0 {
		cfg.RefineMinBin = 1
	}
	if _, ok := cb.SilenceCode(); !ok {
		cfg.Silence = false
	}
	e := &Encoder{
		cfg: cfg,
		cb:  cb,
		fe:  mbe.NewFrontend(cfg.Lookahead),
		vuv: mbe.NewVoicing(),
		q:   quant.NewPredictorFor(cb),
		vad: mbe.NewVAD(),
	}
	if cfg.VoicingScale != 0 {
		e.vuv.Scale = cfg.VoicingScale
	}
	return e
}

// Delay is the encoder's algorithmic delay in samples.
func (e *Encoder) Delay() int { return e.fe.Delay() }

// Encode consumes the next 20 ms of audio and returns the next frame's
// quantizer values.
func (e *Encoder) Encode(pcm *[mbe.FrameSamples]int16) frame.Params {
	PI, EI := e.fe.Push(pcm)
	silent := e.cfg.Silence && e.vad.Silent(e.fe.Energies())
	return e.encodeFrame(PI, EI, silent)
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
		t.B0, _ = e.cb.SilenceCode()
		w0, L := e.cb.Pitch(t.B0)
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
	for i := 0; i < e.cb.VoiceCodes(); i++ {
		if d := math.Abs(e.cb.F0(uint16(i)) - f0); d < bd {
			b0, bd = i, d
		}
	}
	t.B0 = uint16(b0)
	w0, L := e.cb.Pitch(t.B0)
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
		j := quant.BandOf(l, e.cb.F0(t.B0))
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

func log2Amp(a float64) float64 {
	const floor = 1e-3
	if a < floor {
		a = floor
	}
	return math.Log2(a)
}
