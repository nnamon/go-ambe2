package fec

import (
	"math/rand"
	"testing"

	"github.com/nnamon/go-ambe2/frame"
)

// TestSoftDecodeAWGN sends random frames as BPSK through Gaussian noise and
// counts frames whose protected bits (u0, u1) are decoded wrongly, with hard
// and with soft decisions.  Soft decoding must do no worse, and much better
// at moderate noise.
func TestSoftDecodeAWGN(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for _, sigma := range []float64{0.3, 0.6} {
		hardBad, softBad := 0, 0
		for n := 0; n < 1500; n++ {
			var b frame.Bits
			for i := range b {
				b[i] = uint8(r.Intn(2))
			}
			c := Encode(&b)
			var s Soft72
			var h Bits72
			for i, v := range c {
				y := 1 - 2*float64(v) + sigma*r.NormFloat64()
				s[i] = 2 * y / (sigma * sigma)
				if y < 0 {
					h[i] = 1
				}
			}
			hb, _ := Decode(&h)
			sb, _ := DecodeSoft(&s)
			if !protectedEqual(&hb, &b) {
				hardBad++
			}
			if !protectedEqual(&sb, &b) {
				softBad++
			}
		}
		t.Logf("sigma %.1f: protected bits wrong in %d (hard) vs %d (soft) of 1500 frames", sigma, hardBad, softBad)
		if softBad > hardBad {
			t.Errorf("sigma %.1f: soft decoding worse than hard", sigma)
		}
		if sigma == 0.6 && softBad*2 > hardBad {
			t.Errorf("sigma %.1f: soft decoding gains too little (%d vs %d)", sigma, softBad, hardBad)
		}
	}
	// Equal-confidence soft bits give the hard result (frame bit 0 is c0's MSB).
	var b frame.Bits
	c := Encode(&b)
	c[0] ^= 1
	s := HardSoft72(&c)
	got, e := DecodeSoft(&s)
	if got != b || e.Total() != 1 {
		t.Fatalf("one error: %+v", e)
	}
}

func protectedEqual(a, b *frame.Bits) bool {
	for i := 0; i < 24; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
