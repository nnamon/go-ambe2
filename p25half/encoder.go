// Package p25half is a from-scratch AMBE+2 3600x2450 (DMR / NXDN / P25 Phase 2
// half-rate) speech encoder.
//
// Analysis follows the MBE analysis of TIA-102.BABA chapter 5 (pitch
// estimation with look-back/look-ahead tracking, pitch refinement, V/UV
// determination, spectral amplitude estimation) and quantization follows the
// half-rate vocoder description of TIA-102.BABA-1 chapter 4.  The output is
// the 49-bit voice parameter frame (see package frame); FEC and interleaving
// for a particular air interface are separate.
package p25half

import (
	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/internal/halfrate"
	"github.com/nnamon/mbevoc/internal/mbe"
	"github.com/nnamon/mbevoc/quant"
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
	// Tones enables tone frames (TIA-102.BABA-1 clause 7): a frame that is a
	// steady single tone of 141-3828 Hz, or one of the dual tones of Table 9
	// (DTMF, KNOX, call progress), is sent as a tone frame instead of a voice
	// frame, as the MD-380's encoder does.  See mbe.ToneDetector for the
	// criteria.
	Tones bool
}

// DefaultConfig returns the default encoder configuration.
func DefaultConfig() Config {
	h := halfrate.DefaultConfig()
	return Config{
		Lookahead: h.Lookahead, RefineMinBin: h.RefineMinBin, MatchedAmplitudes: h.MatchedAmplitudes,
		Silence: h.Silence, SilenceAttenuation: h.SilenceAttenuation, GainOffset: h.GainOffset,
		VoicingScale: h.VoicingScale, WeightPower: h.WeightPower,
	}
}

// engine returns the configuration of the shared encoder engine.
func (c Config) engine() halfrate.Config {
	return halfrate.Config{
		Lookahead: c.Lookahead, RefineMinBin: c.RefineMinBin, MatchedAmplitudes: c.MatchedAmplitudes,
		Silence: c.Silence, SilenceAttenuation: c.SilenceAttenuation, GainOffset: c.GainOffset,
		VoicingScale: c.VoicingScale, WeightPower: c.WeightPower,
	}
}

// Encoder converts 8 kHz 16-bit PCM into AMBE+2 3600x2450 frames.
// It is not safe for concurrent use.
type Encoder struct {
	e     *halfrate.Encoder
	tones *mbe.ToneDetector // nil unless Config.Tones

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

	// For a tone frame (Config.Tones), the Table 9 index and amplitude code;
	// the fields above are then zero.
	Tone            bool
	ToneID, ToneAmp int
}

// NewEncoder returns an encoder with DefaultConfig.
func NewEncoder() *Encoder { return NewEncoderConfig(DefaultConfig()) }

// NewEncoderConfig returns an encoder with the given configuration.
func NewEncoderConfig(cfg Config) *Encoder {
	e := &Encoder{e: halfrate.NewEncoder(cfg.engine(), quant.AMBE2)}
	if cfg.Tones {
		e.tones = mbe.NewToneDetector()
	}
	return e
}

// Delay is the encoder's algorithmic delay in samples: the frame returned by
// Encode is centred this many samples before the end of the input so far.
func (e *Encoder) Delay() int { return e.e.Delay() }

// Encode consumes the next 20 ms of audio and returns one 49-bit frame.
func (e *Encoder) Encode(pcm *[FrameSamples]int16) frame.Bits {
	e.e.Analyze(pcm)
	if e.tones != nil {
		if t, ok := e.tones.Detect(e.e.Samples(mbe.ToneWindow)); ok {
			if id, amp, ok := toneIndex(t); ok {
				// The quantizer is left alone, as a decoder's is over a tone frame.
				e.Last = Analysis{Tone: true, ToneID: id, ToneAmp: amp}
				return ToneFrame(id, amp)
			}
		}
	}
	p := e.e.Quantize()
	a := e.e.Last
	e.Last = Analysis{PInit: a.PInit, EInit: a.EInit, W0: a.W0, Xi0: a.Xi0, Silence: a.Silence,
		Params: a.Params, Target: a.Target, Model: a.Model}
	return p.Bits()
}
