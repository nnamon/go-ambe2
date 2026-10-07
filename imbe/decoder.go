package imbe

import (
	"math"

	"github.com/nnamon/go-ambe2/internal/mbe"
)

// DecoderConfig tunes the decoder; the zero value gives the defaults.
type DecoderConfig struct {
	// StandardSynthesis uses the TIA-102.BABA voiced phase model exactly:
	// phase-aligned harmonics, interpolated only below the eighth harmonic.
	// By default phases are dispersed and all harmonics interpolated, as in
	// this module's AMBE+2 decoder.
	StandardSynthesis bool
	// NoEnhancement disables the spectral amplitude enhancement of chapter 8.
	NoEnhancement bool
	// NoSmoothing disables the adaptive smoothing of chapter 9.
	NoSmoothing bool
}

// Decoder converts IMBE 7200x4400 frames back into 8 kHz 16-bit PCM.  It is
// not safe for concurrent use.
type Decoder struct {
	cfg  DecoderConfig
	pred predictor
	syn  *mbe.Synthesizer

	errRate float64 // ε_R (eq. 96)
	energy  float64 // S_E (eq. 111)
	tauM    float64 // τ_M (eq. 115)

	last      mbe.Model // last frame as synthesized, for repeats
	muteState float64
}

// NewDecoder returns a decoder with the default configuration.
func NewDecoder() *Decoder { return NewDecoderConfig(DecoderConfig{}) }

// NewDecoderConfig returns a decoder with the given configuration.
func NewDecoderConfig(cfg DecoderConfig) *Decoder {
	d := &Decoder{cfg: cfg, pred: newPredictor(), syn: mbe.NewSynthesizer(),
		energy: 75000, tauM: 20480, muteState: 1}
	if !cfg.StandardSynthesis {
		d.syn.Dispersed = true
		d.syn.InterpLimit = MaxL + 1
	}
	d.syn.Prev.W0 = 0.02985 // ω̃0(−1), Annex A
	d.last.W0 = d.syn.Prev.W0
	return d
}

// Decode synthesizes 20 ms of speech from an 88-bit frame received without
// channel errors (as from a .imb file).
func (d *Decoder) Decode(b *Bits) [FrameSamples]int16 {
	return d.decode(b, Errors{}, false)
}

// DecodeFrame decodes a 144-bit frame: error correction, error estimation,
// frame repeats and muting (§7.6-7.8), then synthesis.
func (d *Decoder) DecodeFrame(f *Frame) ([FrameSamples]int16, Errors) {
	b, e := f.Decode()
	d.errRate = 0.95*d.errRate + 0.000365*float64(e.Total())
	return d.decode(&b, e, true), e
}

func (d *Decoder) decode(b *Bits, e Errors, coded bool) [FrameSamples]int16 {
	p, ok := b.Params()
	eT := e.Total()
	if !ok || (coded && e.E[0] >= 2 && float64(eT) >= 10+40*d.errRate) {
		return d.repeat() // §7.7
	}
	if d.errRate > 0.0875 {
		return d.mute() // §7.8: parameters are repeated, output muted
	}
	m := d.pred.dequantize(&p)

	var sm mbe.Model
	sm.W0, sm.L = m.w0, m.L
	rm0 := 0.0
	for l := 1; l <= m.L; l++ {
		sm.Voiced[l] = m.voiced[l]
		sm.M[l] = math.Exp2(m.log2M[l])
		rm0 += sm.M[l] * sm.M[l]
	}
	if !d.cfg.NoEnhancement {
		mbe.Enhance(sm.W0, sm.L, &sm.M) // eq. 105-110
	}
	if s := 0.95*d.energy + 0.05*rm0; s >= 10000 { // eq. 111
		d.energy = s
	} else {
		d.energy = 10000
	}
	if !d.cfg.NoSmoothing {
		d.smooth(&sm, e)
	}
	d.last = sm
	var out [FrameSamples]float64
	d.syn.Synthesize(&sm, &out)
	return toPCM(&out)
}

// smooth applies the adaptive smoothing of chapter 9 to the enhanced model.
func (d *Decoder) smooth(sm *mbe.Model, e Errors) {
	eR, eT := d.errRate, float64(e.Total())
	vm := math.Inf(1) // eq. 112
	switch {
	case eR <= 0.005 && eT <= 4:
	case eR <= 0.0125 && e.E[4] == 0:
		vm = 45.255 * math.Pow(d.energy, 0.375) / math.Exp(277.26*eR)
	default:
		vm = 1.414 * math.Pow(d.energy, 0.375)
	}
	am := 0.0
	for l := 1; l <= sm.L; l++ {
		if sm.M[l] > vm { // eq. 113
			sm.Voiced[l] = true
		}
		am += sm.M[l] // eq. 114
	}
	if eR <= 0.005 && eT <= 6 { // eq. 115
		d.tauM = 20480
	} else {
		d.tauM = 6000 - 300*eT + d.tauM
	}
	if d.tauM <= am && am > 0 { // eq. 116
		g := d.tauM / am
		for l := 1; l <= sm.L; l++ {
			sm.M[l] *= g
		}
	}
}

// repeat re-synthesizes the previous frame's parameters (§7.7).
func (d *Decoder) repeat() [FrameSamples]int16 {
	if d.errRate > 0.0875 {
		return d.mute()
	}
	var out [FrameSamples]float64
	sm := d.last
	d.syn.Synthesize(&sm, &out)
	return toPCM(&out)
}

// mute outputs comfort noise uniformly distributed over [-5, 5] (§7.8) while
// keeping the synthesizer's state advancing.
func (d *Decoder) mute() [FrameSamples]int16 {
	var out [FrameSamples]int16
	for i := range out {
		d.muteState = mbe.NextNoise(d.muteState)
		out[i] = int16(math.Round(d.muteState/mbe.NoiseHi*10 - 5))
	}
	silent := mbe.Model{W0: d.syn.Prev.W0}
	var sink [FrameSamples]float64
	d.syn.Synthesize(&silent, &sink)
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
