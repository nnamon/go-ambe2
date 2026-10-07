package dstar

import (
	"math/rand"
	"testing"
)

func TestSoftDecodeAWGN(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for _, sigma := range []float64{0.3, 0.6} {
		hardBad, softBad := 0, 0
		for n := 0; n < 1500; n++ {
			var b Bits
			for i := range b {
				b[i] = uint8(r.Intn(2))
			}
			b[unused] = 0
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
			if !first24(&hb, &b) {
				hardBad++
			}
			if !first24(&sb, &b) {
				softBad++
			}
		}
		t.Logf("sigma %.1f: protected bits wrong in %d (hard) vs %d (soft) of 1500 frames", sigma, hardBad, softBad)
		if softBad > hardBad {
			t.Errorf("sigma %.1f: soft decoding worse than hard", sigma)
		}
	}
}

func first24(a, b *Bits) bool {
	for i := 0; i < 24; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
