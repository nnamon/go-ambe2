package p25full

import (
	"math"

	"github.com/nnamon/mbevoc/internal/mbe"
)

// Config tunes the encoder; DefaultConfig gives the standard behaviour.
type Config struct {
	// Lookahead is the number of future frames (0..2) used for pitch
	// tracking.  2 is the TIA-102.BABA algorithm; fewer frames lower the delay.
	Lookahead int
	// RefineMinBin is the first 256-point DFT bin used in the pitch
	// refinement error (TIA-102.BABA eq. 24 uses 50, about 1.56 kHz).
	RefineMinBin int
	// VoicingScale multiplies the V/UV thresholds of TIA-102.BABA eq. 37
	// (> 1 declares more bands voiced).  Zero means 1.
	VoicingScale float64
}

// DefaultConfig returns the default encoder configuration.
func DefaultConfig() Config {
	return Config{Lookahead: 2, RefineMinBin: mbe.RefineMinBin}
}

// Encoder converts 8 kHz 16-bit PCM into IMBE 7200x4400 frames.  It is not
// safe for concurrent use.
type Encoder struct {
	cfg  Config
	fe   *mbe.Frontend
	vuv  *mbe.Voicing
	pred predictor
	fits [MaxL + 2]mbe.HarmonicFit
	sync uint16

	// Last holds the most recent frame's quantizer values.
	Last Params
}

// NewEncoder returns an encoder with DefaultConfig.
func NewEncoder() *Encoder { return NewEncoderConfig(DefaultConfig()) }

// NewEncoderConfig returns an encoder with the given configuration.
func NewEncoderConfig(cfg Config) *Encoder {
	if cfg.RefineMinBin <= 0 {
		cfg.RefineMinBin = 1
	}
	e := &Encoder{
		cfg:  cfg,
		fe:   mbe.NewFrontend(cfg.Lookahead),
		vuv:  mbe.NewVoicingFrom(100000), // ξ_max per Annex A
		pred: newPredictor(),
	}
	if cfg.VoicingScale != 0 {
		e.vuv.Scale = cfg.VoicingScale
	}
	return e
}

// Delay is the encoder's algorithmic delay in samples: the frame returned by
// Encode is centred this many samples before the end of the input so far.
func (e *Encoder) Delay() int { return e.fe.Delay() }

// Encode consumes the next 20 ms of audio and returns one 88-bit frame; use
// Bits.Encode for the 144-bit frame carried on air.
func (e *Encoder) Encode(pcm *[FrameSamples]int16) Bits {
	PI, EI := e.fe.Push(pcm)
	sp := e.fe.Sp
	lf, hf, xi0 := e.vuv.Track(sp)
	w := sp.RefinePitch(PI, e.cfg.RefineMinBin, e.fits[:])

	// §6.1: b0 = ⌊4π/ŵ0 − 39⌋.  The amplitudes are estimated at the refined
	// fundamental for the decoder's L (which equals L̂, eq. 37 and 47).
	var b Params
	b0 := int(math.Floor(4*math.Pi/w - 39))
	if b0 < 0 {
		b0 = 0
	}
	if b0 > MaxB0 {
		b0 = MaxB0
	}
	b[0] = uint16(b0)
	_, L, K, _ := Pitch(b[0])
	sp.Fit(w, L, e.fits[:])

	// §5.2 and §6.2: one V/UV decision per band of three harmonics.
	v := e.vuv.Decide(e.fits[:], L, w, EI, lf, hf, xi0)
	for k := 1; k <= K; k++ {
		if v[k] {
			b[1] |= 1 << uint(K-k)
		}
	}

	// §5.3: spectral amplitudes per the band decision, then §6.3.
	var target [MaxL + 1]float64
	for l := 1; l <= L; l++ {
		a := sp.UnvoicedAmp(e.fits[l])
		if v[bandOf(l)] {
			a = sp.VoicedAmp(e.fits[l])
		}
		target[l] = math.Log2(math.Max(a, 1e-3))
	}
	e.pred.quantize(&b, &target)

	// §6.5: the synchronization bit alternates, starting at 0.
	b[L+2] = e.sync
	e.sync ^= 1
	e.Last = b
	return b.Bits()
}
