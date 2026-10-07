package ambe

import (
	"github.com/nnamon/go-ambe2/fec"
	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/halfrate"
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
	e *halfrate.Decoder
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
	sg := cfg.SilenceGain
	if sg == 0 {
		sg = SilenceGain
	}
	return &Decoder{e: halfrate.NewDecoder(halfrate.DecoderConfig{
		StandardSynthesis: cfg.StandardSynthesis, SilenceGain: sg, NoEnhancement: cfg.NoEnhancement,
	}, quant.AMBE2)}
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
	bad := d.e.Errors(e.C0, e.C1, e.C0Parity)
	return d.decode(&b, bad), e
}

func (d *Decoder) decode(b *frame.Bits, bad bool) [FrameSamples]int16 {
	p := b.Params()
	kind := quant.KindOf(p[0])
	if IsTone(b) {
		kind = quant.Tone
	} else if kind == quant.Tone {
		kind = quant.Erasure // b0 126/127 without the tone pattern
	}
	if !bad && kind == quant.Tone {
		tm, ok := toneModel(b)
		if !ok {
			return d.e.Repeat() // invalid tone index: treated as an erasure
		}
		// The MD-380 decoder applies no spectral enhancement to tones (both
		// DTMF components come out at equal level) and synthesizes them with
		// continuous phase.
		for l := 1; l <= tm.L; l++ {
			tm.M[l] *= toneLevel
		}
		tm.Pure = true
		return d.e.Tone(tm)
	}
	return d.e.Frame(p, kind, bad)
}

// Decode72Soft decodes a frame of soft decisions (fec.DecodeSoft) with the
// error handling of Decode72.
func (d *Decoder) Decode72Soft(s *fec.Soft72) ([FrameSamples]int16, fec.Errors) {
	b, e := fec.DecodeSoft(s)
	bad := d.e.Errors(e.C0, e.C1, false)
	return d.decode(&b, bad), e
}
