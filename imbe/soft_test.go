package imbe

import (
	"math/rand"
	"testing"
)

// TestSoftDecodeAWGN: random P25 and ProVoice frames as BPSK in Gaussian
// noise; soft decoding of the protected bits must do no worse than hard.
func TestSoftDecodeAWGN(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for _, sigma := range []float64{0.3, 0.55} {
		var hardBad, softBad, pvHard, pvSoft int
		for n := 0; n < 400; n++ {
			p := randomParams(r)
			b := p.Bits()
			f := b.Encode()
			var s SoftFrame
			var h Frame
			for i, v := range f {
				y := 1 - 2*float64(v) + sigma*r.NormFloat64()
				s[i] = 2 * y / (sigma * sigma)
				if y < 0 {
					h[i] = 1
				}
			}
			hb, _ := h.Decode()
			sb, _ := s.DecodeSoft()
			hardBad += boolInt(!protected(&hb, &b, 81))
			softBad += boolInt(!protected(&sb, &b, 81))

			pv := b.EncodeProVoice()
			var ps SoftProVoiceFrame
			var ph ProVoiceFrame
			for i, v := range pv {
				y := 1 - 2*float64(v) + sigma*r.NormFloat64()
				ps[i] = 2 * y / (sigma * sigma)
				if y < 0 {
					ph[i] = 1
				}
			}
			hb, _ = ph.Decode()
			sb, _ = ps.DecodeSoft()
			// ProVoice leaves c6 (P25 bits ... the last 23 in ProVoice order)
			// unprotected; compare everything and allow the same exposure to both.
			pvHard += boolInt(hb != b)
			pvSoft += boolInt(sb != b)
		}
		t.Logf("sigma %.2f: P25 protected bits wrong %d (hard) vs %d (soft); ProVoice frames wrong %d vs %d, of 400",
			sigma, hardBad, softBad, pvHard, pvSoft)
		if softBad > hardBad || pvSoft > pvHard {
			t.Errorf("sigma %.2f: soft decoding worse than hard", sigma)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// protected compares the first n bits (P25: û0..û6 are bits 0..80).
func protected(a, b *Bits, n int) bool {
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
