package p25full

import (
	"github.com/nnamon/mbevoc/internal/codebook"
	"github.com/nnamon/mbevoc/internal/ecc"
)

// SoftFrame holds soft decisions for a 144-bit P25 frame in transmission
// order: log-likelihood ratios log(P(0)/P(1)), positive for a likely 0, with
// the magnitude as confidence (0: no information).
type SoftFrame [144]float64

// SoftProVoiceFrame holds soft decisions for a 142-bit ProVoice frame.
type SoftProVoiceFrame [142]float64

func flipBy(s []float64, mask uint32) {
	for i := range s {
		if mask>>uint(i)&1 == 1 {
			s[i] = -s[i]
		}
	}
}

// DecodeSoft decodes a soft frame: maximum-likelihood decoding of the Golay
// and Hamming words (û7 is a hard decision).  Errors counts the hard-decision
// bits each decoded codeword contradicts, which can exceed the codes' hard
// correction capability.
func (f *SoftFrame) DecodeSoft() (Bits, Errors) {
	var s [8][23]float64
	for k, d := range codebook.IMBEInterleave {
		s[d[0][0]][d[0][1]] = f[2*k]
		s[d[1][0]][d[1][1]] = f[2*k+1]
	}
	var u [8]uint32
	var e Errors
	u[0], e.E[0] = ecc.Decode23Soft(s[0][:])
	m := modulation(u[0])
	for i := 1; i < 4; i++ {
		flipBy(s[i][:23], m[i])
		u[i], e.E[i] = ecc.Decode23Soft(s[i][:])
	}
	for i := 4; i < 7; i++ {
		flipBy(s[i][:15], m[i])
		u[i], e.E[i] = ecc.DecodeHamming15Soft(s[i][:15])
	}
	u[7] = ecc.Hard(s[7][:7])
	return fromVectors(u), e
}

// pvC0Words and pvHammingWords are the codewords of ProVoice's c0 (with its
// parity bit) and of its Hamming code, for soft decoding.
var pvC0Words [128]uint32
var pvHammingWords [2048]uint32

func init() {
	for d := range pvC0Words {
		g := ecc.Golay23(uint32(d)) & 0x3FFFF
		pvC0Words[d] = g<<1 | ecc.Parity(g)
	}
	for d := range pvHammingWords {
		pvHammingWords[d] = pvHamming(uint32(d))
	}
}

// DecodeSoft decodes a soft ProVoice frame by maximum likelihood (c6 is a
// hard decision); the parity bits of c0 and c1 take part in decoding.
func (f *SoftProVoiceFrame) DecodeSoft() (Bits, Errors) {
	var s [7][24]float64
	for n, v := range codebook.ProVoiceOrder {
		s[v[0]][v[1]] = f[n]
	}
	var u [7]uint32
	var e Errors
	u[0], e.E[0] = ecc.DecodeCodebookSoft(pvC0Words[:], s[0][:19])
	m := proVoiceModulation(u[0])
	flipBy(s[1][:24], m[1])
	u[1], e.E[1] = ecc.Decode24Soft(s[1][:])
	for i := 2; i < 4; i++ {
		flipBy(s[i][:23], m[i])
		u[i], e.E[i] = ecc.Decode23Soft(s[i][:23])
	}
	for i := 4; i < 6; i++ {
		flipBy(s[i][:15], m[i])
		u[i], e.E[i] = ecc.DecodeCodebookSoft(pvHammingWords[:], s[i][:15])
	}
	u[6] = ecc.Hard(s[6][:23])
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

// DecodeFrameSoft decodes a soft 144-bit frame, with the error handling of
// DecodeFrame.
func (d *Decoder) DecodeFrameSoft(f *SoftFrame) ([FrameSamples]int16, Errors) {
	b, e := f.DecodeSoft()
	d.errRate = 0.95*d.errRate + 0.000365*float64(e.Total())
	return d.decode(&b, e, true), e
}

// DecodeProVoiceSoft decodes a soft 142-bit ProVoice frame.
func (d *Decoder) DecodeProVoiceSoft(f *SoftProVoiceFrame) ([FrameSamples]int16, Errors) {
	b, e := f.DecodeSoft()
	d.errRate = 0.95*d.errRate + 0.000365*float64(e.Total())
	return d.decode(&b, e, true), e
}
