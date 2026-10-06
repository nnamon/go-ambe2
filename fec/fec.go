// Package fec implements the AMBE+2 3600x2450 channel coding of
// TIA-102.BABA-1 clause 5, which turns a 49-bit voice frame into the 72-bit
// frame carried on air by DMR (three per voice burst), NXDN EHR and P25 Phase 2:
//
//	u0 (12 bits) -> c0 = [24,12] extended Golay
//	u1 (12 bits) -> c1 = [23,12] Golay, XORed with a PN sequence seeded by u0
//	u2 (11 bits) -> c2, u3 (14 bits) -> c3, unprotected
//
// followed by the dibit interleave of BABA-1 Annex H (identical to the DMR
// AMBE+2 interleave).  The 49-bit order of package frame is u0|u1|u2|u3.
package fec

import "github.com/nnamon/go-ambe2/frame"

// Bits72 is a 72-bit frame in transmission order (bit 0 is sent first).
type Bits72 [72]uint8

// golayPoly is g(x) = x^11 + x^10 + x^6 + x^5 + x^4 + x^2 + 1.
const golayPoly = 0xC75

// golay23 returns the systematic [23,12] codeword of 12-bit data:
// data in bits 22..11, parity in bits 10..0.
func golay23(data uint32) uint32 {
	r := data << 11
	for i := 22; i >= 11; i-- {
		if r&(1<<uint(i)) != 0 {
			r ^= golayPoly << uint(i-11)
		}
	}
	return data<<11 | r
}

// golay24 appends an even-parity bit (LSB) to the [23,12] codeword.
func golay24(data uint32) uint32 {
	c := golay23(data)
	return c<<1 | parity(c)
}

func parity(v uint32) uint32 {
	v ^= v >> 16
	v ^= v >> 8
	v ^= v >> 4
	v ^= v >> 2
	v ^= v >> 1
	return v & 1
}

// syndromeErr maps each [23,12] syndrome to its minimum-weight error pattern
// (the code is perfect: every syndrome is a pattern of weight <= 3).
var syndromeErr [2048]uint32

func syndrome(c uint32) uint32 {
	r := c
	for i := 22; i >= 11; i-- {
		if r&(1<<uint(i)) != 0 {
			r ^= golayPoly << uint(i-11)
		}
	}
	return r & 0x7FF
}

func init() {
	seen := 0
	set := func(e uint32) {
		s := syndrome(e)
		if s != 0 && syndromeErr[s] == 0 {
			syndromeErr[s] = e
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
		panic("fec: Golay syndrome table incomplete")
	}
}

// decode23 corrects up to three errors in a [23,12] word, returning the data
// and the number of bits corrected.
func decode23(c uint32) (uint32, int) {
	e := syndromeErr[syndrome(c)]
	n := 0
	for v := e; v != 0; v &= v - 1 {
		n++
	}
	return ((c ^ e) >> 11) & 0xFFF, n
}

// pnMask returns the 23-bit modulation vector m1 for u0 (BABA-1 eq. 52-54),
// with m1's first element in bit 22 (the MSB of c1).
func pnMask(u0 uint32) uint32 {
	pr := 16 * u0
	var m uint32
	for n := 1; n <= 23; n++ {
		pr = (173*pr + 13849) % 65536
		m = m<<1 | pr>>15
	}
	return m
}

// interleave[s] gives the (vector, bit) carried by bit 1 and bit 0 of dibit s.
var interleave = [36][2][2]uint8{
	{{0, 23}, {0, 5}}, {{1, 10}, {2, 3}}, {{0, 22}, {0, 4}}, {{1, 9}, {2, 2}},
	{{0, 21}, {0, 3}}, {{1, 8}, {2, 1}}, {{0, 20}, {0, 2}}, {{1, 7}, {2, 0}},
	{{0, 19}, {0, 1}}, {{1, 6}, {3, 13}}, {{0, 18}, {0, 0}}, {{1, 5}, {3, 12}},
	{{0, 17}, {1, 22}}, {{1, 4}, {3, 11}}, {{0, 16}, {1, 21}}, {{1, 3}, {3, 10}},
	{{0, 15}, {1, 20}}, {{1, 2}, {3, 9}}, {{0, 14}, {1, 19}}, {{1, 1}, {3, 8}},
	{{0, 13}, {1, 18}}, {{1, 0}, {3, 7}}, {{0, 12}, {1, 17}}, {{2, 10}, {3, 6}},
	{{0, 11}, {1, 16}}, {{2, 9}, {3, 5}}, {{0, 10}, {1, 15}}, {{2, 8}, {3, 4}},
	{{0, 9}, {1, 14}}, {{2, 7}, {3, 3}}, {{0, 8}, {1, 13}}, {{2, 6}, {3, 2}},
	{{0, 7}, {1, 12}}, {{2, 5}, {3, 1}}, {{0, 6}, {1, 11}}, {{2, 4}, {3, 0}},
}

// split returns u0..u3 from the 49 bits (u0|u1|u2|u3, MSB first).
func split(b *frame.Bits) (u [4]uint32) {
	lens := [4]int{12, 12, 11, 14}
	p := 0
	for i, n := range lens {
		for j := 0; j < n; j++ {
			u[i] = u[i]<<1 | uint32(b[p]&1)
			p++
		}
	}
	return u
}

func join(u [4]uint32) (b frame.Bits) {
	lens := [4]int{12, 12, 11, 14}
	p := 0
	for i, n := range lens {
		for j := n - 1; j >= 0; j-- {
			b[p] = uint8(u[i]>>uint(j)) & 1
			p++
		}
	}
	return b
}

// Encode adds FEC and interleaving to a 49-bit frame.
func Encode(b *frame.Bits) Bits72 {
	u := split(b)
	c := [4]uint32{golay24(u[0]), golay23(u[1]) ^ pnMask(u[0]), u[2], u[3]}
	var out Bits72
	for s, d := range interleave {
		out[2*s] = uint8(c[d[0][0]]>>d[0][1]) & 1
		out[2*s+1] = uint8(c[d[1][0]]>>d[1][1]) & 1
	}
	return out
}

// Errors reports what Decode corrected.
type Errors struct {
	C0, C1 int // bits corrected in c0 and c1 (0..3 each)
	// C0Parity is set when c0's extra parity bit disagrees after correction,
	// i.e. c0 most likely had four or more errors.
	C0Parity bool
}

// Total is the total number of corrected bits.
func (e Errors) Total() int { return e.C0 + e.C1 }

// Decode de-interleaves a 72-bit frame, corrects errors and returns the 49 bits.
func Decode(in *Bits72) (frame.Bits, Errors) {
	var c [4]uint32
	for s, d := range interleave {
		c[d[0][0]] |= uint32(in[2*s]&1) << d[0][1]
		c[d[1][0]] |= uint32(in[2*s+1]&1) << d[1][1]
	}
	var e Errors
	var u [4]uint32
	u[0], e.C0 = decode23(c[0] >> 1)
	e.C0Parity = parity(golay23(u[0]))^(c[0]&1) != 0
	u[1], e.C1 = decode23(c[1] ^ pnMask(u[0]))
	u[2], u[3] = c[2], c[3]
	return join(u), e
}

// Pack packs a 72-bit frame into 9 bytes, first bit in the MSB of byte 0
// (the layout DMR voice bursts and most network protocols use).
func Pack(b *Bits72) (out [9]byte) {
	for i, v := range b {
		out[i/8] |= (v & 1) << (7 - uint(i%8))
	}
	return out
}

// Unpack is the inverse of Pack.
func Unpack(in [9]byte) (b Bits72) {
	for i := range b {
		b[i] = (in[i/8] >> (7 - uint(i%8))) & 1
	}
	return b
}
