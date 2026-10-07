package ecc

import (
	"math/rand"
	"testing"
)

// TestGolayMatchesSpec checks Golay23 against the rows of the generator
// matrix g_G printed in TIA-102.BABA §7.3 (data bit 11 first).
func TestGolayMatchesSpec(t *testing.T) {
	rows := []uint32{
		0b11000111010, 0b01100011101, 0b11110110100, 0b01111011010,
		0b00111101101, 0b11011001100, 0b01101100110, 0b00110110011,
		0b11011100011, 0b10101001011, 0b10010011111, 0b10001110101,
	}
	for i, r := range rows {
		d := uint32(1) << uint(11-i)
		if got := Golay23(d) & 0x7FF; got != r {
			t.Errorf("row %d: parity %011b, spec %011b", i, got, r)
		}
	}
}

// TestGolayDistance checks the [23,12] code's minimum distance of 7.
func TestGolayDistance(t *testing.T) {
	for d := uint32(1); d < 4096; d++ {
		w := 0
		for v := Golay23(d); v != 0; v &= v - 1 {
			w++
		}
		if w < 7 {
			t.Fatalf("codeword of %x has weight %d", d, w)
		}
	}
}

func TestGolayCorrects(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 20000; n++ {
		d := uint32(r.Intn(4096))
		c := Golay23(d)
		k := r.Intn(4)
		e := uint32(0)
		for bits := 0; bits < k; {
			b := uint32(1) << uint(r.Intn(23))
			if e&b == 0 {
				e |= b
				bits++
			}
		}
		got, nc := Decode23(c ^ e)
		if got != d || nc != k {
			t.Fatalf("data %03x errors %06x: got %03x (%d corrected)", d, e, got, nc)
		}
	}
	for d := uint32(0); d < 4096; d++ {
		if Parity(Golay24(d)) != 0 {
			t.Fatalf("[24,12] codeword of %x has odd weight", d)
		}
	}
}

// TestHammingMatchesSpec checks Hamming15 against g_H (TIA-102.BABA §7.3)
// and that every single-bit error is corrected.
func TestHammingMatchesSpec(t *testing.T) {
	rows := []uint32{0b1111, 0b1110, 0b1101, 0b1100, 0b1011, 0b1010, 0b1001, 0b0111, 0b0110, 0b0101, 0b0011}
	for i, r := range rows {
		if got := Hamming15(1<<uint(10-i)) & 0xF; got != r {
			t.Errorf("row %d: parity %04b, spec %04b", i, got, r)
		}
	}
	for d := uint32(0); d < 2048; d++ {
		c := Hamming15(d)
		if got, n := DecodeHamming15(c); got != d || n != 0 {
			t.Fatalf("clean word %x decoded as %x (%d)", c, got, n)
		}
		for b := 0; b < 15; b++ {
			if got, n := DecodeHamming15(c ^ 1<<uint(b)); got != d || n != 1 {
				t.Fatalf("data %x bit %d: got %x (%d)", d, b, got, n)
			}
		}
	}
}

func TestPN(t *testing.T) {
	// pr(1) for u0 = 0 is 13849, so the first bit is 0; for u0 = 1 it is
	// (173*16 + 13849) = 16617 -> 0.  Check the recursion against a direct
	// evaluation for a few seeds.
	for _, u0 := range []uint32{0, 1, 0x5A5, 0xFFF} {
		p := NewPN(u0)
		pr := 16 * u0
		for n := 1; n <= 114; n++ {
			pr = (173*pr + 13849) % 65536
			if got := p.Next(); got != pr/32768 {
				t.Fatalf("u0 %x n %d: got %d", u0, n, got)
			}
		}
	}
}

// TestSoftMatchesHardWhenConfident checks that with every bit equally
// confident, soft ML decoding agrees with hard decoding within the codes'
// correction capability, and that it corrects more when the errors are the
// least confident bits.
func TestSoftMatchesHardWhenConfident(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	llr := make([]float64, 24)
	for n := 0; n < 3000; n++ {
		d := uint32(r.Intn(4096))
		c := Golay23(d)
		e := uint32(0)
		for k := r.Intn(4); k > 0; {
			b := uint32(1) << uint(r.Intn(23))
			if e&b == 0 {
				e |= b
				k--
			}
		}
		for i := 0; i < 23; i++ {
			llr[i] = 4
			if (c^e)>>uint(i)&1 == 1 {
				llr[i] = -4
			}
		}
		got, f := Decode23Soft(llr)
		if got != d || f != popcount(e) {
			t.Fatalf("golay: data %x errors %x: got %x, %d flips", d, e, got, f)
		}
		// Five errors (beyond hard decoding), each on a low-confidence bit.
		e = 0
		for k := 5; k > 0; {
			b := uint32(1) << uint(r.Intn(23))
			if e&b == 0 {
				e |= b
				k--
			}
		}
		for i := 0; i < 23; i++ {
			v := 4.0
			if e>>uint(i)&1 == 1 {
				v = 0.5
			}
			if (c^e)>>uint(i)&1 == 1 {
				v = -v
			}
			llr[i] = v
		}
		if got, _ := Decode23Soft(llr); got != d {
			t.Fatalf("golay: five weak errors not corrected (data %x)", d)
		}
		d11 := uint32(r.Intn(2048))
		h := Hamming15(d11) ^ 1<<uint(r.Intn(15))
		for i := 0; i < 15; i++ {
			llr[i] = 3
			if h>>uint(i)&1 == 1 {
				llr[i] = -3
			}
		}
		if got, f := DecodeHamming15Soft(llr); got != d11 || f != 1 {
			t.Fatalf("hamming: got %x (%d flips), want %x", got, f, d11)
		}
	}
	// Extended Golay: the parity bit takes part.
	d := uint32(0x5A5)
	w := Golay24(d)
	for i := 0; i < 24; i++ {
		llr[i] = 2
		if w>>uint(i)&1 == 1 {
			llr[i] = -2
		}
	}
	if got, f := Decode24Soft(llr); got != d || f != 0 {
		t.Fatalf("golay24: got %x %d", got, f)
	}
}
