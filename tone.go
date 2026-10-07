package ambe

import (
	"math"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/mbe"
)

// toneTable gives, for tone indices 128..163, the MBE representation of
// TIA-102.BABA-1 Annex J: fundamental f0 (Hz) and the two harmonic numbers
// that carry the tone (DTMF, KNOX and call-progress tones of Table 9).
var toneTable = map[int]struct {
	f0     float64
	l1, l2 int
}{
	128: {78.5, 12, 17}, 129: {173.48, 4, 7}, 130: {70.0, 10, 19}, 131: {87.0, 8, 17},
	132: {109.95, 7, 11}, 133: {191.68, 4, 7}, 134: {70.17, 11, 21}, 135: {71.06, 12, 17},
	136: {121.58, 7, 11}, 137: {212.0, 4, 7}, 138: {116.41, 6, 14}, 139: {96.15, 8, 17},
	140: {71.0, 12, 23}, 141: {234.26, 4, 7}, 142: {134.38, 7, 9}, 143: {134.35, 7, 11},
	144: {68.33, 12, 17}, 145: {150.89, 4, 7}, 146: {67.82, 9, 17}, 147: {86.5, 7, 15},
	148: {95.79, 7, 11}, 149: {166.92, 4, 7}, 150: {67.7, 10, 19}, 151: {74.74, 10, 14},
	152: {105.90, 7, 11}, 153: {92.78, 8, 14}, 154: {101.55, 6, 14}, 155: {84.02, 8, 17},
	156: {67.83, 11, 21}, 157: {102.3, 8, 14}, 158: {117.0, 7, 9}, 159: {117.49, 7, 11},
	160: {87.78, 4, 5}, 161: {70.83, 6, 7}, 162: {122.0, 4, 5}, 163: {70.0, 5, 7},
}

// IsTone reports whether a frame is a tone frame: TIA-102.BABA-1 clause 7
// identifies tone frames by the first six bits of u0 all being one (b0 >= 120
// with the two most significant bits of b1 set).  b0 alone is not enough: its
// three least significant bits carry tone index bits, so a tone frame's b0 can
// be anywhere in 120..127.
func IsTone(b *frame.Bits) bool {
	for i := 0; i < 6; i++ {
		if b[i] == 0 {
			return false
		}
	}
	return true
}

// ToneParams returns the tone index and 7-bit log amplitude carried by a tone
// frame (TIA-102.BABA-1 Table 10).  The index is sent four times; a majority
// vote over the copies is returned.
func ToneParams(b *frame.Bits) (id, amp int) {
	u := func(lo, n int) int {
		v := 0
		for i := lo; i < lo+n; i++ {
			v = v<<1 | int(b[i]&1)
		}
		return v
	}
	// u0 = bits 0..11, u1 = 12..23, u2 = 24..34, u3 = 35..48 (MSB first).
	amp = u(6, 6)<<1 | u(35+9, 1)
	copies := [4]int{
		u(12, 8),               // u1(11..4)
		u(20, 4)<<4 | u(24, 4), // u1(3..0), u2(10..7)
		u(28, 7)<<1 | u(35, 1), // u2(6..0), u3(13)
		u(36, 8),               // u3(12..5)
	}
	best, votes := copies[0], 0
	for _, c := range copies {
		n := 0
		for _, o := range copies {
			if o == c {
				n++
			}
		}
		if n > votes {
			best, votes = c, n
		}
	}
	return best, amp
}

// toneMBE maps a tone index to its MBE representation (Annex J).  ok is false
// for invalid indices; index 255 (zero amplitude) returns l1 = l2 = 0.
func toneMBE(id int) (f0 float64, l1, l2 int, ok bool) {
	switch {
	case id == 255:
		return 250, 0, 0, true
	case id >= 5 && id <= 122:
		// Single tones at 31.25·ID Hz, carried as harmonic n of 31.25·ID/n
		// with n = 1..10 for the ID ranges 5-12, 13-25, ..., 116-122.
		// (The draft's 5.2803·ID for 65-76 is read as 31.25/6 = 5.2083.)
		bounds := [...]int{12, 25, 38, 51, 64, 76, 89, 102, 115, 122}
		n := 1
		for n <= len(bounds) && id > bounds[n-1] {
			n++
		}
		return 31.25 * float64(id) / float64(n), n, n, true
	}
	t, found := toneTable[id]
	return t.f0, t.l1, t.l2, found
}

// toneLevel is the MD-380 decoder's tone level relative to the Annex J
// amplitude (eq. 68), measured on single tones (+0.47 dB).
const toneLevel = 1.056

// toneModel builds the synthesis model for a tone frame (BABA-1 eq. 65-68).
func toneModel(b *frame.Bits) (mbe.Model, bool) {
	var m mbe.Model
	id, ad := ToneParams(b)
	f0, l1, l2, ok := toneMBE(id)
	if !ok {
		return m, false
	}
	m.W0 = 2 * math.Pi * f0 / 8000
	m.L = int(3812.5 / f0)
	if m.L > len(m.M)-2 {
		m.L = len(m.M) - 2
	}
	if l1 == 0 {
		return m, true // ID 255: zero amplitude
	}
	a := 16384 * math.Pow(10, 0.03555*float64(ad-127))
	for _, l := range []int{l1, l2} {
		m.Voiced[l] = true
		m.M[l] = a
	}
	return m, true
}

// ToneFrame builds a tone frame (TIA-102.BABA-1 Table 10) for tone index id
// (see Table 9: 5..122 single tones at 31.25·id Hz, 128..163 DTMF/KNOX/call
// progress, 255 silence) and 7-bit log amplitude amp (127 = full scale,
// 0.711 dB per step).
func ToneFrame(id, amp int) frame.Bits {
	id &= 0xFF
	amp &= 0x7F
	u := [4]struct{ v, n int }{
		{63<<6 | amp>>1, 12},
		{id<<4 | id>>4, 12},
		{(id&0xF)<<7 | id>>1, 11},
		{(id&1)<<13 | id<<5 | (amp&1)<<4, 14},
	}
	var b frame.Bits
	p := 0
	for _, x := range u {
		for i := x.n - 1; i >= 0; i-- {
			b[p] = uint8(x.v>>uint(i)) & 1
			p++
		}
	}
	return b
}
