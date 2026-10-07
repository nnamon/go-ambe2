package imbe

import (
	"math/rand"
	"os"
	"testing"

	"github.com/nnamon/go-ambe2/internal/codebook"
)

func TestProVoiceRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	lens := [7]int{19, 24, 23, 23, 15, 15, 23}
	var pos [7][]int
	for n, v := range codebook.ProVoiceOrder {
		pos[v[0]] = append(pos[v[0]], n)
	}
	for v, l := range lens {
		if len(pos[v]) != l {
			t.Fatalf("vector %d has %d bits", v, len(pos[v]))
		}
	}
	for n := 0; n < 5000; n++ {
		p := randomParams(r)
		b := p.Bits()
		f := b.EncodeProVoice()
		if UnpackProVoice(f.Pack()) != f {
			t.Fatal("pack round trip")
		}
		var want Errors
		for v := 0; v < 6; v++ {
			max := 3
			if v >= 4 {
				max = 1
			}
			// Errors only in the bits the codes cover (not c0's and c1's parity bits).
			cand := []int{}
			for _, i := range pos[v] {
				if (v == 0 || v == 1) && codebook.ProVoiceOrder[i][1] == 0 {
					continue
				}
				cand = append(cand, i)
			}
			k := r.Intn(max + 1)
			for _, j := range r.Perm(len(cand))[:k] {
				f[cand[j]] ^= 1
			}
			want.E[v] = k
		}
		got, e := f.Decode()
		if got != b || e != want {
			t.Fatalf("iteration %d: errors %v want %v, bits equal %v", n, e, want, got == b)
		}
	}
}

func readFile18(t *testing.T, path string) [][18]byte {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no reference data (run research/setup.sh)")
	}
	out := make([][18]byte, len(raw)/18)
	for i := range out {
		copy(out[i][:], raw[18*i:])
	}
	return out
}

func readIMBFile(t *testing.T, path string) []Bits {
	f, err := os.Open(path)
	if err != nil {
		t.Skip("no reference data (run research/setup.sh)")
	}
	defer f.Close()
	b, err := ReadIMB(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDecodeMatchesMbelibNeo decodes random 144-bit P25 and 142-bit ProVoice
// frames, which exercises error correction at every error weight, and
// compares the 88 bits with mbelib-neo's decoders (its Hamming decoding is
// correct; upstream mbelib's miscorrects single errors in bits 7..3 of the
// P25 Hamming words).  Reference data from research/setup.sh.
func TestDecodeMatchesMbelibNeo(t *testing.T) {
	for _, c := range []struct {
		name, in, ref string
		pv            bool
	}{
		{"P25", "../research/testdata/imbe/random.imbe144", "../research/testdata/imbe/random.imbe144.neo.imb", false},
		{"ProVoice", "../research/testdata/provoice/random.pv", "../research/testdata/provoice/random.neo.imb", true},
	} {
		frames := readFile18(t, c.in)
		want := readIMBFile(t, c.ref)
		if len(want) != len(frames) {
			t.Fatalf("%s: %d frames, %d reference", c.name, len(frames), len(want))
		}
		for i, p := range frames {
			var got Bits
			if c.pv {
				f := UnpackProVoice(p)
				got, _ = f.Decode()
			} else {
				f := Unpack(p)
				got, _ = f.Decode()
			}
			if got != want[i] {
				t.Fatalf("%s frame %d: decoded bits differ from mbelib-neo's", c.name, i)
			}
		}
		t.Logf("%s: %d random frames decode as mbelib-neo decodes them", c.name, len(frames))
	}
}
