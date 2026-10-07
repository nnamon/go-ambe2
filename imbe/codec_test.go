package imbe

import (
	"math"
	"math/rand"
	"testing"
)

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

func toPCMFrame(x []float64, k int) (pcm [FrameSamples]int16) {
	for i := range pcm {
		pcm[i] = int16(math.Max(-32768, math.Min(32767, math.Round(x[k*FrameSamples+i]))))
	}
	return pcm
}

// codec runs x through the encoder, the 144-bit frame coding and the decoder.
func codec(x []float64) ([]float64, []Params) {
	enc, dec := NewEncoder(), NewDecoder()
	var out []float64
	var ps []Params
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		pcm := toPCMFrame(x, k)
		b := enc.Encode(&pcm)
		ps = append(ps, enc.Last)
		f := b.Encode()
		y, _ := dec.DecodeFrame(&f)
		for _, v := range y {
			out = append(out, float64(v))
		}
	}
	return out, ps
}

func peakFreq(x []float64, lo, hi float64) float64 {
	best, bestF := 0.0, 0.0
	for f := lo; f <= hi; f += 0.5 {
		var re, im float64
		for i, v := range x {
			w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(len(x)-1))
			re += w * v * math.Cos(2*math.Pi*f*float64(i)/8000)
			im += w * v * math.Sin(2*math.Pi*f*float64(i)/8000)
		}
		if a := math.Hypot(re, im); a > best {
			best, bestF = a, f
		}
	}
	return bestF
}

func TestCodecRoundTripLevelAndPitch(t *testing.T) {
	for _, f0 := range []float64{90, 140, 220, 330} {
		x := harmonic(16000, f0, 6000)
		y, ps := codec(x)
		if r := rms(y[4800:]) / rms(x[4000:]); r < 0.7 || r > 1.4 {
			t.Errorf("f0=%v: output/input level %.2f", f0, r)
		}
		if f := peakFreq(y[8000:12000], f0*0.8, f0*1.2); math.Abs(f-f0) > f0*0.03 {
			t.Errorf("f0=%v: decoded fundamental at %.1f Hz", f0, f)
		}
		// The coded fundamental is within the half-sample quantization.
		w0, _, _, _ := Pitch(ps[len(ps)-1][0])
		if f := w0 * 8000 / (2 * math.Pi); math.Abs(f-f0) > f0*0.02 {
			t.Errorf("f0=%v: coded fundamental %.1f Hz", f0, f)
		}
		// Strongly periodic input is voiced in every band.
		_, _, K, _ := Pitch(ps[len(ps)-1][0])
		if b1 := ps[len(ps)-1][1]; b1 != 1<<uint(K)-1 {
			t.Errorf("f0=%v: b1 = %0*b, want all voiced", f0, K, b1)
		}
	}
}

func TestNoiseIsUnvoicedAndLevelKept(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	x := make([]float64, 24000)
	for i := range x {
		x[i] = 2000 * r.NormFloat64()
	}
	y, ps := codec(x)
	voicedBands := 0
	for _, p := range ps[10:] {
		for b := p[1]; b != 0; b &= b - 1 {
			voicedBands++
		}
	}
	if voicedBands > len(ps[10:]) {
		t.Errorf("white noise: %d voiced bands over %d frames", voicedBands, len(ps[10:]))
	}
	if r := rms(y[8000:]) / rms(x[8000:]); r < 0.5 || r > 1.5 {
		t.Errorf("noise output/input level %.2f", r)
	}
}

func TestSyncBitAlternates(t *testing.T) {
	x := harmonic(8000, 150, 5000)
	_, ps := codec(x)
	for k, p := range ps {
		_, L, _, _ := Pitch(p[0])
		if int(p[L+2]) != k%2 {
			t.Fatalf("frame %d: sync bit %d", k, p[L+2])
		}
	}
}

// TestRepeatAndMute checks §7.6-7.8: frames are decoded identically until
// errors start; badly corrupted frames are detected, and once the error rate
// ε_R (which rises as 1 − 0.95^n towards ~0.11 under heavy corruption)
// exceeds 8.75% the output is muted to comfort noise within ±5.
func TestRepeatAndMute(t *testing.T) {
	x := harmonic(24000, 160, 6000)
	enc := NewEncoder()
	var frames []Frame
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		pcm := toPCMFrame(x, k)
		b := enc.Encode(&pcm)
		frames = append(frames, b.Encode())
	}
	clean, hit := NewDecoder(), NewDecoder()
	r := rand.New(rand.NewSource(3))
	muted := -1
	for k, f := range frames {
		a, _ := clean.DecodeFrame(&f)
		g := f
		if k >= 30 {
			for i := range g {
				if r.Intn(4) == 0 {
					g[i] ^= 1
				}
			}
		}
		b, e := hit.DecodeFrame(&g)
		switch {
		case k < 30 && a != b:
			t.Fatalf("frame %d: decoders differ before any errors", k)
		case k >= 30 && e.Total() == 0:
			t.Fatalf("frame %d: corruption not detected", k)
		}
		quiet := true
		for _, v := range b {
			if v < -5 || v > 5 {
				quiet = false
			}
		}
		if hit.errRate > 0.0875 {
			if !quiet {
				t.Fatalf("frame %d: error rate %.4f but output not muted", k, hit.errRate)
			}
			if muted < 0 {
				muted = k
			}
		}
	}
	if muted < 0 || muted > 30+60 {
		t.Fatalf("muting started at frame %d (error rate %.4f at the end)", muted, hit.errRate)
	}
	t.Logf("muted from frame %d (corruption from frame 30)", muted)
}

func TestDecodeInvalidPitchRepeats(t *testing.T) {
	x := harmonic(8000, 160, 6000)
	enc, dec := NewEncoder(), NewDecoder()
	var last Bits
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		pcm := toPCMFrame(x, k)
		last = enc.Encode(&pcm)
		dec.Decode(&last)
	}
	ref := *dec
	refSyn := *dec.syn
	ref.syn = &refSyn
	var p Params
	p[0] = 220 // reserved
	bad := p.Bits()
	got := dec.Decode(&bad)
	want := ref.repeat()
	if got != want {
		t.Fatal("reserved b0 did not repeat the previous frame")
	}
}

func TestDeterministic(t *testing.T) {
	x := harmonic(8000, 130, 7000)
	a, _ := codec(x)
	b, _ := codec(x)
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("codec output not deterministic")
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	x := harmonic(FrameSamples*50, 150, 6000)
	enc := NewEncoder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pcm := toPCMFrame(x, i%50)
		enc.Encode(&pcm)
	}
}

func BenchmarkDecode(b *testing.B) {
	x := harmonic(FrameSamples*50, 150, 6000)
	enc := NewEncoder()
	var frames []Bits
	for k := 0; k < 50; k++ {
		pcm := toPCMFrame(x, k)
		frames = append(frames, enc.Encode(&pcm))
	}
	dec := NewDecoder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(&frames[i%50])
	}
}
