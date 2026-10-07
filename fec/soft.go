package fec

import (
	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/ecc"
)

// Soft72 holds soft decisions for a 72-bit frame in transmission order:
// log-likelihood ratios log(P(0)/P(1)), positive for a likely 0, with the
// magnitude as confidence (0: no information).
type Soft72 [72]float64

// HardSoft72 converts hard bits to soft decisions of equal confidence.
func HardSoft72(b *Bits72) (s Soft72) {
	for i, v := range b {
		s[i] = 1
		if v&1 == 1 {
			s[i] = -1
		}
	}
	return s
}

// DecodeSoft de-interleaves a soft frame and decodes c0 and c1 by maximum
// likelihood (u2 and u3 are hard decisions).  Errors counts the hard-decision
// bits of c0 and c1 that the decoded codewords contradict; these can exceed
// three, and C0Parity is never set (the parity bit takes part in decoding).
func DecodeSoft(in *Soft72) (frame.Bits, Errors) {
	var s [4][24]float64
	for k, d := range interleave {
		s[d[0][0]][d[0][1]] = in[2*k]
		s[d[1][0]][d[1][1]] = in[2*k+1]
	}
	var e Errors
	var u [4]uint32
	u[0], e.C0 = ecc.Decode24Soft(s[0][:])
	m := pnMask(u[0])
	for i := 0; i < 23; i++ {
		if m>>uint(i)&1 == 1 {
			s[1][i] = -s[1][i]
		}
	}
	u[1], e.C1 = ecc.Decode23Soft(s[1][:23])
	u[2], u[3] = ecc.Hard(s[2][:11]), ecc.Hard(s[3][:14])
	return join(u), e
}
