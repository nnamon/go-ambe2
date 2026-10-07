package mbe

import "math"

const (
	lpfHalf = 10
	histLen = 1024
)

// Frontend is the analysis front end shared by the encoders: the input
// high-pass filter, the initial pitch estimate with look-back and look-ahead
// tracking (TIA-102.BABA §5.1.1-5.1.4), and the spectrum S_w(m) of the frame
// being encoded.  It is not safe for concurrent use.
type Frontend struct {
	lookahead int

	hist       [histLen]float64 // high-pass filtered input, newest at the end
	hpfX, hpfY float64

	pa *pitchAnalyzer
	pt *pitchTracker
	// Sp is the spectrum of the frame returned by the latest Push.
	Sp *Spectrum

	errs  [3]pitchErr // E(P) for frames t .. t+lookahead
	nErrs int
	lpf   [2*halfWin + 1]float64
	c     int // history index of the current frame's centre
}

// NewFrontend returns a front end that tracks pitch over lookahead (0..2)
// future frames.
func NewFrontend(lookahead int) *Frontend {
	if lookahead < 0 {
		lookahead = 0
	}
	if lookahead > 2 {
		lookahead = 2
	}
	f := &Frontend{lookahead: lookahead, pa: newPitchAnalyzer(), pt: newPitchTracker(), Sp: NewSpectrum()}
	// Pretend lookahead frames of silence preceded the input so that every
	// call can produce a frame.
	for i := 0; i < lookahead; i++ {
		for j := range f.errs[i] {
			f.errs[i][j] = 1
		}
	}
	f.nErrs = lookahead
	return f
}

// Lookahead returns the number of future frames used for pitch tracking.
func (f *Frontend) Lookahead() int { return f.lookahead }

// Delay is the algorithmic delay in samples: the frame analysed by Push is
// centred this many samples before the end of the input so far.
func (f *Frontend) Delay() int {
	return halfWin + lpfHalf + FrameSamples*f.lookahead
}

// Push consumes the next 20 ms of audio and analyses the frame lookahead
// frames back.  It returns the initial pitch estimate P_I (samples) and its
// error E(P_I); f.Sp then holds that frame's spectrum.
func (f *Frontend) Push(pcm *[FrameSamples]int16) (PI, EI float64) {
	// High-pass filter H(z) = (1 − z⁻¹)/(1 − 0.99 z⁻¹) into the history.
	copy(f.hist[:], f.hist[FrameSamples:])
	base := histLen - FrameSamples
	for i, v := range pcm {
		x := float64(v)
		y := x - f.hpfX + 0.99*f.hpfY
		f.hpfX, f.hpfY = x, y
		f.hist[base+i] = y
	}

	// E(P) for the newest frame whose pitch window (plus LPF taps) is complete.
	cE := histLen - 1 - halfWin - lpfHalf
	for n := -halfWin; n <= halfWin; n++ {
		s := 0.0
		for j := -lpfHalf; j <= lpfHalf; j++ {
			s += f.hist[cE+n-j] * LPF[j+lpfHalf]
		}
		f.lpf[n+halfWin] = s
	}
	f.errs[f.nErrs] = f.pa.analyze(f.lpf[:])
	f.nErrs++

	// The frame analysed now is lookahead frames older.
	f.c = cE - FrameSamples*f.lookahead
	future := make([]*pitchErr, 0, 2)
	for i := 1; i < f.nErrs; i++ {
		future = append(future, &f.errs[i])
	}
	PI, EI = f.pt.track(&f.errs[0], future)
	copy(f.errs[:], f.errs[1:])
	f.nErrs--

	f.Sp.Analyze(f.hist[f.c-WRHalf : f.c+WRHalf+1])
	return PI, EI
}

// Samples returns the n high-pass filtered input samples centred on the
// frame analysed by the latest Push (n even, at most 2·FrameSamples).
func (f *Frontend) Samples(n int) []float64 { return f.hist[f.c-n/2 : f.c+n/2] }

// Energies returns the mean-square input level of the frame analysed by the
// latest Push (its 160 samples) and of the 160 samples after it.
func (f *Frontend) Energies() (cur, next float64) {
	return meanSquare(f.hist[f.c-80 : f.c+80]), meanSquare(f.hist[f.c+80 : f.c+240])
}

func meanSquare(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		s += v * v
	}
	return s / float64(len(x))
}

// RefinePitch is the pitch refinement of §5.1.5: of the ten quarter-sample
// candidates around P_I it returns the fundamental (radians/sample) with the
// least E_R (eq. 24), computed from DFT bin minBin up.
func (sp *Spectrum) RefinePitch(PI float64, minBin int, scratch []HarmonicFit) float64 {
	bestW, bestE := 0.0, math.Inf(1)
	for i := 0; i < 10; i++ {
		P := PI - 9.0/8 + float64(i)/4
		w0 := 2 * math.Pi / P
		if er := sp.RefineError(w0, minBin, scratch); er < bestE {
			bestW, bestE = w0, er
		}
	}
	return bestW
}
