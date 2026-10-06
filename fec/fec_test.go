package fec

import (
	"bufio"
	"encoding/hex"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/nnamon/go-ambe2/frame"
)

// bits49 parses a 49-bit value given as 13 hex digits, left-aligned (the last
// three bits are padding), as printed in NXDN TS 1-A §7.2.1.
func bits49(t *testing.T, h string) frame.Bits {
	raw, err := hex.DecodeString(h + "0")
	if err != nil {
		t.Fatal(err)
	}
	var b frame.Bits
	for i := range b {
		b[i] = (raw[i/8] >> (7 - uint(i%8))) & 1
	}
	return b
}

func hex72(t *testing.T, h string) Bits72 {
	raw, err := hex.DecodeString(h)
	if err != nil || len(raw) != 9 {
		t.Fatalf("bad vector %q", h)
	}
	var a [9]byte
	copy(a[:], raw)
	return Unpack(a)
}

// TestPublishedVectors checks the 49->72 bit mapping against the silence and
// tone frames published in NXDN TS 1-A v1.3 §7.2.1.  The silence frame also
// appears as DMR_SILENCE_DATA in MMDVMHost (DMR), confirming the DMR layout.
func TestPublishedVectors(t *testing.T) {
	for _, v := range []struct{ name, in49, out72 string }{
		{"silence", "F801A99F8CE08", "B9E881526173002A6B"},
		{"tone", "FEE21212121" + "00", "CEA8FE83ACC458200A"},
	} {
		in := bits49(t, v.in49)
		got := Encode(&in)
		want := hex72(t, v.out72)
		if got != want {
			p := Pack(&got)
			t.Errorf("%s: Encode = %X, want %s", v.name, p, v.out72)
		}
		dec, e := Decode(&want)
		if dec != in || e.Total() != 0 || e.C0Parity {
			t.Errorf("%s: Decode = %v (%+v), want %v", v.name, dec, e, in)
		}
	}
	// The silence vector is a b0 = 124 frame.
	s := bits49(t, "F801A99F8CE08")
	if p := s.Params(); p[0] != 124 {
		t.Errorf("silence vector b0 = %d", p[0])
	}
}

func TestGolay(t *testing.T) {
	for d := uint32(0); d < 4096; d++ {
		c := golay23(d)
		if syndrome(c) != 0 {
			t.Fatalf("codeword %x has nonzero syndrome", c)
		}
		if parity(golay24(d)) != 0 {
			t.Fatalf("[24,12] codeword %x has odd weight", golay24(d))
		}
	}
	// Minimum distance 7: every nonzero codeword has weight >= 7.
	for d := uint32(1); d < 4096; d++ {
		w := 0
		for v := golay23(d); v != 0; v &= v - 1 {
			w++
		}
		if w < 7 {
			t.Fatalf("codeword weight %d < 7", w)
		}
	}
}

func TestRoundTripAndCorrection(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	// positions of c0 and c1 bits in the interleaved frame
	var pos [2][]int
	for s, d := range interleave {
		for k := 0; k < 2; k++ {
			if v := d[k][0]; v < 2 {
				pos[v] = append(pos[v], 2*s+k)
			}
		}
	}
	for n := 0; n < 20000; n++ {
		var b frame.Bits
		for i := range b {
			b[i] = uint8(r.Intn(2))
		}
		c := Encode(&b)
		// Up to three errors in c0 (excluding its parity bit) and three in c1.
		for v := 0; v < 2; v++ {
			k := r.Intn(4)
			perm := r.Perm(len(pos[v]))
			for _, i := range perm[:k] {
				c[pos[v][i]] ^= 1
			}
		}
		got, e := Decode(&c)
		if got != b {
			t.Fatalf("iteration %d: decode mismatch (errors %+v)", n, e)
		}
	}
}

// TestMatchesMbelib decodes externally produced reference data: 72-bit frames
// from research/tools/fec_xcheck (mbelib's demodulation and Golay decoding, with DSD's
// DMR deinterleave tables) alongside the 49 bits mbelib recovered.
func TestMatchesMbelib(t *testing.T) {
	f, err := os.Open("../research/testdata/fec/mbelib_xcheck.txt")
	if err != nil {
		t.Skip("no cross-check data:", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) != 2 {
			continue
		}
		in := hex72(t, fs[0])
		want, err := frame.ParseBits(fs[1])
		if err != nil {
			t.Fatal(err)
		}
		got, _ := Decode(&in)
		if got != want {
			t.Fatalf("line %d: got %v want %v", n+1, got, want)
		}
		n++
	}
	if n == 0 {
		t.Fatal("empty cross-check file")
	}
	t.Logf("%d frames agree with mbelib", n)
}
