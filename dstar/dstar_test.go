package dstar

import (
	"bytes"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/quant"
)

// TestLayoutMatchesMbelib parses the voice-frame bit assignments out of
// mbelib's mbe_decodeAmbe2400Parms (b0 from the top, the rest from after the
// tone-frame branch) and checks ours agrees.  mbelib reads b8 into bits 3..1
// of a 16-row table index; our b8 is those three bits.
func TestLayoutMatchesMbelib(t *testing.T) {
	d, _ := filepath.Abs("../research/refs/mbelib/ambe3600x2400.c")
	src, err := os.ReadFile(d)
	if err != nil {
		t.Skip("reference sources not present")
	}
	i := bytes.Index(src, []byte("mbe_decodeAmbe2400Parms (char"))
	fn := src[i:]
	fn = fn[:bytes.Index(fn, []byte("\nvoid"))]
	tone := bytes.Index(fn, []byte("if ((b0&0x7E) == 0x7E)"))
	voice := bytes.Index(fn, []byte("decode fundamental frequency w0 from b0 is already done"))
	if tone < 0 || voice < 0 {
		t.Fatal("could not find the voice section")
	}
	re := regexp.MustCompile(`\bb(\d)\s*\|=\s*ambe_d\[(\d+)\](?:\s*<<\s*(\d+))?\s*;`)
	got := map[int]slot{}
	add := func(part []byte) {
		for _, m := range re.FindAllSubmatch(part, -1) {
			f, _ := strconv.Atoi(string(m[1]))
			pos, _ := strconv.Atoi(string(m[2]))
			sig := 0
			if len(m[3]) > 0 {
				sig, _ = strconv.Atoi(string(m[3]))
			}
			if f == 8 {
				sig-- // mbelib's b8 is our b8 << 1
			}
			got[pos] = slot{uint8(f), uint8(sig)}
		}
	}
	add(fn[:tone])
	add(fn[voice:])
	if len(got) != 48 {
		t.Fatalf("parsed %d positions", len(got))
	}
	for pos := 0; pos < 49; pos++ {
		if pos == unused {
			if _, ok := got[pos]; ok {
				t.Errorf("mbelib uses position %d", pos)
			}
			continue
		}
		if got[pos] != layout[pos] {
			t.Errorf("pos %d: mbelib %+v, ours %+v", pos, got[pos], layout[pos])
		}
	}
}

func TestParamsRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 10000; n++ {
		var p Params
		for i, w := range Widths {
			p[i] = uint16(r.Intn(1 << uint(w)))
		}
		b := p.Bits()
		if b[unused] != 0 || b.Params() != p {
			t.Fatalf("round trip %v -> %v", p, b.Params())
		}
	}
}

func TestFrameCodingCorrects(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	var pos [4][]int
	for i, d := range dstarInterleave() {
		pos[d[0]] = append(pos[d[0]], i)
	}
	for n := 0; n < 20000; n++ {
		var b Bits
		for i := range b {
			b[i] = uint8(r.Intn(2))
		}
		b[unused] = 0 // carries c1's parity in the frame; decoded as 0
		f := b.Encode()
		if Unpack(f.Pack()) != f {
			t.Fatal("pack round trip")
		}
		var want Errors
		for v := 0; v < 2; v++ {
			cand := pos[v]
			if v == 0 {
				cand = cand[:0:0]
				for _, i := range pos[0] {
					if dstarInterleave()[i][1] != 0 { // not c0's parity bit
						cand = append(cand, i)
					}
				}
			}
			k := r.Intn(4)
			for _, j := range r.Perm(len(cand))[:k] {
				f[cand[j]] ^= 1
			}
			if v == 0 {
				want.C0 = k
			} else {
				want.C1 = k
			}
		}
		got, e := f.Decode()
		if got != b || e != want {
			t.Fatalf("iteration %d: errors %+v want %+v, bits equal %v", n, e, want, got == b)
		}
	}
}

func TestDMBFile(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	frames := make([]Bits, 7)
	for i := range frames {
		for j := range frames[i] {
			frames[i][j] = uint8(r.Intn(2))
		}
	}
	var buf bytes.Buffer
	if err := WriteDMB(&buf, frames); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDMB(&buf)
	if err != nil || len(got) != len(frames) {
		t.Fatal(err)
	}
	for i := range got {
		if got[i] != frames[i] {
			t.Fatalf("frame %d differs", i)
		}
	}
}

func harmonic(n int, f0, level float64) []float64 {
	x := make([]float64, n)
	peak := 0.0
	for i := range x {
		s := 0.0
		for l := 1; float64(l)*f0 < 3600; l++ {
			s += math.Cos(2*math.Pi*f0*float64(l*i)/8000+0.3*float64(l*l)) / float64(l)
		}
		x[i] = s
		peak = math.Max(peak, math.Abs(s))
	}
	for i := range x {
		x[i] *= level / peak
	}
	return x
}

func rms(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		s += v * v
	}
	return math.Sqrt(s / float64(len(x)))
}

func TestCodecRoundTrip(t *testing.T) {
	for _, f0 := range []float64{100, 160, 250} {
		x := harmonic(16000, f0, 6000)
		enc, dec := NewEncoder(), NewDecoder()
		var y []float64
		for k := 0; (k+1)*FrameSamples <= len(x); k++ {
			var pcm [FrameSamples]int16
			for i := range pcm {
				pcm[i] = int16(math.Round(x[k*FrameSamples+i]))
			}
			b := enc.Encode(&pcm)
			f := b.Encode()
			out, e := dec.DecodeFrame(&f)
			if e.Total() != 0 {
				t.Fatal("clean frame reported errors")
			}
			for _, v := range out {
				y = append(y, float64(v))
			}
		}
		if r := rms(y[4800:]) / rms(x[4000:]); r < 0.7 || r > 1.4 {
			t.Errorf("f0=%v: output/input level %.2f", f0, r)
		}
		// Peak of the output spectrum near f0.
		best, bestF := 0.0, 0.0
		seg := y[8000:12000]
		for f := f0 * 0.8; f <= f0*1.2; f += 0.5 {
			var re, im float64
			for i, v := range seg {
				w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(len(seg)-1))
				re += w * v * math.Cos(2*math.Pi*f*float64(i)/8000)
				im += w * v * math.Sin(2*math.Pi*f*float64(i)/8000)
			}
			if a := math.Hypot(re, im); a > best {
				best, bestF = a, f
			}
		}
		if math.Abs(bestF-f0) > 0.03*f0 {
			t.Errorf("f0=%v: decoded fundamental at %.1f Hz", f0, bestF)
		}
	}
}

// TestNullAMBE decodes the "null AMBE" frame D-STAR gateways send for
// silence (MMDVMHost's DSTAR_NULL_AMBE_DATA_BYTES): a real frame, which must
// decode without errors, with both Golay parity bits consistent, as a
// silence frame (b0 = 124).
func TestNullAMBE(t *testing.T) {
	f := Unpack([9]byte{0x9E, 0x8D, 0x32, 0x88, 0x26, 0x1A, 0x3F, 0x61, 0xE8})
	b, e := f.Decode()
	if e != (Errors{}) {
		t.Fatalf("errors %+v", e)
	}
	p := b.Params()
	if p[0] != 124 || p[1] != 0 {
		t.Fatalf("params %v", p)
	}
	if g := b.Encode(); g != f {
		t.Fatal("re-encoding does not reproduce the frame")
	}
	if out := NewDecoder().Decode(&b); out == ([FrameSamples]int16{}) {
		t.Log("silence frame decoded to digital silence")
	}
}

// TestDVMatchesMbelibNeo compares the 9-byte voice data with mbelib-neo's
// frame encoder (mbe_encodeAmbe3600x2400Frame + mbe_encodeDStarDVData) on
// reference frames from research/setup.sh.
func TestDVMatchesMbelibNeo(t *testing.T) {
	f, err := os.Open("../research/testdata/dstar/frames.dmb")
	if err != nil {
		t.Skip("no reference data (run research/setup.sh)")
	}
	frames, err := ReadDMB(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../research/testdata/dstar/frames.neo.dv")
	if err != nil {
		t.Skip(err)
	}
	if len(want) != 9*len(frames) {
		t.Fatalf("%d frames, %d bytes of reference", len(frames), len(want))
	}
	for i := range frames {
		fr := frames[i].Encode()
		if p := fr.Pack(); !bytes.Equal(p[:], want[9*i:9*i+9]) {
			t.Fatalf("frame %d: ours % x, mbelib-neo % x", i, p, want[9*i:9*i+9])
		}
	}
	t.Logf("%d frames identical", len(frames))
}

// TestDequantizeMatchesMbelib replays reference streams from this encoder
// through the D-STAR parameter reconstruction and compares w0, L, voicing and
// every log2 M_l with mbelib's (research/testdata/dstar, from setup.sh).
func TestDequantizeMatchesMbelib(t *testing.T) {
	paths, _ := filepath.Glob("../research/testdata/dstar/*.godstar.dmb")
	if len(paths) == 0 {
		t.Skip("no reference streams (run research/setup.sh)")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(strings.TrimSuffix(path, ".dmb") + ".mbelib.params")
		if err != nil {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		frames, err := ReadDMB(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		p := quant.NewPredictorFor(quant.DStar)
		k, maxErr := 0, 0.0
		for _, line := range strings.Split(string(raw), "\n") {
			fl := strings.Fields(line)
			if len(fl) < 5 {
				continue
			}
			if _, err := strconv.Atoi(fl[0]); err != nil {
				continue
			}
			w0, _ := strconv.ParseFloat(fl[1], 64)
			L, _ := strconv.Atoi(fl[2])
			gamma, _ := strconv.ParseFloat(fl[3], 64)
			m, kind := p.Dequantize(frame.Params(frames[k].Params()))
			if kind != quant.Voice {
				t.Fatalf("%s frame %d: kind %v", path, k, kind)
			}
			if m.L != L || math.Abs(m.W0-w0) > 1e-6 || math.Abs(m.Gamma-gamma) > 1e-4 {
				t.Fatalf("%s frame %d: w0=%g L=%d gamma=%g, mbelib %g %d %g", path, k, m.W0, m.L, m.Gamma, w0, L, gamma)
			}
			for l := 1; l <= L; l++ {
				if m.Voiced[l] != (fl[4][l-1] == '1') {
					t.Fatalf("%s frame %d: voicing differs at l=%d", path, k, l)
				}
				want, _ := strconv.ParseFloat(fl[4+l], 64)
				if d := math.Abs(m.Log2M[l] - want); d > maxErr {
					maxErr = d
				}
			}
			k++
		}
		if k != len(frames) {
			t.Fatalf("%s: compared %d of %d frames", path, k, len(frames))
		}
		if maxErr > 2e-4 {
			t.Errorf("%s: max |log2 M diff| = %g", path, maxErr)
		}
		t.Logf("%s: %d frames, max |log2 M diff| %.2e", filepath.Base(path), k, maxErr)
	}
}
