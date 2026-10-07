package dstar

import (
	"github.com/nnamon/go-ambe2/internal/codebook"
	"github.com/nnamon/go-ambe2/internal/ecc"
)

// SoftFrame holds soft decisions for a 72-bit frame in transmission order:
// log-likelihood ratios log(P(0)/P(1)), positive for a likely 0, with the
// magnitude as confidence (0: no information).
type SoftFrame [72]float64

// DecodeSoft decodes a soft frame by maximum likelihood over the two
// extended Golay words (c1 with its parity bit, see Bits.Encode); the 24
// unprotected bits are hard decisions.  Errors counts the hard-decision bits
// each decoded codeword contradicts; the parity flags are never set.
func (f *SoftFrame) DecodeSoft() (Bits, Errors) {
	var s [4][24]float64
	for i, d := range codebook.DStarInterleave {
		s[d[0]][d[1]] = f[i]
	}
	var u [4]uint32
	var e Errors
	u[0], e.C0 = ecc.Decode24Soft(s[0][:])
	pn := ecc.NewPN(u[0])
	m := pn.Mask(24)
	// c1 as an extended word: parity bit (in c2's bit 10) at index 0.
	var c1 [24]float64
	c1[0] = s[2][10]
	copy(c1[1:], s[1][:23])
	for i := range c1 {
		if m>>uint(i)&1 == 1 {
			c1[i] = -c1[i]
		}
	}
	u[1], e.C1 = ecc.Decode24Soft(c1[:])
	u[2] = ecc.Hard(s[2][:10])
	u[3] = ecc.Hard(s[3][:14])
	return fromVectors(u), e
}

// DecodeFrameSoft decodes a soft 72-bit frame with the error handling of
// DecodeFrame.
func (d *Decoder) DecodeFrameSoft(f *SoftFrame) ([FrameSamples]int16, Errors) {
	b, e := f.DecodeSoft()
	bad := d.e.Errors(e.C0, e.C1, false)
	return d.decode(&b, bad), e
}
