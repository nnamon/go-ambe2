package mbe

import (
	"math"

	"github.com/nnamon/mbevoc/internal/dsp"
)

// ToneWindow is the number of samples a ToneDetector examines: 40 ms
// centred on the frame being encoded (Frontend.Samples).
const ToneWindow = 2 * FrameSamples

const (
	toneFFT    = 1024
	toneBinHz  = 8000.0 / toneFFT
	toneLobe   = 7            // bins either side of a peak: a Hann main lobe (±50 Hz) for ToneWindow samples
	toneLoBin  = 18           // the search covers 140.6 Hz ...
	toneHiBin  = 492          // ... to 3843.8 Hz
	toneGuard  = toneLobe + 3 // bins around the first peak skipped when looking for a second
	toneSpread = 17.0         // Hz: the most rms width a single tone's lobe may have (a pure tone's is 14.4)
	toneTwist  = 10.0         // dB: the most two tones' levels may differ
)

var (
	toneFloor  = math.Pow(32767*math.Pow(10, -50.0/20), 2) / 2 // mean square of a -50 dBFS sine
	toneSingle = math.Pow(10, -22.0/10)                        // energy outside a single tone's lobe
	toneHarm   = math.Pow(10, -30.0/10)                        // a single tone's 2nd and 3rd harmonics, each
	toneDual   = math.Pow(10, -15.0/10)                        // energy outside two tones' lobes
)

// Tone is a steady tone found by ToneDetector: one sinusoid (F2 = 0) or
// two.  Frequencies are in Hz and amplitudes are peak sample values.
type Tone struct {
	F1, A1 float64 // the stronger component
	F2, A2 float64 // the weaker one, for dual tones
}

// ToneDetector recognises frames that are a single steady sinusoid or a
// steady pair of them, as tone frames carry (TIA-102.BABA-1 clause 7.1
// leaves the method open).  Both are judged from the Hann-windowed spectrum
// of ToneWindow samples:
//
//   - a single tone puts all but 22 dB of the energy in one main lobe, whose
//     width is that of a pure sinusoid, and its 2nd and 3rd harmonics are
//     each at least 30 dB down (voiced speech whose first harmonic dominates
//     still has a harmonic series, typically 20-26 dB down);
//   - a dual tone puts all but 15 dB in two lobes whose levels are within
//     10 dB of each other.  Tones closer together than a lobe width (480 and
//     440 Hz) are not separated: they fail the single-tone width test, and
//     may come out as a pair with one frequency misplaced, which the caller's
//     list of valid pairs then rejects.
//
// Frames below -50 dBFS are never tones.  It is not safe for concurrent use.
type ToneDetector struct {
	fft *dsp.FFT
	buf [toneFFT]complex128
	p   [toneFFT/2 + 1]float64
}

// toneHann is the analysis window.
var toneHann = func() (w [ToneWindow]float64) {
	for n := range w {
		w[n] = 0.5 - 0.5*math.Cos(2*math.Pi*(float64(n)+0.5)/ToneWindow)
	}
	return
}()

// NewToneDetector returns a detector.
func NewToneDetector() *ToneDetector { return &ToneDetector{fft: dsp.NewFFT(toneFFT)} }

// Detect examines ToneWindow samples and reports the steady tone they hold.
func (d *ToneDetector) Detect(x []float64) (Tone, bool) {
	var t Tone
	ms := 0.0
	for _, v := range x {
		ms += v * v
	}
	ms /= ToneWindow
	if len(x) != ToneWindow || ms < toneFloor {
		return t, false
	}
	for i := range d.buf {
		d.buf[i] = 0
	}
	for n, v := range x {
		d.buf[n] = complex(v*toneHann[n], 0)
	}
	d.fft.Transform(d.buf[:])
	total := 0.0
	for k := range d.p {
		re, im := real(d.buf[k]), imag(d.buf[k])
		d.p[k] = re*re + im*im
		total += d.p[k]
	}
	if total <= 0 {
		return t, false
	}

	k1 := d.peak(-1)
	e1 := d.lobe(k1)
	t.F1, t.A1 = d.freq(k1), math.Sqrt(2*ms*e1/total)
	if 1-e1/total <= toneSingle && d.harmonics(t.F1, e1) <= toneHarm && d.spread(k1, t.F1, e1) <= toneSpread {
		return t, true
	}

	k2 := d.peak(k1)
	lo1, hi1 := lobeRange(k1)
	e2 := 0.0
	lo2, hi2 := lobeRange(k2)
	for k := lo2; k <= hi2; k++ {
		if k < lo1 || k > hi1 {
			e2 += d.p[k]
		}
	}
	if 1-(e1+e2)/total > toneDual {
		return t, false
	}
	t.F2, t.A2 = d.freq(k2), math.Sqrt(2*ms*d.lobe(k2)/total)
	if t.A2 <= 0 || math.Abs(20*math.Log10(t.A1/t.A2)) > toneTwist {
		return t, false
	}
	return t, true
}

func lobeRange(k int) (lo, hi int) {
	lo, hi = k-toneLobe, k+toneLobe
	if lo < 0 {
		lo = 0
	}
	if hi > toneFFT/2 {
		hi = toneFFT / 2
	}
	return
}

// peak returns the strongest bin in the search range, away from bin skip
// (by toneGuard bins) when skip >= 0.
func (d *ToneDetector) peak(skip int) int {
	best, bk := -1.0, toneLoBin
	for k := toneLoBin; k <= toneHiBin; k++ {
		if skip >= 0 && k >= skip-toneGuard && k <= skip+toneGuard {
			continue
		}
		if d.p[k] > best {
			best, bk = d.p[k], k
		}
	}
	return bk
}

// lobe returns the energy within the main lobe around bin k.
func (d *ToneDetector) lobe(k int) float64 {
	lo, hi := lobeRange(k)
	e := 0.0
	for i := lo; i <= hi; i++ {
		e += d.p[i]
	}
	return e
}

// freq refines the frequency of the peak at bin k by parabolic
// interpolation of the log power.
func (d *ToneDetector) freq(k int) float64 {
	a, b, c := math.Log(d.p[k-1]+1e-30), math.Log(d.p[k]+1e-30), math.Log(d.p[k+1]+1e-30)
	off := 0.0
	if den := a - 2*b + c; den < 0 {
		off = 0.5 * (a - c) / den
	}
	return (float64(k) + off) * toneBinHz
}

// harmonics returns the larger of the 2nd and 3rd harmonic lobes of a tone
// at f relative to its own lobe energy e1.
func (d *ToneDetector) harmonics(f, e1 float64) float64 {
	worst := 0.0
	for h := 2.0; h <= 3; h++ {
		k := int(math.Round(h * f / toneBinHz))
		if k+toneLobe > toneFFT/2 {
			break
		}
		worst = math.Max(worst, d.lobe(k)/e1)
	}
	return worst
}

// spread returns the rms width (Hz) of the lobe around bin k about f.
func (d *ToneDetector) spread(k int, f, e1 float64) float64 {
	lo, hi := lobeRange(k)
	s := 0.0
	for i := lo; i <= hi; i++ {
		df := float64(i)*toneBinHz - f
		s += d.p[i] * df * df
	}
	return math.Sqrt(s / e1)
}
