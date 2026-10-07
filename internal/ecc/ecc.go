// Package ecc holds the error-control codes of the MBE vocoder frame
// formats: the [23,12] Golay code and its [24,12] extension (TIA-102.BABA
// §7.3, TIA-102.BABA-1 §5.2), the P25 [15,11] Hamming code (TIA-102.BABA
// §7.3) and the pseudo-random bit modulation sequence (TIA-102.BABA §7.4).
//
// Code words are held in the low bits of a uint32 with the data in the high
// part: a [23,12] word is data in bits 22..11 and parity in bits 10..0.
package ecc

// GolayPoly is the [23,12] Golay generator g(x) = x^11 + x^10 + x^6 + x^5 +
// x^4 + x^2 + 1.
const GolayPoly = 0xC75

// Golay23 returns the systematic [23,12] codeword of 12-bit data.
func Golay23(data uint32) uint32 {
	return data<<11 | golayRem(data<<11)
}

func golayRem(r uint32) uint32 {
	for i := 22; i >= 11; i-- {
		if r&(1<<uint(i)) != 0 {
			r ^= GolayPoly << uint(i-11)
		}
	}
	return r & 0x7FF
}

// Golay24 appends an even-parity bit (the LSB) to the [23,12] codeword.
func Golay24(data uint32) uint32 {
	c := Golay23(data)
	return c<<1 | Parity(c)
}

// Parity returns the XOR of the bits of v.
func Parity(v uint32) uint32 {
	v ^= v >> 16
	v ^= v >> 8
	v ^= v >> 4
	v ^= v >> 2
	v ^= v >> 1
	return v & 1
}

// golayErr maps each [23,12] syndrome to its minimum-weight error pattern
// (the code is perfect: every syndrome is a pattern of weight <= 3).
var golayErr [2048]uint32

func init() {
	seen := 0
	set := func(e uint32) {
		s := golayRem(e)
		if s != 0 && golayErr[s] == 0 {
			golayErr[s] = e
			seen++
		}
	}
	for i := 0; i < 23; i++ {
		set(1 << uint(i))
	}
	for i := 0; i < 23; i++ {
		for j := i + 1; j < 23; j++ {
			set(1<<uint(i) | 1<<uint(j))
		}
	}
	for i := 0; i < 23; i++ {
		for j := i + 1; j < 23; j++ {
			for k := j + 1; k < 23; k++ {
				set(1<<uint(i) | 1<<uint(j) | 1<<uint(k))
			}
		}
	}
	if seen != 2047 {
		panic("ecc: Golay syndrome table incomplete")
	}
}

// Decode23 corrects up to three errors in a [23,12] word, returning the data
// and the number of bits corrected.
func Decode23(c uint32) (data uint32, corrected int) {
	e := golayErr[golayRem(c&0x7FFFFF)]
	for v := e; v != 0; v &= v - 1 {
		corrected++
	}
	return ((c ^ e) >> 11) & 0xFFF, corrected
}

// hammingRows are the parity columns of the P25 [15,11] Hamming generator
// g_H (TIA-102.BABA §7.3), for data bits 10 (MSB) down to 0.
var hammingRows = [11]uint32{0xF, 0xE, 0xD, 0xC, 0xB, 0xA, 0x9, 0x7, 0x6, 0x5, 0x3}

// Hamming15 returns the systematic [15,11] codeword of 11-bit data: data in
// bits 14..4, parity in bits 3..0.
func Hamming15(data uint32) uint32 {
	return data<<4 | hammingPar(data)
}

func hammingPar(data uint32) uint32 {
	var p uint32
	for i, r := range hammingRows {
		if data&(1<<uint(10-i)) != 0 {
			p ^= r
		}
	}
	return p
}

// hammingErr maps a [15,11] syndrome to the single-bit error it indicates.
var hammingErr [16]uint32

func init() {
	for i, r := range hammingRows {
		hammingErr[r] = 1 << uint(14-i)
	}
	for j := 0; j < 4; j++ {
		hammingErr[1<<uint(j)] = 1 << uint(j)
	}
}

// DecodeHamming15 corrects up to one error in a [15,11] word, returning the
// data and the number of bits corrected.
func DecodeHamming15(c uint32) (data uint32, corrected int) {
	s := hammingPar(c>>4&0x7FF) ^ c&0xF
	if e := hammingErr[s]; e != 0 {
		c ^= e
		corrected = 1
	}
	return c >> 4 & 0x7FF, corrected
}

// PN is the pseudo-random sequence of TIA-102.BABA eq. 84-85 (and BABA-1
// eq. 52-53): pr(0) = 16·u0, pr(n) = (173·pr(n−1) + 13849) mod 65536.
type PN struct{ pr uint32 }

// NewPN starts the sequence for the 12-bit vector u0.
func NewPN(u0 uint32) PN { return PN{pr: 16 * (u0 & 0xFFF)} }

// Next returns the next modulation bit, ⌊pr(n)/32768⌋.
func (p *PN) Next() uint32 {
	p.pr = (173*p.pr + 13849) % 65536
	return p.pr >> 15
}

// Mask returns the next n modulation bits as an n-bit word, the first bit in
// the MSB.
func (p *PN) Mask(n int) uint32 {
	var m uint32
	for i := 0; i < n; i++ {
		m = m<<1 | p.Next()
	}
	return m
}
