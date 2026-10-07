package mbe

import (
	"math"

	"github.com/nnamon/go-ambe2/internal/dsp"
)

const (
	synN    = FrameSamples // samples per synthesis frame
	wsHalf  = 105          // w_S spans n = -105..105 (non-zero for |n| <= 104)
	noiseN  = 2*104 + 1    // noise samples windowed per frame (n = -104..104)
	NoiseHi = 53125        // modulus of the noise generator (TIA-102.BABA eq. 117)
)

// Model is a frame of MBE parameters as the Synthesizer uses them:
// fundamental, number of harmonics, per-harmonic voicing and (enhanced)
// spectral amplitudes in TIA-102.BABA units (a voiced harmonic of amplitude
// M is synthesized as 2M·cos).
type Model struct {
	W0     float64
	L      int
	Voiced [MaxL + 2]bool
	M      [MaxL + 2]float64
	// Pure disables phase randomisation and dispersion (tone frames).
	Pure bool
}

// Synthesizer implements TIA-102.BABA chapter 11 speech synthesis.
type Synthesizer struct {
	fft    *dsp.FFT
	buf    [DFTN]complex128
	gammaW float64

	noise [noiseN]float64 // u(n) for the current frame, n = -104..104
	uLast float64         // last generated noise value

	Prev    Model
	prevUw  [DFTN]float64     // ũ_w(n; -1), index n mod 256
	psi     [MaxL + 2]float64 // ψ_l
	phi     [MaxL + 2]float64 // φ_l
	started bool

	// Dispersed adds a fixed per-harmonic phase to the voiced phases.
	Dispersed bool
	// InterpLimit: harmonics below it that are voiced in both frames are
	// synthesized with amplitude/phase interpolation (eq. 134); TIA-102.BABA uses 8.
	InterpLimit int
}

// NewSynthesizer returns a synthesizer in its initial state, with the
// TIA-102.BABA phase model (no dispersion, interpolation below the 8th harmonic).
func NewSynthesizer() *Synthesizer {
	s := &Synthesizer{fft: dsp.NewFFT(DFTN), InterpLimit: 8}
	// γ_w (eq. 121).
	var sumR, sumR2, sumS2 float64
	for _, w := range WR {
		sumR += w
		sumR2 += w * w
	}
	for _, w := range WS {
		sumS2 += w * w
	}
	s.gammaW = sumR * math.Sqrt(sumS2/sumR2)
	// u(-105) = 3147; the first frame windows u(-104..104).
	s.uLast = 3147
	for i := range s.noise {
		s.uLast = NextNoise(s.uLast)
		s.noise[i] = s.uLast
	}
	s.Prev.W0 = 2 * math.Pi / 32
	s.Prev.L = 0
	return s
}

// NextNoise is the recommended noise generator u(n+1) = (171 u(n) + 11213) mod 53125.
func NextNoise(u float64) float64 {
	return math.Mod(171*u+11213, NoiseHi)
}

func ws(n int) float64 {
	if n < -wsHalf || n > wsHalf {
		return 0
	}
	return WS[n+wsHalf]
}

// advanceNoise shifts the noise sequence by one frame (160 samples).
func (s *Synthesizer) advanceNoise() {
	copy(s.noise[:], s.noise[synN:])
	for i := noiseN - synN; i < noiseN; i++ {
		s.uLast = NextNoise(s.uLast)
		s.noise[i] = s.uLast
	}
}

// synthesize produces one frame of speech from the current model m,
// interpolating from the previous frame's model.
func (s *Synthesizer) Synthesize(m *Model, out *[synN]float64) {
	if s.started {
		s.advanceNoise()
	}
	s.started = true
	for i := range out {
		out[i] = 0
	}
	s.unvoiced(m, out)
	s.voiced(m, out)
	s.Prev = *m
}

// unvoiced adds the unvoiced component (eq. 117-126).
func (s *Synthesizer) unvoiced(m *Model, out *[synN]float64) {
	for i := range s.buf {
		s.buf[i] = 0
	}
	for n := -104; n <= 104; n++ {
		s.buf[(n+DFTN)%DFTN] = complex(s.noise[n+104]*ws(n), 0)
	}
	s.fft.Transform(s.buf[:])
	var Uw [DFTN/2 + 1]complex128
	copy(Uw[:], s.buf[:DFTN/2+1])

	var spec [DFTN/2 + 1]complex128
	for l := 1; l <= m.L; l++ {
		lo, hi := BandBins(l, m.W0)
		if m.Voiced[l] || hi <= lo {
			continue
		}
		e := 0.0
		for k := lo; k < hi; k++ {
			e += real(Uw[k])*real(Uw[k]) + imag(Uw[k])*imag(Uw[k])
		}
		if e <= 0 {
			continue
		}
		g := s.gammaW * m.M[l] / math.Sqrt(e/float64(hi-lo))
		for k := lo; k < hi; k++ {
			spec[k] = Uw[k] * complex(g, 0)
		}
	}
	// Inverse DFT of the Hermitian Spectrum: ũ_w(n) = (1/256) Σ Ũ_w(m) e^{j2πmn/256}.
	for i := range s.buf {
		s.buf[i] = 0
	}
	for k := 1; k < DFTN/2; k++ {
		s.buf[k] = spec[k]
		s.buf[DFTN-k] = complexConj(spec[k])
	}
	s.buf[0] = spec[0]
	s.buf[DFTN/2] = complex(real(spec[DFTN/2]), 0)
	for i := range s.buf {
		s.buf[i] = complexConj(s.buf[i])
	}
	s.fft.Transform(s.buf[:])
	var cur [DFTN]float64
	for i := range cur {
		cur[i] = real(s.buf[i]) / DFTN
	}
	// Weighted overlap-add with the previous frame (eq. 126).
	for n := 0; n < synN; n++ {
		a, b := ws(n), ws(n-synN)
		den := a*a + b*b
		if den == 0 {
			continue
		}
		v := 0.0
		if n <= 127 {
			v += a * s.prevUw[n]
		}
		if n-synN >= -128 {
			v += b * cur[(n-synN+DFTN)%DFTN]
		}
		out[n] += v / den
	}
	s.prevUw = cur
}

func complexConj(c complex128) complex128 { return complex(real(c), -imag(c)) }

// voiced adds the voiced component (eq. 127-141) and updates the phases.
func (s *Synthesizer) voiced(m *Model, out *[synN]float64) {
	p := &s.Prev
	w0p, w0c := p.W0, m.W0
	// Phase updates (eq. 139-141).
	var psiPrev, phiPrev [MaxL + 2]float64
	psiPrev, phiPrev = s.psi, s.phi
	for l := 1; l <= MaxL; l++ {
		s.psi[l] = psiPrev[l] + (w0p+w0c)*float64(l)*synN/2
	}
	luv := 0
	for l := 1; l <= m.L; l++ {
		if !m.Voiced[l] {
			luv++
		}
	}
	lmax := m.L
	if p.L > lmax {
		lmax = p.L
	}
	for l := 1; l <= MaxL; l++ {
		s.phi[l] = s.psi[l]
		if m.Pure {
			continue
		}
		if s.Dispersed {
			s.phi[l] += dispersion[l]
		}
		if l > m.L/4 && l <= lmax && m.L > 0 {
			rho := 2*math.Pi*s.noise[104+l]/NoiseHi - math.Pi
			s.phi[l] += float64(luv) * rho / float64(m.L)
		}
	}

	stable := math.Abs(w0c-w0p) < 0.1*w0c
	for l := 1; l <= lmax; l++ {
		vp := l <= p.L && p.Voiced[l]
		vc := l <= m.L && m.Voiced[l]
		var mp, mc float64
		if l <= p.L {
			mp = p.M[l]
		}
		if l <= m.L {
			mc = m.M[l]
		}
		fl := float64(l)
		switch {
		case !vp && !vc:
			// eq. 130: nothing.
		case vp && !vc:
			for n := 0; n < synN; n++ {
				out[n] += 2 * ws(n) * mp * math.Cos(w0p*float64(n)*fl+phiPrev[l])
			}
		case !vp && vc:
			for n := 0; n < synN; n++ {
				out[n] += 2 * ws(n-synN) * mc * math.Cos(w0c*float64(n-synN)*fl+s.phi[l])
			}
		case (l >= s.InterpLimit) || !stable:
			for n := 0; n < synN; n++ {
				out[n] += 2 * (ws(n)*mp*math.Cos(w0p*float64(n)*fl+phiPrev[l]) +
					ws(n-synN)*mc*math.Cos(w0c*float64(n-synN)*fl+s.phi[l]))
			}
		default:
			// eq. 134-138: amplitude and phase interpolation.
			dphi := s.phi[l] - phiPrev[l] - (w0p+w0c)*fl*synN/2
			dw := (dphi - 2*math.Pi*math.Floor((dphi+math.Pi)/(2*math.Pi))) / synN
			for n := 0; n < synN; n++ {
				fn := float64(n)
				a := mp + fn/synN*(mc-mp)
				th := phiPrev[l] + (w0p*fl+dw)*fn + (w0c-w0p)*fl*fn*fn/(2*synN)
				out[n] += 2 * a * math.Cos(th)
			}
		}
	}
}

// Enhance applies the spectral amplitude enhancement of TIA-102.BABA
// chapter 8 (eq. 105-110) to M (indices 1..L) in place.
func Enhance(w0 float64, L int, M *[MaxL + 2]float64) {
	var r0, r1 float64
	for l := 1; l <= L; l++ {
		m2 := M[l] * M[l]
		r0 += m2
		r1 += m2 * math.Cos(w0*float64(l))
	}
	if r0 <= 0 || r0*r0 == r1*r1 {
		return
	}
	k := 0.96 * math.Pi / (w0 * r0 * (r0*r0 - r1*r1))
	sum := 0.0
	for l := 1; l <= L; l++ {
		if 8*l > L {
			x := k * (r0*r0 + r1*r1 - 2*r0*r1*math.Cos(w0*float64(l)))
			W := 0.0
			if x > 0 {
				W = math.Sqrt(M[l]) * math.Pow(x, 0.25)
			}
			switch {
			case W > 1.2:
				M[l] *= 1.2
			case W < 0.5:
				M[l] *= 0.5
			default:
				M[l] *= W
			}
		}
		sum += M[l] * M[l]
	}
	if sum > 0 {
		g := math.Sqrt(r0 / sum)
		for l := 1; l <= L; l++ {
			M[l] *= g
		}
	}
}

// dispersion is a fixed pseudo-random phase per harmonic.  Like the MD-380
// decoder (whose steady-state harmonic phases are fixed but not aligned), this
// avoids the high crest factor of phase-aligned harmonics.
var dispersion = func() (d [MaxL + 2]float64) {
	u := 3147.0
	for l := range d {
		u = NextNoise(u)
		d[l] = 2*math.Pi*u/NoiseHi - math.Pi
	}
	return d
}()
