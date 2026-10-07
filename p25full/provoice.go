package p25full

import (
	"github.com/nnamon/mbevoc/internal/codebook"
	"github.com/nnamon/mbevoc/internal/ecc"
)

// ProVoiceFrame is a 142-bit IMBE 7100x4400 frame, the vocoder frame of
// EDACS ProVoice, in its own bit order (bit 0 first).  It carries the same 88
// parameter bits as a P25 Frame, rearranged and with different error control:
// a shortened [18,7] Golay word (with a parity bit), an extended [24,12]
// Golay word, two [23,12] Golay words, two [15,11] Hamming words of a
// different code from P25's, and 23 unprotected bits.  (A ProVoice voice
// burst interleaves two such frames with signalling; separating them is the
// receiver's job.)
//
// The format follows the reverse-engineered description in mbelib and DSD;
// no specification is published.  The parity bits completing c0 and c1 are
// not read by those decoders and are written here as even parity (c1's
// modulated like the rest of c1), which is unverified against real radios.
type ProVoiceFrame [142]uint8

// proVoiceOrder converts between the 88 parameter bits in P25 order (Bits)
// and ProVoice order.  In ProVoice order the synchronization bit comes
// first, and b1 and bits 2 and 1 of b2, which P25 places at the start of û4,
// come after û3's first 41 bits (mbelib's mbe_convertImbe7100to7200).
func proVoiceOrder(K int) (to7100 [88]int) {
	to7100[87] = 0
	to7100[48+K] = 42
	to7100[49+K] = 43
	for i := 0; i < K; i++ {
		to7100[48+i] = 44 + i
	}
	j, k := 0, 1
	for j < 87 {
		to7100[j] = k
		if j++; j == 48 {
			j += K + 2
		}
		if k++; k == 42 {
			k += K + 2
		}
	}
	return to7100
}

func bandsOfB0(b0 uint16) int {
	_, _, K, ok := Pitch(b0)
	if !ok {
		// Reserved b0: the frame will be discarded; any layout will do.
		return 12
	}
	return K
}

// proVoiceVecLen are the lengths of the information vectors in ProVoice order.
var proVoiceVecLen = [7]int{7, 12, 12, 12, 11, 11, 23}

// pvHammingCols[p] is the syndrome of a single error at bit p of a ProVoice
// Hamming codeword; pvHammingErr inverts it.
var pvHammingErr [16]uint32

func init() {
	for p := 0; p < 15; p++ {
		s := pvSyndrome(1 << uint(p))
		if s == 0 || pvHammingErr[s] != 0 {
			panic("imbe: ProVoice Hamming code has a repeated syndrome")
		}
		pvHammingErr[s] = 1 << uint(p)
	}
}

func pvSyndrome(c uint32) uint32 {
	var s uint32
	for _, m := range codebook.ProVoiceHamming {
		s = s<<1 | ecc.Parity(c&m)
	}
	return s
}

func pvHamming(data uint32) uint32 {
	c := data << 4
	for r, m := range codebook.ProVoiceHamming {
		c |= ecc.Parity(c&m&0x7FF0) << uint(3-r)
	}
	return c
}

func pvHammingDecode(c uint32) (uint32, int) {
	if s := pvSyndrome(c); s != 0 {
		return (c ^ pvHammingErr[s]) >> 4 & 0x7FF, 1
	}
	return c >> 4 & 0x7FF, 0
}

// proVoiceModulation returns the 100 modulation bits for seed, the 7 data
// bits of c0: c1 (24 bits), c2, c3 (23 each), c4, c5 (15 each), as words.
func proVoiceModulation(seed uint32) (m [7]uint32) {
	pn := ecc.NewPN(seed)
	m[1] = pn.Mask(24)
	m[2], m[3] = pn.Mask(23), pn.Mask(23)
	m[4], m[5] = pn.Mask(15), pn.Mask(15)
	return m
}

// EncodeProVoice codes an 88-bit frame as a 142-bit ProVoice frame.
func (b *Bits) EncodeProVoice() ProVoiceFrame {
	ord := proVoiceOrder(bandsOfB0(b.B0()))
	var d [88]uint8
	for i, k := range ord {
		d[k] = b[i]
	}
	var u [7]uint32
	p := 0
	for i, n := range proVoiceVecLen {
		for j := 0; j < n; j++ {
			u[i] = u[i]<<1 | uint32(d[p])
			p++
		}
	}
	var c [7]uint32
	g0 := ecc.Golay23(u[0]) & 0x3FFFF // 7 data bits, 11 parity bits
	c[0] = g0<<1 | ecc.Parity(g0)
	g1 := ecc.Golay23(u[1])
	c[1] = g1<<1 | ecc.Parity(g1)
	c[2], c[3] = ecc.Golay23(u[2]), ecc.Golay23(u[3])
	c[4], c[5] = pvHamming(u[4]), pvHamming(u[5])
	c[6] = u[6]
	m := proVoiceModulation(u[0])
	for i := range c {
		c[i] ^= m[i]
	}
	var f ProVoiceFrame
	for n, v := range codebook.ProVoiceOrder {
		f[n] = uint8(c[v[0]]>>uint(v[1])) & 1
	}
	return f
}

// Decode corrects errors in a ProVoice frame and returns its 88 parameter bits
// in P25 order.  Errors.E[0..5] count the bits corrected in c0..c5 (E[6] is
// always 0: ProVoice has two Hamming words, not three).
func (f *ProVoiceFrame) Decode() (Bits, Errors) {
	var c [7]uint32
	for n, v := range codebook.ProVoiceOrder {
		c[v[0]] |= uint32(f[n]&1) << uint(v[1])
	}
	var u [7]uint32
	var e Errors
	u[0], e.E[0] = ecc.Decode23(c[0] >> 1 & 0x3FFFF)
	u[0] &= 0x7F
	m := proVoiceModulation(u[0])
	for i := range c {
		c[i] ^= m[i]
	}
	u[1], e.E[1] = ecc.Decode23(c[1] >> 1)
	u[2], e.E[2] = ecc.Decode23(c[2])
	u[3], e.E[3] = ecc.Decode23(c[3])
	u[4], e.E[4] = pvHammingDecode(c[4])
	u[5], e.E[5] = pvHammingDecode(c[5])
	u[6] = c[6]
	var d [88]uint8
	p := 0
	for i, n := range proVoiceVecLen {
		for j := n - 1; j >= 0; j-- {
			d[p] = uint8(u[i]>>uint(j)) & 1
			p++
		}
	}
	return fromProVoiceOrder(&d), e
}

// fromProVoiceOrder rearranges 88 bits in ProVoice order into P25 order.
func fromProVoiceOrder(d *[88]uint8) (b Bits) {
	// b0 sits at ProVoice positions 1..6, 86 and 87.
	var b0 uint16
	for i := 1; i <= 6; i++ {
		b0 = b0<<1 | uint16(d[i])
	}
	b0 = b0<<2 | uint16(d[86])<<1 | uint16(d[87])
	for i, k := range proVoiceOrder(bandsOfB0(b0)) {
		b[i] = d[k]
	}
	return b
}

// DecodeProVoice decodes a 142-bit ProVoice frame: error correction, then
// the error estimation, frame repeats and muting of the P25 decoder.
func (d *Decoder) DecodeProVoice(f *ProVoiceFrame) ([FrameSamples]int16, Errors) {
	b, e := f.Decode()
	d.errRate = 0.95*d.errRate + 0.000365*float64(e.Total())
	return d.decode(&b, e, true), e
}

// Pack packs a ProVoice frame into 18 bytes, first bit in the MSB of byte 0
// (the last two bits of byte 17 are 0).
func (f *ProVoiceFrame) Pack() (out [18]byte) {
	for i, v := range f {
		out[i/8] |= (v & 1) << (7 - uint(i%8))
	}
	return out
}

// UnpackProVoice is the inverse of ProVoiceFrame.Pack.
func UnpackProVoice(in [18]byte) (f ProVoiceFrame) {
	for i := range f {
		f[i] = (in[i/8] >> (7 - uint(i%8))) & 1
	}
	return f
}
