package quant

import (
	"math"

	"github.com/nnamon/mbevoc/internal/codebook"
)

// Codebook is the set of tables that defines a vocoder of the half-rate
// family.  AMBE+2 3600x2450 (TIA-102.BABA-1) and D-STAR's AMBE 3600x2400 share
// the parameter reconstruction (a PRBA vector and higher-order DCT
// coefficients over four blocks, spectral prediction with ρ = 0.65, gain
// prediction with leak 0.5) but differ in their tables, field widths and
// special frame codes.
type Codebook struct {
	name     string
	f0       []float64 // fundamental (cycles/sample) per voice b0
	nharm    []int     // L per voice b0
	vuv      [][8]uint8
	vuvCand  []uint16
	dg       []float64
	prba24   [][3]float64
	prba58   [][4]float64
	hoc      [4][][4]float64
	blockLen *[57][4]int
	kind     func(b0 uint16) Kind
	silence  int // b0 of the silence frame, or -1

	// Precomputed PRBA contributions to R (index 1..8) for the encoder.
	prbaR24 [][9]float64
	prbaR58 [][9]float64
	prbaQ   [][8]float64
}

// String returns the codebook's name.
func (c *Codebook) String() string { return c.name }

// AMBE2 is the AMBE+2 3600x2450 codebook (DMR, NXDN, P25 Phase 2), the
// default of NewPredictor.
var AMBE2 = newCodebook("AMBE+2 3600x2450", codebook.W0[:], codebook.L[:], codebook.VUV[:], VUVCandidates,
	codebook.Dg[:], codebook.PRBA24[:], codebook.PRBA58[:],
	[4][][4]float64{codebook.HOC1[:], codebook.HOC2[:], codebook.HOC3[:], codebook.HOC4[:]}, KindOf)

// DStar is the D-STAR AMBE 3600x2400 codebook.  Its tables and field layout
// are those of mbelib, which reverse-engineered them; the fundamental for
// voice code b0 is 2^(−4.311767578125 − 0.021336·(b0 + 0.5)) cycles/sample.
// Frame codes are classified as for AMBE+2 (KindOf): the D-STAR "null AMBE"
// frame that gateways send for silence has b0 = 124.
var DStar = newCodebook("D-STAR AMBE 3600x2400", dstarF0(), codebook.DStarL[:], codebook.DStarVUV[:],
	[]uint16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	codebook.DStarDg[:], codebook.DStarPRBA24[:], codebook.DStarPRBA58[:],
	[4][][4]float64{codebook.DStarHOC1[:], codebook.DStarHOC2[:], codebook.DStarHOC3[:], codebook.DStarHOC4[:]},
	KindOf)

func dstarF0() []float64 {
	f := make([]float64, 120)
	for b0 := range f {
		f[b0] = math.Exp2(-4.311767578125 - 0.021336*(float64(b0)+0.5))
	}
	return f
}

func newCodebook(name string, f0 []float64, nharm []int, vuv [][8]uint8, cand []uint16, dg []float64,
	p24 [][3]float64, p58 [][4]float64, hoc [4][][4]float64, kind func(uint16) Kind) *Codebook {
	c := &Codebook{name: name, f0: f0, nharm: nharm, vuv: vuv, vuvCand: cand, dg: dg,
		prba24: p24, prba58: p58, hoc: hoc, blockLen: &codebook.BlockLen, kind: kind, silence: -1}
	for b0 := 0; b0 < 128; b0++ {
		if kind(uint16(b0)) == Silence {
			c.silence = b0
			break
		}
	}
	c.prbaR24 = make([][9]float64, len(p24))
	for b := range p24 {
		var G [9]float64
		copy(G[2:5], p24[b][:])
		c.prbaR24[b] = prbaToR(&G)
	}
	c.prbaR58 = make([][9]float64, len(p58))
	c.prbaQ = make([][8]float64, len(p58))
	for b := range p58 {
		var G [9]float64
		copy(G[5:9], p58[b][:])
		c.prbaR58[b] = prbaToR(&G)
		copy(c.prbaQ[b][:], c.prbaR58[b][1:9])
	}
	return c
}

// Kind classifies b0.
func (c *Codebook) Kind(b0 uint16) Kind { return c.kind(b0) }

// Pitch returns w0 (radians/sample) and L for a voice (or AMBE+2 silence) b0.
func (c *Codebook) Pitch(b0 uint16) (w0 float64, L int) {
	if c.kind(b0) == Silence {
		return 2 * math.Pi / 32, 14
	}
	return 2 * math.Pi * c.f0[b0], c.nharm[b0]
}

// F0 returns the fundamental in cycles/sample for voice code b0.
func (c *Codebook) F0(b0 uint16) float64 { return c.f0[b0] }

// VoiceCodes is the number of voice b0 values (0..VoiceCodes-1).
func (c *Codebook) VoiceCodes() int { return len(c.f0) }

// SilenceCode returns the b0 an encoder sends for silence frames; ok is false
// if the codebook has none (D-STAR).
func (c *Codebook) SilenceCode() (b0 uint16, ok bool) {
	return uint16(c.silence), c.silence >= 0
}

// Voiced reports the voicing of 500 Hz band j (0..7) for b1.
func (c *Codebook) Voiced(b1 uint16, j int) bool { return c.vuv[b1][j] == 1 }
