// Package denoise is a single-channel noise suppressor for 8 kHz speech,
// applied before encoding.  It works on 30 ms Hann-windowed frames every
// 10 ms (short-time Fourier transform with overlap-add) and combines two
// published methods:
//
//   - the noise spectrum is tracked by minima controlled recursive averaging
//     (I. Cohen and B. Berdugo, "Noise estimation by minima controlled
//     recursive averaging for robust speech enhancement", IEEE Signal
//     Processing Letters 9(1), 2002): spectral minima over a sliding window
//     decide where speech is present, and the noise estimate is averaged only
//     where it is absent;
//   - each frequency bin is scaled by the log-spectral amplitude estimator
//     (Y. Ephraim and D. Malah, IEEE Trans. ASSP 33(2), 1985), with the
//     "decision-directed" a priori SNR of their 1984 paper, and never below a
//     floor, which keeps residual noise natural rather than "musical".
package denoise

import (
	"math"

	"github.com/nnamon/mbevoc/internal/dsp"
)

const (
	window = 240 // 30 ms analysis window
	hop    = 80  // 10 ms
	nfft   = 256
	nbins  = nfft/2 + 1

	// Delay is the suppressor's delay in samples.
	Delay = window - hop
)

// Config tunes the suppressor; DefaultConfig gives the defaults.
type Config struct {
	// Floor is the least gain (dB, negative) applied to any bin: how far
	// noise is pushed down.
	Floor float64
	// Bias scales the noise estimate (> 1 suppresses more).
	Bias float64
	// Delta is the ratio of smoothed power to its minimum above which a bin
	// is taken to hold speech.
	Delta float64
	// AlphaD is the noise estimate's averaging factor where speech is absent.
	AlphaD float64
	// MinWindow is the length (frames of 10 ms) of the minimum-tracking
	// sub-windows.
	MinWindow int
	// AlphaDD is the decision-directed a priori SNR smoothing.
	AlphaDD float64
	// Full and None set the suppression depth from the long-term ratio of
	// speech to noise (dB): the full Floor at Full or below, no suppression
	// at None or above, and in between a floor proportionally nearer 0 dB.
	// Noise that far below the speech is not worth the distortion of
	// removing it.
	Full, None float64
}

// DefaultConfig returns the default configuration, tuned on the research
// corpus's dev set (research/tools/eval_noise.py) for the quality of the
// coded speech.  The noise estimate is scaled to 0.35: suppressing less
// keeps speech intact, which counts for more once it has been through the
// vocoder; steady noise between words is lowered by about 5 dB.
func DefaultConfig() Config {
	return Config{Floor: -15, Bias: 0.35, Delta: 5, AlphaD: 0.95, MinWindow: 100, AlphaDD: 0.98, Full: 20, None: 35}
}

// Fixed parameters of the noise tracker and gain rule.
const (
	alphaS     = 0.8 // smoothing of the power spectrum for minimum tracking
	alphaP     = 0.2 // smoothing of the speech presence indicator
	xiMin      = 0.003162278
	warmFrames = 10 // frames over which the first noise estimate is formed
)

// sqrtHann is the analysis and synthesis window: a periodic square-root
// Hann window, whose square overlaps to a constant 1.5 at a hop of 80.
var sqrtHann = func() (w [window]float64) {
	for n := range w {
		w[n] = math.Sqrt(0.5 - 0.5*math.Cos(2*math.Pi*float64(n)/window))
	}
	return
}()

// Suppressor removes stationary background noise from a stream.  It is not
// safe for concurrent use.
type Suppressor struct {
	cfg   Config
	floor float64 // linear gain floor
	fft   *dsp.FFT
	buf   [nfft]complex128

	in  [window]float64 // the latest window of input
	out [window]float64 // overlap-add accumulator; out[:hop] is complete

	frames      int
	s, sMin     [nbins]float64 // smoothed power and its minimum (MCRA)
	sTmp        [nbins]float64
	p           [nbins]float64 // speech presence probability
	noise       [nbins]float64 // noise power estimate
	gPrev, gamP [nbins]float64 // previous gain and a posteriori SNR
	speech      float64        // long-term speech power (sum over bins)
	floorNow    float64        // least frame power in the current second
	floorPrev   float64        // ... and in the one before
	restart     bool           // no gain history: the next a priori SNR is γ - 1
}

// New returns a suppressor.
func New(cfg Config) *Suppressor {
	return &Suppressor{cfg: cfg, floor: math.Pow(10, cfg.Floor/20), fft: dsp.NewFFT(nfft), restart: true}
}

// Flush discards the signal in flight (the last Delay samples of input) and
// the gain history, keeping the noise and speech level estimates: for a new
// transmission over the same channel.
func (s *Suppressor) Flush() {
	s.in, s.out = [window]float64{}, [window]float64{}
	s.gPrev, s.gamP = [nbins]float64{}, [nbins]float64{}
	s.restart = true
}

// Process denoises n samples (a multiple of 80) in place; the output lags
// the input by Delay samples, the first Delay of them being silence.
func (s *Suppressor) Process(x []float64) {
	for i := 0; i+hop <= len(x); i += hop {
		copy(s.in[:], s.in[hop:])
		copy(s.in[window-hop:], x[i:i+hop])
		s.frame()
		copy(x[i:i+hop], s.out[:hop])
		copy(s.out[:], s.out[hop:])
		for j := window - hop; j < window; j++ {
			s.out[j] = 0
		}
	}
}

// ProcessPCM is Process for 16-bit samples, rounded and clipped back.
func (s *Suppressor) ProcessPCM(pcm []int16) {
	var x [hop]float64
	for i := 0; i+hop <= len(pcm); i += hop {
		for j := range x {
			x[j] = float64(pcm[i+j])
		}
		s.Process(x[:])
		for j, v := range x {
			pcm[i+j] = int16(math.Max(-32768, math.Min(32767, math.Round(v))))
		}
	}
}

// frame analyses the latest window, applies the gains and overlap-adds the
// result.
func (s *Suppressor) frame() {
	for n := range s.buf {
		s.buf[n] = 0
	}
	for n := 0; n < window; n++ {
		s.buf[n] = complex(s.in[n]*sqrtHann[n], 0)
	}
	s.fft.Transform(s.buf[:])
	var pw [nbins]float64
	for k := range pw {
		re, im := real(s.buf[k]), imag(s.buf[k])
		pw[k] = re*re + im*im
	}
	s.track(&pw)
	floor := s.depth(&pw)

	var g [nbins]float64
	for k := range g {
		gamma := pw[k] / math.Max(s.noise[k], 1e-10)
		xi := s.cfg.AlphaDD*s.gPrev[k]*s.gPrev[k]*s.gamP[k] + (1-s.cfg.AlphaDD)*math.Max(gamma-1, 0)
		if s.restart {
			xi = math.Max(gamma-1, 0)
		}
		xi = math.Max(xi, xiMin)
		v := xi * gamma / (1 + xi)
		gk := xi / (1 + xi) * math.Exp(0.5*expint(v))
		gk = math.Min(gk, 1)
		s.gPrev[k], s.gamP[k] = gk, gamma
		g[k] = math.Max(gk, floor)
	}
	s.restart = false

	// Apply the gains to the full (Hermitian) spectrum and transform back:
	// the inverse is the forward transform of the conjugate, conjugated.
	for k := 0; k < nfft; k++ {
		kk := k
		if kk > nfft/2 {
			kk = nfft - k
		}
		s.buf[k] = complex(real(s.buf[k])*g[kk], -imag(s.buf[k])*g[kk])
	}
	s.fft.Transform(s.buf[:])
	const scale = 1 / (nfft * 1.5)
	for n := 0; n < window; n++ {
		s.out[n] += real(s.buf[n]) * scale * sqrtHann[n]
	}
}

// depth updates the long-term speech and background levels with the power
// spectrum pw and returns the gain floor for the frame: s.floor when the
// background is within cfg.Full dB of the speech, 1 (no suppression) beyond
// cfg.None dB.  The background level is the least frame power over the last
// one to two seconds: unlike the smoothed noise estimate, it reaches the
// noise floor in any pause of a few tens of milliseconds, however quiet.
func (s *Suppressor) depth(pw *[nbins]float64) float64 {
	p := 0.0
	for k := range pw {
		p += pw[k]
	}
	if s.frames%s.cfg.MinWindow == 1 {
		s.floorPrev, s.floorNow = s.floorNow, p
	}
	s.floorNow = math.Min(s.floorNow, p)
	n := s.floorNow
	if s.floorPrev > 0 {
		n = math.Min(n, s.floorPrev)
	}
	if p > 10*n { // a frame well above the background: speech
		if s.speech == 0 {
			s.speech = p
		}
		s.speech = 0.99*s.speech + 0.01*p
	}
	if s.speech == 0 || n <= 0 {
		return s.floor
	}
	snr := 10 * math.Log10(s.speech/n)
	w := (s.cfg.None - snr) / (s.cfg.None - s.cfg.Full)
	w = math.Max(0, math.Min(1, w))
	return math.Pow(s.floor, w)
}

// track updates the MCRA noise estimate with the power spectrum pw.
func (s *Suppressor) track(pw *[nbins]float64) {
	s.frames++
	var sf [nbins]float64
	for k := range sf {
		lo, hi := pw[max(k-1, 0)], pw[min(k+1, nbins-1)]
		sf[k] = 0.25*lo + 0.5*pw[k] + 0.25*hi
	}
	if s.frames == 1 {
		s.s, s.sMin, s.sTmp, s.noise = sf, sf, sf, *pw
		return
	}
	for k := range sf {
		s.s[k] = alphaS*s.s[k] + (1-alphaS)*sf[k]
		s.sMin[k] = math.Min(s.sMin[k], s.s[k])
		s.sTmp[k] = math.Min(s.sTmp[k], s.s[k])
	}
	if s.frames%s.cfg.MinWindow == 0 {
		for k := range sf {
			s.sMin[k] = math.Min(s.sTmp[k], s.s[k])
			s.sTmp[k] = s.s[k]
		}
	}
	for k := range sf {
		present := 0.0
		if s.s[k] > s.cfg.Delta*s.sMin[k] {
			present = 1
		}
		s.p[k] = alphaP*s.p[k] + (1-alphaP)*present
		a := s.cfg.AlphaD + (1-s.cfg.AlphaD)*s.p[k]
		if s.frames <= warmFrames {
			a = math.Min(a, 1-1/float64(s.frames))
		}
		// A frame counts towards the noise with at most Delta times the
		// tracked minimum, so that speech onsets (still below the presence
		// threshold after smoothing) do not inflate the estimate (as in
		// Cohen's improved MCRA, which excludes strong components).
		s.noise[k] = a*s.noise[k] + (1-a)*math.Min(pw[k], s.cfg.Delta*s.sMin[k])*s.cfg.Bias
	}
}

// expint returns the exponential integral E1(v) for v > 0 (Abramowitz and
// Stegun 5.1.53 below 1 and 5.1.56 above; error below 5e-5 relative).
func expint(v float64) float64 {
	if v <= 0 {
		return math.Inf(1)
	}
	if v < 1 {
		return -math.Log(v) - 0.57721566 + v*(0.99999193+v*(-0.24991055+v*(0.05519968+v*(-0.00976004+v*0.00107857))))
	}
	return math.Exp(-v) / v * (v*v + 2.334733*v + 0.250621) / (v*v + 3.330657*v + 1.681534)
}
