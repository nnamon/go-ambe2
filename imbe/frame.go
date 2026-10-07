package imbe

import (
	"github.com/nnamon/go-ambe2/internal/codebook"
	"github.com/nnamon/go-ambe2/internal/ecc"
)

// MaxB0 is the largest valid fundamental frequency code; larger b0 values are
// reserved and make the decoder repeat the previous frame (§6.1, §7.7).
const MaxB0 = 207

// Params holds the quantizer values b0, b1, ..., b_{L+2} of one frame
// (TIA-102.BABA chapter 6): b0 the fundamental, b1 the V/UV decisions, b2 the
// gain, b3..b7 the rest of the transformed gain vector, b8..b_{L+1} the
// higher-order DCT coefficients and b_{L+2} the synchronization bit.  How
// many are used, and how many bits each has, depends on L, which b0 sets.
type Params [MaxL + 3]uint16

// MaxL is the largest number of spectral amplitudes (harmonics) a frame carries.
const MaxL = 56

// Bits is a frame's 88 information bits: the bit vectors û0..û7 of §7.1
// (12, 12, 12, 12, 11, 11, 11 and 7 bits) concatenated, each MSB first.  This
// is the order of DSD's .imb files and mbelib's imbe_d.
type Bits [88]uint8

// Frame is a 144-bit frame in transmission order (bit 0 is sent first): the
// Golay- and Hamming-coded, modulated and interleaved form of Bits (§7.3-7.5)
// carried in P25 Phase 1 voice LDUs.
type Frame [144]uint8

// Pitch returns the fundamental w0 (radians/sample), the number of harmonics
// L and the number of V/UV bands K for b0 (eq. 46-48).  ok is false for
// reserved values of b0.
func Pitch(b0 uint16) (w0 float64, L, K int, ok bool) {
	if b0 > MaxB0 {
		return 0, 0, 0, false
	}
	w0 = 4 * 3.141592653589793 / (float64(b0) + 39.5)
	L = harmonics(w0)
	K = 12
	if L <= 36 {
		K = (L + 2) / 3
	}
	return w0, L, K, true
}

// harmonics is eq. 47, L = ⌊.9254 ⌊π/w0 + .25⌋⌋.
func harmonics(w0 float64) int {
	return int(0.9254 * float64(int(3.141592653589793/w0+0.25)))
}

// widths returns the number of bits of b3..b_{L+1}, indexed by m.
func widths(L int) (B [MaxL + 2]int) {
	for m := 3; m <= 7; m++ {
		B[m] = codebook.IMBEGainBits[L][m-3]
	}
	for m := 8; m <= L+1; m++ {
		B[m] = codebook.IMBEHOCBits[L][m-8]
	}
	return B
}

// slot names one bit of one quantizer value.
type slot struct{ m, bit uint8 }

// layouts[L][p] is the quantizer bit at position p of Bits for a frame with
// L harmonics (§7.1).
var layouts [MaxL + 1][88]slot

func init() {
	for L := 9; L <= MaxL; L++ {
		layouts[L] = layout(L)
	}
}

// layout builds the bit prioritization of §7.1 (Figures 22-24).  b3..b_{L+1}
// are scanned from the most significant row down, left to right within a
// row (b3 first); the scan fills û0 bits 2..0 and û1..û3, and, after all of
// b1 and bits 2 and 1 of b2, continues through û4..û6 and û7 bits 6..4.
func layout(L int) (pos [88]slot) {
	B := widths(L)
	var scan []slot
	for row := 9; row >= 0; row-- {
		for m := 3; m <= L+1; m++ {
			if B[m] > row {
				scan = append(scan, slot{uint8(m), uint8(row)})
			}
		}
	}
	K := 12
	if L <= 36 {
		K = (L + 2) / 3
	}
	p := 0
	put := func(s slot) { pos[p] = s; p++ }
	for b := 7; b >= 2; b-- {
		put(slot{0, uint8(b)})
	}
	for b := 5; b >= 3; b-- {
		put(slot{2, uint8(b)})
	}
	i := 0
	for p < 48 {
		put(scan[i])
		i++
	}
	for b := K - 1; b >= 0; b-- {
		put(slot{1, uint8(b)})
	}
	put(slot{2, 2})
	put(slot{2, 1})
	for p < 84 {
		put(scan[i])
		i++
	}
	if i != len(scan) {
		panic("imbe: bit allocation does not fill the frame")
	}
	put(slot{2, 0})
	put(slot{0, 1})
	put(slot{0, 0})
	put(slot{uint8(L + 2), 0})
	return pos
}

// Bits arranges the quantizer values into the 88-bit frame (§7.1).  b0 must
// be valid (at most MaxB0); values are masked to their allotted widths.
func (p *Params) Bits() Bits {
	var b Bits
	_, L, _, ok := Pitch(p[0])
	if !ok {
		// Reserved b0: only its own bits are meaningful.
		for i := 0; i < 6; i++ {
			b[i] = uint8(p[0]>>uint(7-i)) & 1
		}
		b[85], b[86] = uint8(p[0]>>1)&1, uint8(p[0])&1
		return b
	}
	for i, s := range layouts[L] {
		b[i] = uint8(p[s.m]>>s.bit) & 1
	}
	return b
}

// B0 returns the frame's b0, which determines how the rest is laid out.
func (b *Bits) B0() uint16 {
	var v uint16
	for i := 0; i < 6; i++ {
		v = v<<1 | uint16(b[i]&1)
	}
	return v<<2 | uint16(b[85]&1)<<1 | uint16(b[86]&1)
}

// Params extracts the quantizer values.  ok is false when b0 is reserved, in
// which case only p[0] is set.
func (b *Bits) Params() (p Params, ok bool) {
	p[0] = b.B0()
	_, L, _, ok := Pitch(p[0])
	if !ok {
		return p, false
	}
	p[0] = 0
	for i, s := range layouts[L] {
		p[s.m] |= uint16(b[i]&1) << s.bit
	}
	return p, true
}

// vectors splits the 88 bits into û0..û7.
var vecLen = [8]int{12, 12, 12, 12, 11, 11, 11, 7}

func (b *Bits) vectors() (u [8]uint32) {
	p := 0
	for i, n := range vecLen {
		for j := 0; j < n; j++ {
			u[i] = u[i]<<1 | uint32(b[p]&1)
			p++
		}
	}
	return u
}

func fromVectors(u [8]uint32) (b Bits) {
	p := 0
	for i, n := range vecLen {
		for j := n - 1; j >= 0; j-- {
			b[p] = uint8(u[i]>>uint(j)) & 1
			p++
		}
	}
	return b
}

// modulation returns the vectors m̂1..m̂6 of eq. 86-93 for û0.
func modulation(u0 uint32) (m [8]uint32) {
	pn := ecc.NewPN(u0)
	for i := 1; i <= 6; i++ {
		n := 23
		if i >= 4 {
			n = 15
		}
		m[i] = pn.Mask(n)
	}
	return m
}

// codeVectors returns the modulated code vectors ĉ0..ĉ7 (§7.3-7.4).
func (b *Bits) codeVectors() (c [8]uint32) {
	u := b.vectors()
	for i := 0; i < 4; i++ {
		c[i] = ecc.Golay23(u[i])
	}
	for i := 4; i < 7; i++ {
		c[i] = ecc.Hamming15(u[i])
	}
	c[7] = u[7]
	m := modulation(u[0])
	for i := range c {
		c[i] ^= m[i]
	}
	return c
}

// Encode adds error control coding, bit modulation and interleaving to an
// 88-bit frame (§7.3-7.5).
func (b *Bits) Encode() Frame {
	c := b.codeVectors()
	var f Frame
	for s, d := range codebook.IMBEInterleave {
		f[2*s] = uint8(c[d[0][0]]>>uint(d[0][1])) & 1
		f[2*s+1] = uint8(c[d[1][0]]>>uint(d[1][1])) & 1
	}
	return f
}

// Errors reports the bit errors corrected while decoding a Frame: E[i] for
// code vector i (0..3 Golay, at most 3 each; 4..6 Hamming, at most 1 each).
type Errors struct{ E [7]int }

// Total is ε_T, the total number of corrected errors (eq. 95).
func (e Errors) Total() int {
	t := 0
	for _, v := range e.E {
		t += v
	}
	return t
}

// Decode de-interleaves a 144-bit frame, corrects errors and removes the bit
// modulation, returning the 88 information bits.
func (f *Frame) Decode() (Bits, Errors) {
	var c [8]uint32
	for s, d := range codebook.IMBEInterleave {
		c[d[0][0]] |= uint32(f[2*s]&1) << uint(d[0][1])
		c[d[1][0]] |= uint32(f[2*s+1]&1) << uint(d[1][1])
	}
	var u [8]uint32
	var e Errors
	u[0], e.E[0] = ecc.Decode23(c[0])
	m := modulation(u[0])
	for i := 1; i < 4; i++ {
		u[i], e.E[i] = ecc.Decode23(c[i] ^ m[i])
	}
	for i := 4; i < 7; i++ {
		u[i], e.E[i] = ecc.DecodeHamming15(c[i] ^ m[i])
	}
	u[7] = c[7]
	return fromVectors(u), e
}

// Pack packs a frame into 18 bytes, first bit in the MSB of byte 0.
func (f *Frame) Pack() (out [18]byte) {
	for i, v := range f {
		out[i/8] |= (v & 1) << (7 - uint(i%8))
	}
	return out
}

// Unpack is the inverse of Pack.
func Unpack(in [18]byte) (f Frame) {
	for i := range f {
		f[i] = (in[i/8] >> (7 - uint(i%8))) & 1
	}
	return f
}
