package p25full

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nnamon/mbevoc/internal/codebook"
)

// TestDequantizeMatchesMbelib replays reference IMBE streams (from this
// module's and OP25's encoders) through the decoder's parameter
// reconstruction and compares w0, L, voicing and every log2 M_l with mbelib's
// (an independent implementation).  mbelib's Annex F step for G5 at L = 23
// is 0.068 instead of the specification's 0.058 (see internal/codebook); the
// comparison uses mbelib's value so that the rest can be checked exactly.
// Reference files come from research/setup.sh; the test skips without them.
func TestDequantizeMatchesMbelib(t *testing.T) {
	ims, _ := filepath.Glob("../research/testdata/imbe/*.imb")
	if len(ims) == 0 {
		t.Skip("no reference streams (run research/setup.sh)")
	}
	saved := codebook.IMBEGainStep[23][3]
	codebook.IMBEGainStep[23][3] = 0.068
	defer func() { codebook.IMBEGainStep[23][3] = saved }()

	checked := 0
	for _, path := range ims {
		gf, err := os.Open(strings.TrimSuffix(path, ".imb") + ".mbelib.params")
		if err != nil {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		frames, err := ReadIMB(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		p := newPredictor()
		sc := bufio.NewScanner(gf)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		k, maxErr := 0, 0.0
		for sc.Scan() {
			fl := strings.Fields(sc.Text())
			if len(fl) < 5 {
				continue
			}
			if _, err := strconv.Atoi(fl[0]); err != nil {
				continue
			}
			w0, _ := strconv.ParseFloat(fl[1], 64)
			L, _ := strconv.Atoi(fl[2])
			vl := fl[4]
			par, ok := frames[k].Params()
			if !ok {
				t.Fatalf("%s frame %d: reserved b0", path, k)
			}
			m := p.dequantize(&par)
			if m.L != L || math.Abs(m.w0-w0) > 1e-6 {
				t.Fatalf("%s frame %d: w0=%g L=%d, mbelib %g %d", path, k, m.w0, m.L, w0, L)
			}
			for l := 1; l <= L; l++ {
				if m.voiced[l] != (vl[l-1] == '1') {
					t.Fatalf("%s frame %d: voicing differs at l=%d", path, k, l)
				}
				want, _ := strconv.ParseFloat(fl[4+l], 64)
				if d := math.Abs(m.log2M[l] - want); d > maxErr {
					maxErr = d
				}
			}
			k++
		}
		gf.Close()
		if k != len(frames) {
			t.Fatalf("%s: compared %d of %d frames", path, k, len(frames))
		}
		if maxErr > 1e-3 {
			t.Errorf("%s: max |log2 M diff| = %g", path, maxErr)
		}
		checked += k
		t.Logf("%s: %d frames, max |log2 M diff| %.2e", filepath.Base(path), k, maxErr)
	}
	if checked == 0 {
		t.Skip("no reference parameter files")
	}
}
