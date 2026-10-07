package halfrate

import (
	"math"

	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/internal/mbe"
	"github.com/nnamon/mbevoc/quant"
)

// DecoderConfig tunes the decoder (see p25half.DecoderConfig).
type DecoderConfig struct {
	StandardSynthesis bool
	SilenceGain       float64 // used as given
	NoEnhancement     bool
}

// Decoder reconstructs and synthesizes frames of its codebook, with the
// frame repeat and muting rules of TIA-102.BABA-1 clauses 5.5-5.7.  It is not
// safe for concurrent use.
type Decoder struct {
	q   *quant.Predictor
	syn *mbe.Synthesizer

	last    mbe.Model // last valid frame's enhanced model, for repeats
	haveVal bool
	repeats int
	errRate float64 // ε_R

	muteState   float64
	noEnhance   bool
	silenceGain float64
}

// NewDecoder returns a decoder for codebook cb.
func NewDecoder(cfg DecoderConfig, cb *quant.Codebook) *Decoder {
	d := &Decoder{q: quant.NewPredictorFor(cb), syn: mbe.NewSynthesizer(), muteState: 1,
		silenceGain: cfg.SilenceGain, noEnhance: cfg.NoEnhancement}
	if !cfg.StandardSynthesis {
		d.syn.Dispersed = true
		d.syn.InterpLimit = mbe.MaxL + 1
	}
	return d
}

// Errors updates the error rate ε_R with the errors corrected in a coded
// frame's c0 and c1 (c0Parity reports a parity failure after correcting c0,
// i.e. four or more errors) and reports whether the frame must be discarded
// (BABA-1 clause 5.5).
func (d *Decoder) Errors(e0, e1 int, c0Parity bool) (bad bool) {
	if c0Parity {
		e0 = 4
	}
	eT := e0 + e1
	d.errRate = 0.95*d.errRate + 0.001064*float64(eT)
	return e0 >= 4 || (e0 >= 2 && eT >= 6)
}

// Frame synthesizes a voice, silence or erasure frame with quantizer values
// p (kind as classified by the caller); bad frames and erasures repeat the
// previous frame.
func (d *Decoder) Frame(p frame.Params, kind quant.Kind, bad bool) [mbe.FrameSamples]int16 {
	if bad || kind == quant.Erasure || kind == quant.Tone {
		return d.Repeat()
	}
	m, _ := d.q.Dequantize(p)
	var sm mbe.Model
	sm.W0, sm.L = m.W0, m.L
	for l := 1; l <= m.L; l++ {
		sm.Voiced[l] = m.Voiced[l]
		sm.M[l] = m.Amplitude(l)
	}
	if !d.noEnhance {
		mbe.Enhance(sm.W0, sm.L, &sm.M)
	}
	if kind == quant.Silence {
		for l := 1; l <= sm.L; l++ {
			sm.M[l] *= d.silenceGain
		}
	}

	d.last, d.haveVal, d.repeats = sm, true, 0
	if d.errRate > 0.096 {
		return d.Mute()
	}
	var out [mbe.FrameSamples]float64
	d.syn.Synthesize(&sm, &out)
	return toPCM(&out)
}

// Tone synthesizes a tone frame's model (with Pure set, no enhancement).
func (d *Decoder) Tone(tm mbe.Model) [mbe.FrameSamples]int16 {
	d.last, d.haveVal, d.repeats = tm, true, 0
	var out [mbe.FrameSamples]float64
	d.syn.Synthesize(&tm, &out)
	return toPCM(&out)
}

// Repeat re-synthesizes the previous valid parameters (BABA-1 clause 5.6),
// muting instead on the fourth consecutive repeat (clause 5.7).
func (d *Decoder) Repeat() [mbe.FrameSamples]int16 {
	d.repeats++
	if d.repeats >= 4 || d.errRate > 0.096 || !d.haveVal {
		return d.Mute()
	}
	var out [mbe.FrameSamples]float64
	sm := d.last
	d.syn.Synthesize(&sm, &out)
	return toPCM(&out)
}

// Mute outputs comfort noise uniformly distributed over [-5, 5] while keeping
// the synthesizer's state advancing.
func (d *Decoder) Mute() [mbe.FrameSamples]int16 {
	var out [mbe.FrameSamples]int16
	for i := range out {
		d.muteState = mbe.NextNoise(d.muteState)
		out[i] = int16(math.Round(d.muteState/mbe.NoiseHi*10 - 5))
	}
	var silent mbe.Model
	silent.W0 = d.syn.Prev.W0
	var sink [mbe.FrameSamples]float64
	d.syn.Synthesize(&silent, &sink)
	return out
}

func toPCM(x *[mbe.FrameSamples]float64) (out [mbe.FrameSamples]int16) {
	for i, v := range x {
		v = math.Round(v)
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return out
}
