package ambe

import (
	"math"

	"github.com/nnamon/go-ambe2/fec"
	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/quant"
)

// Decoder converts AMBE+2 3600x2450 frames back into 8 kHz 16-bit PCM.
//
// Model parameters are reconstructed per TIA-102.BABA-1 clause 4,
// enhanced per TIA-102.BABA chapter 8 and synthesized per chapter 11.
// Erasure frames, frames with uncorrectable errors (Decode72) and invalid
// tone frames repeat the previous parameters, and the output is muted after
// four consecutive repeats or at a high error rate (BABA-1 clauses 5.5-5.7).
// Tone frames are regenerated from their Annex J MBE representation
// (BABA-1 clauses 7.3 and 8).
// It is not safe for concurrent use.
type Decoder struct {
	q   *quant.Predictor
	syn *synthesizer

	last    synthModel // last valid frame's enhanced model, for repeats
	haveVal bool
	repeats int
	errRate float64 // ε_R

	muteState   float64
	noEnhance   bool
	silenceGain float64
}

// SilenceGain is the default amplitude factor applied to silence frames
// (comfort noise).  TIA-102.BABA-1 synthesizes silence frames like any other
// frame (factor 1); the MD-380 decoder plays them about 12.8 dB lower, which
// this default reproduces.
const SilenceGain = 0.228

// DecoderConfig tunes the decoder; the zero value gives the defaults.
type DecoderConfig struct {
	// StandardSynthesis uses the TIA-102.BABA voiced phase model exactly:
	// phase-aligned harmonics, interpolated only below the eighth harmonic.
	// By default phases are dispersed and all harmonics interpolated, which
	// scores slightly higher and is closer to the MD-380 decoder.
	StandardSynthesis bool
	// SilenceGain scales silence frames (comfort noise).  Zero means the
	// default, SilenceGain (MD-380 behaviour); 1 synthesizes them as the
	// standard does.
	SilenceGain float64
	// NoEnhancement disables the spectral amplitude enhancement of
	// TIA-102.BABA chapter 8.
	NoEnhancement bool
}

// NewDecoder returns a decoder with the default configuration.
func NewDecoder() *Decoder { return NewDecoderConfig(DecoderConfig{}) }

// NewDecoderConfig returns a decoder with the given configuration.
func NewDecoderConfig(cfg DecoderConfig) *Decoder {
	d := &Decoder{q: quant.NewPredictor(), syn: newSynthesizer(), muteState: 1,
		silenceGain: cfg.SilenceGain, noEnhance: cfg.NoEnhancement}
	if d.silenceGain == 0 {
		d.silenceGain = SilenceGain
	}
	if !cfg.StandardSynthesis {
		d.syn.dispersed = true
		d.syn.interpLimit = quant.MaxL + 1
	}
	return d
}

// Decode synthesizes 20 ms of speech from a 49-bit frame received without
// channel errors (as from a .amb file or a 49-bit network payload).
func (d *Decoder) Decode(b *frame.Bits) [FrameSamples]int16 {
	return d.decode(b, false)
}

// Decode72 de-interleaves and error-corrects a 72-bit on-air frame and
// synthesizes 20 ms of speech, repeating or muting on badly corrupted frames.
func (d *Decoder) Decode72(c *fec.Bits72) ([FrameSamples]int16, fec.Errors) {
	b, e := fec.Decode(c)
	e0 := e.C0
	if e.C0Parity {
		e0 = 4 // parity failure after correction: at least four errors in c0
	}
	eT := e0 + e.C1
	d.errRate = 0.95*d.errRate + 0.001064*float64(eT)
	bad := e0 >= 4 || (e0 >= 2 && eT >= 6)
	return d.decode(&b, bad), e
}

func (d *Decoder) decode(b *frame.Bits, bad bool) [FrameSamples]int16 {
	p := b.Params()
	var out [FrameSamples]float64
	kind := quant.KindOf(p[0])
	if IsTone(b) {
		kind = quant.Tone
	} else if kind == quant.Tone {
		kind = quant.Erasure // b0 126/127 without the tone pattern
	}
	if !bad && kind == quant.Tone {
		tm, ok := toneModel(b)
		if !ok {
			return d.repeat() // invalid tone index: treated as an erasure
		}
		// The MD-380 decoder applies no spectral enhancement to tones (both
		// DTMF components come out at equal level) and synthesizes them with
		// continuous phase.
		for l := 1; l <= tm.L; l++ {
			tm.M[l] *= toneLevel
		}
		tm.pure = true
		d.last, d.haveVal, d.repeats = tm, true, 0
		d.syn.synthesize(&tm, &out)
		return toPCM(&out)
	}
	if bad || kind == quant.Erasure {
		return d.repeat()
	}

	m, _ := d.q.Dequantize(p)
	var sm synthModel
	sm.w0, sm.L = m.W0, m.L
	for l := 1; l <= m.L; l++ {
		sm.voiced[l] = m.Voiced[l]
		sm.M[l] = m.Amplitude(l)
	}
	if !d.noEnhance {
		enhance(sm.w0, sm.L, &sm.M)
	}
	if kind == quant.Silence {
		for l := 1; l <= sm.L; l++ {
			sm.M[l] *= d.silenceGain
		}
	}

	d.last, d.haveVal, d.repeats = sm, true, 0
	if d.errRate > 0.096 {
		return d.mute()
	}
	d.syn.synthesize(&sm, &out)
	return toPCM(&out)
}

// repeat re-synthesizes the previous valid parameters (BABA-1 clause 5.6),
// muting instead on the fourth consecutive repeat (clause 5.7).
func (d *Decoder) repeat() [FrameSamples]int16 {
	d.repeats++
	if d.repeats >= 4 || d.errRate > 0.096 || !d.haveVal {
		return d.mute()
	}
	var out [FrameSamples]float64
	sm := d.last
	d.syn.synthesize(&sm, &out)
	return toPCM(&out)
}

// mute outputs comfort noise uniformly distributed over [-5, 5] while keeping
// the synthesizer's state advancing.
func (d *Decoder) mute() [FrameSamples]int16 {
	var out [FrameSamples]int16
	for i := range out {
		d.muteState = nextNoise(d.muteState)
		out[i] = int16(math.Round(d.muteState/noiseHi*10 - 5))
	}
	var silent synthModel
	silent.w0 = d.syn.prev.w0
	var sink [FrameSamples]float64
	d.syn.synthesize(&silent, &sink)
	return out
}

func toPCM(x *[FrameSamples]float64) (out [FrameSamples]int16) {
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
