package mbe

import "math"

// Voicing makes the V/UV decisions of TIA-102.BABA §5.2 over K bands of
// three harmonics, keeping the state they depend on: the tracked maximum
// frame energy ξ_max and the previous frame's decisions.
type Voicing struct {
	// Scale multiplies the V/UV thresholds of eq. 37 (> 1 declares more
	// bands voiced).
	Scale float64

	xiMax float64
	prev  [13]bool // previous frame's decision per band (1..12)
}

// NewVoicing returns the V/UV state at initialisation, with ξ_max at the
// floor of eq. 41 (20000).
func NewVoicing() *Voicing { return NewVoicingFrom(20000) }

// NewVoicingFrom returns the V/UV state with ξ_max initialised to xiMax
// (TIA-102.BABA Annex A gives 100000).
func NewVoicingFrom(xiMax float64) *Voicing { return &Voicing{Scale: 1, xiMax: xiMax} }

// Bands returns K, the number of V/UV bands for L harmonics (eq. 34).
func Bands(L int) int {
	if L <= 36 {
		return (L + 2) / 3
	}
	return 12
}

// BandOfHarmonic returns the V/UV band (1..12) holding harmonic l.
func BandOfHarmonic(l int) int {
	if l <= 36 {
		return (l + 2) / 3
	}
	return 12
}

// Track updates ξ_max with the frame's energies ξ_LF and ξ_HF (eq. 38-41)
// and returns them with ξ_0 = ξ_LF + ξ_HF.  Call it once per frame, before
// Decide.
func (v *Voicing) Track(sp *Spectrum) (lf, hf, xi0 float64) {
	lf, hf = sp.Energies()
	xi0 = lf + hf
	if xi0 > v.xiMax {
		v.xiMax = 0.5*v.xiMax + 0.5*xi0
	} else if x := 0.99*v.xiMax + 0.01*xi0; x > 20000 {
		v.xiMax = x
	} else {
		v.xiMax = 20000
	}
	return lf, hf, xi0
}

// Reset clears the previous frame's decisions (after a frame that was not
// analysed for voicing, such as a silence frame).
func (v *Voicing) Reset() { v.prev = [13]bool{} }

// Decide returns the V/UV decision for each of the K = Bands(L) bands
// (indices 1..K) of a frame whose harmonics l = 1..L of the analysis
// fundamental wa are fitted in fits, given E(P_I) and the energies from Track.
func (v *Voicing) Decide(fits []HarmonicFit, L int, wa, EI, lf, hf, xi0 float64) (vk [13]bool) {
	K := Bands(L)
	M := (0.0025*v.xiMax + xi0) / (0.01*v.xiMax + xi0)
	if lf < 5*hf {
		M *= math.Sqrt(lf / (5 * hf))
	}
	for k := 1; k <= K; k++ {
		lHi := 3 * k
		if k == K {
			lHi = L
		}
		var num, den float64
		for l := 3*k - 2; l <= lHi; l++ {
			num += fits[l].Err
			den += fits[l].Energy
		}
		theta := 0.0
		if !(EI > 0.5 && k >= 2) {
			base := 0.45
			if v.prev[k] {
				base = 0.5625
			}
			theta = base * (1 - 0.3096*float64(k-1)*wa) * M * v.Scale
		}
		vk[k] = den > 0 && num/den < theta
	}
	v.prev = vk
	return vk
}
