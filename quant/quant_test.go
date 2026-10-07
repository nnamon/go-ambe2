package quant

import (
	"bufio"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/internal/codebook"
)

// TestDequantizeMatchesMbelib replays reference bitstreams through Dequantize
// and compares w0, L, gamma, voicing and every log2 M against the values
// mbelib (an independent implementation) reconstructed for the same frames.
// Golden files come from research/setup.sh (research/testdata, not committed);
// the test skips when they are absent.
func TestDequantizeMatchesMbelib(t *testing.T) {
	ambs, _ := filepath.Glob("../research/testdata/matrix/*/*.amb")
	if len(ambs) == 0 {
		t.Skip("no reference bitstreams (run research/setup.sh)")
	}
	checked := 0
	for _, amb := range ambs {
		golden := strings.TrimSuffix(amb, ".amb") + ".mbelib.params"
		gf, err := os.Open(golden)
		if err != nil {
			continue
		}
		af, err := os.Open(amb)
		if err != nil {
			t.Fatal(err)
		}
		frames, err := frame.ReadAMB(af)
		af.Close()
		if err != nil {
			t.Fatal(err)
		}
		p := NewPredictor()
		p.MbelibCompat = true // mbelib updates state on silence frames
		sc := bufio.NewScanner(gf)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		k := 0
		maxErr := 0.0
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) < 5 {
				continue
			}
			if _, err := strconv.Atoi(f[0]); err != nil {
				continue
			}
			w0, _ := strconv.ParseFloat(f[1], 64)
			L, _ := strconv.Atoi(f[2])
			gamma, _ := strconv.ParseFloat(f[3], 64)
			vl := f[4]
			// mbelib reads b3 and b4 with the draft layout.
			fb := frame.FromDraftLayout(frames[k])
			m, kind := p.Dequantize(fb.Params())
			if kind == Erasure || kind == Tone {
				t.Fatalf("%s frame %d: unexpected kind %v", amb, k, kind)
			}
			if m.L != L || math.Abs(m.W0-w0) > 1e-6 || math.Abs(m.Gamma-gamma) > 1e-4 {
				t.Fatalf("%s frame %d: got w0=%g L=%d gamma=%g want %g %d %g", amb, k, m.W0, m.L, m.Gamma, w0, L, gamma)
			}
			for l := 1; l <= L; l++ {
				if m.Voiced[l] != (vl[l-1] == '1') {
					t.Fatalf("%s frame %d: voicing differs at l=%d", amb, k, l)
				}
				want, _ := strconv.ParseFloat(f[4+l], 64)
				if d := math.Abs(m.Log2M[l] - want); d > maxErr {
					maxErr = d
				}
			}
			k++
		}
		gf.Close()
		if k != len(frames) {
			t.Fatalf("%s: compared %d of %d frames", amb, k, len(frames))
		}
		// mbelib computes in float32; allow for its rounding.
		if maxErr > 2e-4 {
			t.Errorf("%s: max |log2M diff| = %g", amb, maxErr)
		}
		checked += k
		t.Logf("%s: %d frames, max |log2M diff| %.2e", filepath.Base(filepath.Dir(amb))+"/"+filepath.Base(amb), k, maxErr)
	}
	if checked == 0 {
		t.Skip("no golden params files")
	}
}

// TestSilenceDoesNotUpdatePredictor checks the spec rule that only voice
// frames feed the predictors.
func TestSilenceDoesNotUpdatePredictor(t *testing.T) {
	p := NewPredictor()
	p.Dequantize(frame.Params{60, 0, 20, 100, 50, 3, 3, 3, 3})
	before := *p
	p.Dequantize(frame.Params{SilenceB0, 16, 31, 7, 7, 1, 1, 1, 1})
	if *p != before {
		t.Fatal("silence frame changed predictor state")
	}
	p.Dequantize(frame.Params{122, 0, 0, 0, 0, 0, 0, 0, 0}) // erasure
	p.Dequantize(frame.Params{126, 0, 0, 0, 0, 0, 0, 0, 0}) // tone
	if *p != before {
		t.Fatal("erasure/tone frame changed predictor state")
	}
}

// TestQuantizeStaysInSync checks that an independent decoder replaying the
// emitted bits reconstructs exactly the model the encoder believes it sent.
func TestQuantizeStaysInSync(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	enc, dec := NewPredictor(), NewPredictor()
	for n := 0; n < 300; n++ {
		tg := randomTarget(r)
		b, m := enc.Quantize(tg)
		bits := b.Bits()
		got, _ := dec.Dequantize(bits.Params())
		if got != m {
			t.Fatalf("frame %d: decoder model differs from encoder's", n)
		}
	}
}

func randomTarget(r *rand.Rand) *Target {
	tg := &Target{}
	if r.Intn(10) == 0 {
		tg.B0 = SilenceB0
	} else {
		tg.B0 = uint16(r.Intn(120))
	}
	_, L := PitchOf(tg.B0)
	level := 2 + 6*r.Float64()
	tilt := -0.15 * r.Float64()
	for l := 1; l <= L; l++ {
		v := level + tilt*float64(l) + 0.7*r.NormFloat64()
		tg.LogV[l], tg.LogU[l] = v, v+1
	}
	for j := range tg.Voicing {
		tg.Voicing[j] = r.Float64()
		tg.BandWeight[j] = 1
	}
	return tg
}

// trueShapeError computes the exact mean-removed squared error between the
// residual rebuilt from b and the target X (both length L).
func trueShapeError(b frame.Params, X *[MaxL + 1]float64, L int) float64 {
	T := AMBE2.residual(b, L)
	var e [MaxL + 1]float64
	mean := 0.0
	for l := 1; l <= L; l++ {
		e[l] = T[l] - X[l]
		mean += e[l]
	}
	mean /= float64(L)
	s := 0.0
	for l := 1; l <= L; l++ {
		d := e[l] - mean
		s += d * d
	}
	return s
}

// TestSearchIsOptimal checks the closed-form PRBA/HOC search against the
// directly computed reconstruction error: no random alternative may beat it.
func TestSearchIsOptimal(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for n := 0; n < 40; n++ {
		tg := randomTarget(r)
		tg.B0 = uint16(r.Intn(120))
		p := NewPredictor()
		// Give the predictor some history.
		p.Quantize(randomTarget(r))
		pre := *p
		b, _ := p.Quantize(tg)

		// Rebuild the residual target the search saw.
		_, L := PitchOf(tg.B0)
		f0 := codebook.W0[tg.B0]
		pl, _ := pre.predicted(L)
		var X [MaxL + 1]float64
		for l := 1; l <= L; l++ {
			lt := tg.LogU[l]
			if codebook.VUV[b[1]][BandOf(l, f0)] == 1 {
				lt = tg.LogV[l]
			}
			X[l] = lt - rho*pl[l]
		}
		got := trueShapeError(b, &X, L)
		for trial := 0; trial < 2000; trial++ {
			alt := b
			switch r.Intn(3) {
			case 0:
				alt[3] = uint16(r.Intn(512))
			case 1:
				alt[4] = uint16(r.Intn(128))
			default:
				i := 5 + r.Intn(4)
				alt[i] = uint16(r.Intn(1 << frame.Widths[i]))
			}
			if e := trueShapeError(alt, &X, L); e < got-1e-9 {
				t.Fatalf("frame %d: alternative %v error %g beats chosen %v error %g", n, alt, e, b, got)
			}
		}
	}
}

func BenchmarkQuantize(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	p := NewPredictor()
	tg := randomTarget(r)
	tg.B0 = 60
	for i := 0; i < b.N; i++ {
		p.Quantize(tg)
	}
}
