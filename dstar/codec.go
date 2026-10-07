package dstar

import (
	"math"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/halfrate"
	"github.com/nnamon/go-ambe2/internal/mbe"
	"github.com/nnamon/go-ambe2/quant"
)

// Config tunes the encoder; see ambe.Config for the fields' meaning.  The
// encoder always sends voice frames: the silence settings are ignored, since
// how D-STAR radios encode silence is not known (the decoder plays b0 = 124
// and 125 as silence frames, as the null AMBE frame of D-STAR gateways uses
// 124).
type Config = halfrate.Config

// DefaultConfig returns the default encoder configuration.
func DefaultConfig() Config { return halfrate.DefaultConfig() }

// Encoder converts 8 kHz 16-bit PCM into D-STAR AMBE frames.  It is not safe
// for concurrent use.
type Encoder struct{ e *halfrate.Encoder }

// NewEncoder returns an encoder with DefaultConfig.
func NewEncoder() *Encoder { return NewEncoderConfig(DefaultConfig()) }

// NewEncoderConfig returns an encoder with the given configuration.
func NewEncoderConfig(cfg Config) *Encoder {
	cfg.Silence = false
	return &Encoder{e: halfrate.NewEncoder(cfg, quant.DStar)}
}

// Delay is the encoder's algorithmic delay in samples.
func (e *Encoder) Delay() int { return e.e.Delay() }

// Encode consumes the next 20 ms of audio and returns one 49-bit frame; use
// Bits.Encode for the 72-bit frame.
func (e *Encoder) Encode(pcm *[FrameSamples]int16) Bits {
	return Params(e.e.Encode(pcm)).Bits()
}

// DecoderConfig tunes the decoder (see ambe.DecoderConfig; there are no
// silence frames).
type DecoderConfig struct {
	StandardSynthesis bool
	NoEnhancement     bool
}

// Decoder converts D-STAR AMBE frames back into 8 kHz 16-bit PCM, with frame
// repeats and muting on uncorrectable frames.  It is not safe for concurrent
// use.
type Decoder struct{ e *halfrate.Decoder }

// NewDecoder returns a decoder with the default configuration.
func NewDecoder() *Decoder { return NewDecoderConfig(DecoderConfig{}) }

// NewDecoderConfig returns a decoder with the given configuration.
func NewDecoderConfig(cfg DecoderConfig) *Decoder {
	return &Decoder{e: halfrate.NewDecoder(halfrate.DecoderConfig{
		StandardSynthesis: cfg.StandardSynthesis, SilenceGain: 1, NoEnhancement: cfg.NoEnhancement,
	}, quant.DStar)}
}

// Decode synthesizes 20 ms of speech from a 49-bit frame received without
// channel errors.
func (d *Decoder) Decode(b *Bits) [FrameSamples]int16 { return d.decode(b, false) }

// DecodeFrame error-corrects a 72-bit frame and synthesizes 20 ms of speech,
// repeating or muting on badly corrupted frames.
func (d *Decoder) DecodeFrame(f *Frame) ([FrameSamples]int16, Errors) {
	b, e := f.Decode()
	e1 := e.C1
	if e.C1Parity {
		e1 = 4 // c1 most likely has four or more errors
	}
	bad := d.e.Errors(e.C0, e1, e.C0Parity)
	return d.decode(&b, bad), e
}

func (d *Decoder) decode(b *Bits, bad bool) [FrameSamples]int16 {
	p := b.Params()
	kind := quant.DStar.Kind(p[0])
	if kind == quant.Tone && !bad {
		if m, ok := toneModel(b); ok {
			return d.e.Tone(m)
		}
		// Other tone indices, including 128, which mbelib-neo's encoder sends
		// as D-STAR's silence frame, are played as silence.
		return d.e.Mute()
	}
	return d.e.Frame(frame.Params(p), kind, bad)
}

// ToneIndex returns the tone index of a tone frame, read as mbelib reads it
// (its bit layout was partly inferred from captured transmissions).
func ToneIndex(b *Bits) int {
	t7 := [8]uint8{1, 0, 0, 0, 0, 1, 1, 1}
	t6 := [8]uint8{0, 0, 0, 1, 1, 1, 1, 0}
	t5 := [8]uint8{0, 0, 1, 0, 1, 1, 0, 1}
	def := b[6]<<2 | b[7]<<1 | b[8]
	id := int(t7[def])<<7 | int(t6[def])<<6 | int(t5[def])<<5
	id |= int(b[9])<<4 | int(b[42])<<3 | int(b[43])<<2 | int(b[10])<<1 | int(b[11])
	return id
}

// toneModel synthesizes single tones (index 5..122 at 31.25·index Hz) as
// mbelib-neo does, at its nominal level (amplitude code 103 on the
// TIA-102.BABA-1 Annex J scale).  Dual tones are not known for this rate.
func toneModel(b *Bits) (mbe.Model, bool) {
	var m mbe.Model
	id := ToneIndex(b)
	if id < 5 || id > 122 {
		return m, false
	}
	f := 31.25 * float64(id)
	l := 1
	for f/float64(l) > 400 {
		l++
	}
	m.W0 = 2 * math.Pi * f / float64(l) / 8000
	m.L = int(3812.5 / (f / float64(l)))
	if m.L > mbe.MaxL {
		m.L = mbe.MaxL
	}
	m.Voiced[l] = true
	m.M[l] = 16384 * math.Pow(10, 0.03555*float64(103-127))
	m.Pure = true
	return m, true
}
