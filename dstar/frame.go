package dstar

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/nnamon/mbevoc/internal/codebook"
	"github.com/nnamon/mbevoc/internal/ecc"
)

// Params holds the quantizer values b0..b8: b0 the fundamental (7 bits),
// b1 the voicing (4), b2 the gain (6), b3 and b4 the PRBA vector (9 and 7),
// b5..b8 the higher-order coefficients (4, 4, 4 and 3 bits).
type Params [9]uint16

// Widths gives the bit width of each of b0..b8.
var Widths = [9]int{7, 4, 6, 9, 7, 4, 4, 4, 3}

// Bits is a frame's 49 information bits: the vectors u0 (12 bits), u1 (12),
// u2 (11) and u3 (14) concatenated, each MSB first, as mbelib's ambe_d.  Bit
// 24 carries no parameter: in the 72-bit frame its place holds the parity bit
// that extends c1 to a [24,12] Golay codeword (see Encode), and Decode
// returns it as 0.
type Bits [49]uint8

// Frame is the 72-bit frame in transmission order: bit i is bit i&7 (LSB
// first) of byte i>>3 of the 9-byte D-STAR voice data.
type Frame [72]uint8

type slot struct{ field, sig uint8 }

// layout[pos] says which field and bit position pos carries (mbelib's
// mbe_decodeAmbe2400Parms); pos 24 is unused.
var layout [49]slot

const unused = 24

func init() {
	for _, f := range []struct {
		field uint8
		pos   []int
	}{
		{0, []int{0, 1, 2, 3, 4, 5, 48}},
		{1, []int{38, 39, 40, 41}},
		{2, []int{6, 7, 8, 9, 42, 43}},
		{3, []int{10, 11, 12, 13, 14, 15, 16, 44, 45}},
		{4, []int{17, 18, 19, 20, 21, 46, 47}},
		{5, []int{22, 23, 25, 26}},
		{6, []int{27, 28, 29, 30}},
		{7, []int{31, 32, 33, 34}},
		{8, []int{35, 36, 37}},
	} {
		w := Widths[f.field]
		for i, p := range f.pos {
			layout[p] = slot{f.field, uint8(w - 1 - i)}
		}
	}
	layout[unused] = slot{255, 0}
}

// Params extracts b0..b8.
func (b *Bits) Params() Params {
	var p Params
	for pos, s := range layout {
		if s.field != 255 {
			p[s.field] |= uint16(b[pos]&1) << s.sig
		}
	}
	return p
}

// Bits packs b0..b8 into a frame; out-of-range values are masked.
func (p Params) Bits() Bits {
	var b Bits
	for pos, s := range layout {
		if s.field != 255 {
			b[pos] = uint8(p[s.field]>>s.sig) & 1
		}
	}
	return b
}

// IsTone reports whether the frame is a tone frame (b0 = 126 or 127).
func (b *Bits) IsTone() bool {
	for i := 0; i < 6; i++ {
		if b[i] == 0 {
			return false
		}
	}
	return true
}

func (b *Bits) vectors() (u [4]uint32) {
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

func fromVectors(u [4]uint32) (b Bits) {
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

// Encode adds forward error correction and interleaving.  The 48 parameter
// bits are protected as D-STAR's 2400 + 1200 bps format implies: u0 and u1 as
// extended [24,12] Golay codewords, u1's modulated by 24 bits of the
// pseudo-random sequence seeded by u0 (TIA-102.BABA-1 eq. 52-53), and the
// remaining 24 bits unprotected.  In mbelib's description of the frame,
// which reuses the AMBE+2 3600x2450 structure, c1 is a [23,12] word and its
// parity bit sits in the place of information bit 24 (as mbelib-neo encodes
// it); the frame is the same.  The D-STAR null AMBE frame used by gateways
// decodes without errors under this structure.
func (b *Bits) Encode() Frame {
	u := b.vectors()
	pn := ecc.NewPN(u[0])
	m := pn.Mask(24)
	g1 := ecc.Golay23(u[1])
	u[2] = u[2]&0x3FF | (ecc.Parity(g1)^m&1)<<10
	c := [4]uint32{ecc.Golay24(u[0]), g1 ^ m>>1, u[2], u[3]}
	var f Frame
	for i, d := range codebook.DStarInterleave {
		f[i] = uint8(c[d[0]]>>uint(d[1])) & 1
	}
	return f
}

// Errors reports what Decode corrected.
type Errors struct {
	C0, C1 int // bits corrected in c0 and c1 (0..3 each)
	// C0Parity and C1Parity are set when a codeword's extra parity bit
	// disagrees after correction, i.e. it most likely had four or more errors.
	C0Parity, C1Parity bool
}

// Total is the total number of corrected bits.
func (e Errors) Total() int { return e.C0 + e.C1 }

// Decode de-interleaves a frame, corrects errors and returns the 49 bits.
func (f *Frame) Decode() (Bits, Errors) {
	var c [4]uint32
	for i, d := range codebook.DStarInterleave {
		c[d[0]] |= uint32(f[i]&1) << uint(d[1])
	}
	var e Errors
	var u [4]uint32
	u[0], e.C0 = ecc.Decode23(c[0] >> 1)
	e.C0Parity = ecc.Parity(ecc.Golay23(u[0]))^(c[0]&1) != 0
	pn := ecc.NewPN(u[0])
	m := pn.Mask(24)
	u[1], e.C1 = ecc.Decode23(c[1] ^ m>>1)
	e.C1Parity = ecc.Parity(ecc.Golay23(u[1]))^m&1 != c[2]>>10&1
	u[2], u[3] = c[2]&0x3FF, c[3]
	return fromVectors(u), e
}

// Pack returns the 9-byte D-STAR voice data (bit i in bit i&7 of byte i>>3).
func (f *Frame) Pack() (out [9]byte) {
	for i, v := range f {
		out[i>>3] |= (v & 1) << uint(i&7)
	}
	return out
}

// Unpack is the inverse of Pack.
func Unpack(in [9]byte) (f Frame) {
	for i := range f {
		f[i] = (in[i>>3] >> uint(i&7)) & 1
	}
	return f
}

// DMBMagic is the 4-byte header of a dsd-fme .dmb file of D-STAR frames.
const DMBMagic = ".dmb"

// ReadDMB reads a .dmb stream: the header, then per frame a status byte, bits
// 0..47 MSB-first in 6 bytes and bit 48 in the LSB of a seventh (the .amb
// record layout).
func ReadDMB(r io.Reader) ([]Bits, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("dstar: reading .dmb header: %w", err)
	}
	if string(hdr[:]) != DMBMagic {
		return nil, fmt.Errorf("dstar: bad .dmb magic %q", hdr[:])
	}
	br := bufio.NewReader(r)
	var frames []Bits
	for {
		var rec [8]byte
		_, err := io.ReadFull(br, rec[:])
		if errors.Is(err, io.EOF) {
			return frames, nil
		}
		if err != nil {
			return frames, fmt.Errorf("dstar: truncated .dmb record %d: %w", len(frames), err)
		}
		var b Bits
		for i := 0; i < 48; i++ {
			b[i] = (rec[1+i/8] >> (7 - uint(i%8))) & 1
		}
		b[48] = rec[7] & 1
		frames = append(frames, b)
	}
}

// WriteDMB writes frames as a .dmb stream with status 0.
func WriteDMB(w io.Writer, frames []Bits) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(DMBMagic)
	for _, b := range frames {
		var rec [8]byte
		for i := 0; i < 48; i++ {
			rec[1+i/8] |= (b[i] & 1) << (7 - uint(i%8))
		}
		rec[7] = b[48] & 1
		bw.Write(rec[:])
	}
	return bw.Flush()
}

func dstarInterleave() *[72][2]int { return &codebook.DStarInterleave }
