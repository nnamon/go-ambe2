package imbe

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nnamon/go-ambe2/internal/codebook"
)

// TestEncodingExample checks the bit allocation and prioritization against
// the parameter encoding example of TIA-102.BABA chapter 10 (L = 16,
// Tables 6-10).  The example has three misprints, corrected here: its
// fundamental, 2π/35.125, gives L = 15 by eq. 31 (L = 16 needs b0 = 32..35);
// and in Table 10, û7 bit 6 is bit 0 (not bit 1) of b16 and û7 bit 0 is
// bit 0 of b18, as the scanning procedure of §7.1 and every other entry imply.
func TestEncodingExample(t *testing.T) {
	const L = 16
	for b0 := uint16(32); b0 <= 35; b0++ {
		if _, l, k, _ := Pitch(b0); l != L || k != 6 {
			t.Fatalf("b0 = %d gives L=%d K=%d, want 16 and 6", b0, l, k)
		}
	}
	// Tables 6 and 7: bits and step sizes.
	wantGain := [5]struct {
		B    int
		step float64
	}{{6, .04650}, {6, .03015}, {6, .02520}, {5, .04060}, {5, .03696}}
	for i, w := range wantGain {
		if codebook.IMBEGainBits[L][i] != w.B || codebook.IMBEGainStep[L][i] != w.step {
			t.Errorf("G%d: %d bits step %v, want %d %v", i+2, codebook.IMBEGainBits[L][i], codebook.IMBEGainStep[L][i], w.B, w.step)
		}
	}
	wantHOC := []struct {
		B, k int
		step float64
	}{{6, 2, .04605}, {6, 2, .04605}, {5, 2, .08596}, {4, 3, .09640}, {4, 2, .12280},
		{3, 3, .15665}, {3, 2, .19955}, {3, 3, .15665}, {3, 2, .19955}, {2, 3, .20485}}
	for i, w := range wantHOC {
		B := codebook.IMBEHOCBits[L][i]
		if s := hocStep(B, w.k); B != w.B || s < w.step-5e-6 || s > w.step+5e-6 {
			t.Errorf("b%d: %d bits step %.5f, want %d %.5f", i+8, B, s, w.B, w.step)
		}
	}
	// Tables 8-10: (vector, bit number, quantizer value, bit number), 1-based bit numbers.
	tab := [][4]int{
		{0, 12, 0, 8}, {0, 11, 0, 7}, {0, 10, 0, 6}, {0, 9, 0, 5}, {0, 8, 0, 4}, {0, 7, 0, 3},
		{0, 6, 2, 6}, {0, 5, 2, 5}, {0, 4, 2, 4}, {0, 3, 3, 6}, {0, 2, 4, 6}, {0, 1, 5, 6},
		{1, 12, 8, 6}, {1, 11, 9, 6}, {1, 10, 3, 5}, {1, 9, 4, 5}, {1, 8, 5, 5}, {1, 7, 6, 5},
		{1, 6, 7, 5}, {1, 5, 8, 5}, {1, 4, 9, 5}, {1, 3, 10, 5}, {1, 2, 3, 4}, {1, 1, 4, 4},
		{2, 12, 5, 4}, {2, 11, 6, 4}, {2, 10, 7, 4}, {2, 9, 8, 4}, {2, 8, 9, 4}, {2, 7, 10, 4},
		{2, 6, 11, 4}, {2, 5, 12, 4}, {2, 4, 3, 3}, {2, 3, 4, 3}, {2, 2, 5, 3}, {2, 1, 6, 3},
		{3, 12, 7, 3}, {3, 11, 8, 3}, {3, 10, 9, 3}, {3, 9, 10, 3}, {3, 8, 11, 3}, {3, 7, 12, 3},
		{3, 6, 13, 3}, {3, 5, 14, 3}, {3, 4, 15, 3}, {3, 3, 16, 3}, {3, 2, 3, 2}, {3, 1, 4, 2},
		{4, 11, 1, 6}, {4, 10, 1, 5}, {4, 9, 1, 4}, {4, 8, 1, 3}, {4, 7, 1, 2}, {4, 6, 1, 1},
		{4, 5, 2, 3}, {4, 4, 2, 2}, {4, 3, 5, 2}, {4, 2, 6, 2}, {4, 1, 7, 2},
		{5, 11, 8, 2}, {5, 10, 9, 2}, {5, 9, 10, 2}, {5, 8, 11, 2}, {5, 7, 12, 2}, {5, 6, 13, 2},
		{5, 5, 14, 2}, {5, 4, 15, 2}, {5, 3, 16, 2}, {5, 2, 17, 2}, {5, 1, 3, 1},
		{6, 11, 4, 1}, {6, 10, 5, 1}, {6, 9, 6, 1}, {6, 8, 7, 1}, {6, 7, 8, 1}, {6, 6, 9, 1},
		{6, 5, 10, 1}, {6, 4, 11, 1}, {6, 3, 12, 1}, {6, 2, 13, 1}, {6, 1, 14, 1},
		{7, 7, 15, 1}, {7, 6, 16, 1}, {7, 5, 17, 1}, {7, 4, 2, 1}, {7, 3, 0, 2}, {7, 2, 0, 1}, {7, 1, 18, 1},
	}
	if len(tab) != 88 {
		t.Fatalf("table has %d entries", len(tab))
	}
	start := [8]int{0, 12, 24, 36, 48, 59, 70, 81}
	for _, r := range tab {
		pos := start[r[0]] + vecLen[r[0]] - r[1]
		want := slot{uint8(r[2]), uint8(r[3] - 1)}
		if got := layouts[L][pos]; got != want {
			t.Errorf("u%d bit %d: got b%d bit %d, want b%d bit %d", r[0], r[1]-1, got.m, got.bit, want.m, want.bit)
		}
	}
}

func refs(t *testing.T) string {
	d, _ := filepath.Abs("../research/refs")
	if _, err := os.Stat(d); err != nil {
		t.Skip("reference sources not present at", d)
	}
	return d
}

func intArray(t *testing.T, src []byte, name string) []int {
	t.Helper()
	i := bytes.Index(src, []byte(name))
	if i < 0 {
		t.Fatalf("%s not found", name)
	}
	body := src[i:]
	body = body[bytes.IndexByte(body, '{'):]
	body = body[:bytes.Index(body, []byte("};"))]
	body = regexp.MustCompile(`//[^\n]*`).ReplaceAll(body, nil)
	var out []int
	for _, m := range regexp.MustCompile(`-?\d+`).FindAll(body, -1) {
		v, _ := strconv.Atoi(string(m))
		out = append(out, v)
	}
	return out
}

// TestLayoutMatchesMbelib compares the bit prioritization with mbelib's
// precomputed table bo[L-9][i] = (quantizer value, bit) for imbe_d[6+i].
func TestLayoutMatchesMbelib(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(refs(t), "mbelib/imbe7200x4400_const.h"))
	if err != nil {
		t.Skip(err)
	}
	bo := intArray(t, src, "bo[48][79][2]")
	if len(bo) != 48*79*2 {
		t.Fatalf("parsed %d entries", len(bo))
	}
	for L := 9; L <= MaxL; L++ {
		for i := 0; i < 79; i++ {
			k := ((L-9)*79 + i) * 2
			want := slot{uint8(bo[k]), uint8(bo[k+1])}
			if got := layouts[L][6+i]; got != want {
				t.Fatalf("L=%d imbe_d[%d]: ours b%d bit %d, mbelib b%d bit %d", L, 6+i, got.m, got.bit, want.m, want.bit)
			}
		}
	}
}

// TestInterleaveMatchesDSD compares Annex H with DSD's P25 Phase 1
// de-interleave tables (iW, iX for dibit bit 1; iY, iZ for bit 0).
func TestInterleaveMatchesDSD(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(refs(t), "dsd-fme/include/p25p1_const.h"))
	if err != nil {
		t.Skip(err)
	}
	w, x := intArray(t, src, "iW[72] ="), intArray(t, src, "iX[72] =")
	y, z := intArray(t, src, "iY[72] ="), intArray(t, src, "iZ[72] =")
	for s, d := range codebook.IMBEInterleave {
		if d[0] != [2]int{w[s], x[s]} || d[1] != [2]int{y[s], z[s]} {
			t.Errorf("symbol %d: ours %v, DSD (%d,%d) (%d,%d)", s, d, w[s], x[s], y[s], z[s])
		}
	}
}

func randomParams(r *rand.Rand) Params {
	var p Params
	p[0] = uint16(r.Intn(MaxB0 + 1))
	_, L, K, _ := Pitch(p[0])
	B := widths(L)
	B[1], B[2] = K, 6
	for m := 1; m <= L+1; m++ {
		p[m] = uint16(r.Intn(1 << uint(B[m])))
	}
	p[L+2] = uint16(r.Intn(2))
	return p
}

func TestParamsRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 20000; n++ {
		p := randomParams(r)
		b := p.Bits()
		got, ok := b.Params()
		if !ok || got != p {
			t.Fatalf("round trip: %v -> %v (%v)", p[:8], got[:8], ok)
		}
	}
	var p Params
	p[0] = 216
	b := p.Bits()
	if got, ok := b.Params(); ok || got[0] != 216 {
		t.Fatalf("reserved b0: %v %v", got[0], ok)
	}
}

func TestFrameCodingCorrects(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	lens := [8]int{23, 23, 23, 23, 15, 15, 15, 7}
	// Frame positions of each code vector's bits.
	var pos [8][]int
	for s, d := range codebook.IMBEInterleave {
		pos[d[0][0]] = append(pos[d[0][0]], 2*s)
		pos[d[1][0]] = append(pos[d[1][0]], 2*s+1)
	}
	for i, n := range lens {
		if len(pos[i]) != n {
			t.Fatalf("vector %d has %d frame bits", i, len(pos[i]))
		}
	}
	for n := 0; n < 5000; n++ {
		var b Bits
		for i := range b {
			b[i] = uint8(r.Intn(2))
		}
		f := b.Encode()
		var want Errors
		for v := 0; v < 7; v++ {
			max := 3
			if v >= 4 {
				max = 1
			}
			k := r.Intn(max + 1)
			for _, i := range r.Perm(len(pos[v]))[:k] {
				f[pos[v][i]] ^= 1
			}
			want.E[v] = k
		}
		got, e := f.Decode()
		if got != b || e != want {
			t.Fatalf("iteration %d: decode mismatch, errors %v want %v", n, e, want)
		}
		p := f.Pack()
		if Unpack(p) != f {
			t.Fatal("pack round trip")
		}
	}
}

func TestIMBFile(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	frames := make([]Bits, 10)
	for i := range frames {
		for j := range frames[i] {
			frames[i][j] = uint8(r.Intn(2))
		}
	}
	var buf bytes.Buffer
	if err := WriteIMB(&buf, frames); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 4+12*len(frames) {
		t.Fatalf("size %d", buf.Len())
	}
	got, err := ReadIMB(&buf)
	if err != nil || len(got) != len(frames) {
		t.Fatal(err, len(got))
	}
	for i := range got {
		if got[i] != frames[i] {
			t.Fatalf("frame %d differs", i)
		}
	}
}

// TestCodeVectorsMatchOP25 compares the Golay/Hamming coding and bit
// modulation (before interleaving) with OP25's imbe_header_encode on random
// frames (research/testdata/imbe, from research/setup.sh).
func TestCodeVectorsMatchOP25(t *testing.T) {
	f, err := os.Open("../research/testdata/imbe/random.imb")
	if err != nil {
		t.Skip("no reference data (run research/setup.sh)")
	}
	frames, err := ReadIMB(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../research/testdata/imbe/random.op25cw.txt")
	if err != nil {
		t.Skip(err)
	}
	lines := strings.Fields(string(want))
	if len(lines) != len(frames) {
		t.Fatalf("%d frames, %d reference lines", len(frames), len(lines))
	}
	lens := [8]int{23, 23, 23, 23, 15, 15, 15, 7}
	for i := range frames {
		c := frames[i].codeVectors()
		var sb strings.Builder
		var acc, n uint32
		for v, l := range lens {
			for j := l - 1; j >= 0; j-- {
				acc = acc<<1 | c[v]>>uint(j)&1
				if n++; n == 4 {
					sb.WriteString(strconv.FormatUint(uint64(acc), 16))
					acc, n = 0, 0
				}
			}
		}
		if sb.String() != lines[i] {
			t.Fatalf("frame %d: ours %s, OP25 %s", i, sb.String(), lines[i])
		}
	}
	t.Logf("%d frames match OP25", len(frames))
}
