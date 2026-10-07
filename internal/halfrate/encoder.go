// Package halfrate is the encoder and decoder engine of the half-rate MBE
// vocoder family: AMBE+2 3600x2450 (package p25half) and D-STAR's AMBE
// 3600x2400 (package dstar).  They share the analysis, quantizer structure
// and synthesis and differ in their codebooks (quant.Codebook) and frame
// formats, which the public packages handle.
package halfrate

import (
	"math"

	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/internal/denoise"
	"github.com/nnamon/mbevoc/internal/mbe"
	"github.com/nnamon/mbevoc/quant"
)

// Config tunes the encoder (see p25half.Config for the meaning of each field).
type Config struct {
	Lookahead          int
	RefineMinBin       int
	MatchedAmplitudes  bool
	Silence            bool
	SilenceAttenuation float64
	GainOffset         float64
	VoicingScale       float64
	WeightPower        float64
	Denoise            bool
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

	// The frame analysed by the latest Analyze.
	pi, ei float64
	silent bool

	// With Denoise: the suppressor, and the high-pass filtered input before
	// suppression (for Samples), newest at the end.
	ns         *denoise.Suppressor
	raw        [rawLen]float64
	rawX, rawY float64

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
	if cfg.Denoise {
		e.ns = denoise.New(denoise.DefaultConfig())
	}
	return e
}

const rawLen = 1024

// Reset returns the encoder to its initial state, for a new transmission,
// except that the noise suppressor keeps its estimate of the background
// noise, which outlasts a pause in the same channel.
func (e *Encoder) Reset() {
	ns := e.ns
	*e = *NewEncoder(e.cfg, e.cb)
	if ns != nil {
		ns.Flush()
		e.ns = ns
	}
}

// Delay is the encoder's algorithmic delay in samples.
func (e *Encoder) Delay() int {
	if e.ns != nil {
		return e.fe.Delay() + denoise.Delay
	}
	return e.fe.Delay()
}

// Encode consumes the next 20 ms of audio and returns the next frame's
// quantizer values.
func (e *Encoder) Encode(pcm *[mbe.FrameSamples]int16) frame.Params {
	e.Analyze(pcm)
	return e.Quantize()
}

// Analyze consumes the next 20 ms of audio and analyses the frame to be
// encoded next, without quantizing it.  Quantize then encodes that frame.
// A caller that sends something else in its place (a tone frame) skips
// Quantize, which leaves the prediction state as a decoder's stays over
// such a frame.
func (e *Encoder) Analyze(pcm *[mbe.FrameSamples]int16) {
	if e.ns != nil {
		pcm = e.suppress(pcm)
	}
	e.pi, e.ei = e.fe.Push(pcm)
	e.silent = e.cfg.Silence && e.vad.Silent(e.fe.Energies())
}

// suppress keeps the input's high-pass filtered history (the front end's
// filter) and returns the input with the background noise suppressed.
func (e *Encoder) suppress(pcm *[mbe.FrameSamples]int16) *[mbe.FrameSamples]int16 {
	copy(e.raw[:], e.raw[mbe.FrameSamples:])
	for i, v := range pcm {
		x := float64(v)
		y := x - e.rawX + 0.99*e.rawY
		e.rawX, e.rawY = x, y
		e.raw[rawLen-mbe.FrameSamples+i] = y
	}
	out := *pcm
	e.ns.ProcessPCM(out[:])
	return &out
}

// Quantize encodes the frame analysed by the latest Analyze.
func (e *Encoder) Quantize() frame.Params { return e.encodeFrame(e.pi, e.ei, e.silent) }

// Samples returns n high-pass filtered input samples centred on the frame
// analysed by the latest Analyze (n even, at most 2·mbe.FrameSamples).  With
// Denoise they are the input before suppression.
func (e *Encoder) Samples(n int) []float64 {
	if e.ns == nil {
		return e.fe.Samples(n)
	}
	c := rawLen - 1 - e.Delay()
	return e.raw[c-n/2 : c+n/2]
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
