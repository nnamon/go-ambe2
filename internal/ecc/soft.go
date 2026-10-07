package ecc

// Soft-decision decoding.  A soft bit is a log-likelihood ratio
// llr = log(P(bit = 0) / P(bit = 1)): positive values favour 0, their
// magnitude is the confidence, and 0 means no information.  The decoders here
// are exact maximum-likelihood decoders: they search every codeword for the
// one that best agrees with the received soft bits, which these short codes
// make cheap.  Slices are indexed by codeword bit (0 = LSB).

var (
	golay23Words [4096]uint32
	hamming15    [2048]uint32
)

func init() {
	for d := range golay23Words {
		golay23Words[d] = Golay23(uint32(d))
	}
	for d := range hamming15 {
		hamming15[d] = Hamming15(uint32(d))
	}
}

// Hard returns the hard decision of n soft bits as a word.
func Hard(llr []float64) uint32 {
	var w uint32
	for i, v := range llr {
		if v < 0 {
			w |= 1 << uint(i)
		}
	}
	return w
}

// costTable holds, for each byte of a codeword, Σ llr_i over the bits set
// in every possible value of that byte, so that the cost of a codeword (Σ
// llr_i over its set bits: its log-likelihood penalty up to a constant, to be
// minimised) is three table lookups.
type costTable [3][256]float64

func (t *costTable) fill(llr []float64) {
	for b := 0; b < 3; b++ {
		t[b][0] = 0
		for v := 1; v < 256; v++ {
			low := v & -v
			i := 8*b + bitIndex(low)
			x := 0.0
			if i < len(llr) {
				x = llr[i]
			}
			t[b][v] = t[b][v&^low] + x
		}
	}
}

func bitIndex(v int) int {
	n := 0
	for v > 1 {
		v >>= 1
		n++
	}
	return n
}

func (t *costTable) cost(w uint32) float64 {
	return t[0][w&255] + t[1][w>>8&255] + t[2][w>>16&255]
}

func popcount(w uint32) int {
	n := 0
	for ; w != 0; w &= w - 1 {
		n++
	}
	return n
}

// best returns the codeword (from words, masked to n bits) of least cost and
// the number of bits in which it differs from the hard decision.
func best(words []uint32, llr []float64, extra func(uint32) uint32) (idx int, flips int) {
	var t costTable
	t.fill(llr)
	bc := 0.0
	for d, w := range words {
		if extra != nil {
			w = extra(w)
		}
		if c := t.cost(w); d == 0 || c < bc {
			idx, bc = d, c
		}
	}
	w := words[idx]
	if extra != nil {
		w = extra(w)
	}
	return idx, popcount(w ^ Hard(llr))
}

// Decode23Soft returns the 12 data bits of the most likely [23,12] Golay
// codeword given 23 soft bits, and the number of hard-decision bits it
// differs in.
func Decode23Soft(llr []float64) (data uint32, flips int) {
	d, f := best(golay23Words[:], llr[:23], nil)
	return uint32(d), f
}

// Decode24Soft is Decode23Soft for the extended [24,12] Golay code: llr[0] is
// the parity bit, llr[1..23] the [23,12] codeword (as Golay24 lays it out).
func Decode24Soft(llr []float64) (data uint32, flips int) {
	d, f := best(golay24Words[:], llr[:24], nil)
	return uint32(d), f
}

// DecodeShortSoft decodes a shortened [23,12] Golay codeword whose data has
// only its low k bits free (the others zero), from 23-(12-k) soft bits.
func DecodeShortSoft(llr []float64, k int) (data uint32, flips int) {
	d, f := best(golay23Words[:1<<uint(k)], llr[:23-(12-k)], nil)
	return uint32(d), f
}

// DecodeHamming15Soft decodes the P25 [15,11] Hamming code from 15 soft bits.
func DecodeHamming15Soft(llr []float64) (data uint32, flips int) {
	d, f := best(hamming15[:], llr[:15], nil)
	return uint32(d), f
}

// DecodeCodebookSoft decodes any code given as the table of its codewords
// (indexed by data), from n soft bits.
func DecodeCodebookSoft(words []uint32, llr []float64) (data uint32, flips int) {
	d, f := best(words, llr, nil)
	return uint32(d), f
}

// golay24Words are the extended codewords, for Decode24Soft.
var golay24Words [4096]uint32

func init() {
	for d := range golay24Words {
		golay24Words[d] = Golay24(uint32(d))
	}
}
