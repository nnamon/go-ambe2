package denoise

import (
	"math"
	"math/rand"
	"testing"
)

func power(x []float64) float64 {
	p := 0.0
	for _, v := range x {
		p += v * v
	}
	return p / float64(len(x))
}

// voiced returns n samples of a speech-like signal: harmonics of a slowly
// gliding fundamental, at a syllabic rate (4 Hz) between full level and
// silence.
func voiced(n int, level float64) []float64 {
	x := make([]float64, n)
	ph := 0.0
	for i := range x {
		f0 := 140 + 30*math.Sin(2*math.Pi*0.7*float64(i)/8000)
		ph += 2 * math.Pi * f0 / 8000
		env := math.Max(0, math.Sin(2*math.Pi*4*float64(i)/8000))
		for l := 1; float64(l)*f0 < 3500; l++ {
			x[i] += level * env * math.Sin(float64(l)*ph) / float64(l)
		}
	}
	return x
}

func noise(n int, rms float64, seed int64) []float64 {
	r := rand.New(rand.NewSource(seed))
	x := make([]float64, n)
	for i := range x {
		x[i] = rms * r.NormFloat64()
	}
	return x
}

// TestReconstruction: with a 0 dB floor nothing is suppressed, and the
// output is the input delayed by Delay samples.
func TestReconstruction(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Floor = 0
	in := noise(8000, 3000, 1)
	x := append([]float64(nil), in...)
	New(cfg).Process(x)
	for i := Delay; i < len(x); i++ {
		if math.Abs(x[i]-in[i-Delay]) > 1e-6 {
			t.Fatalf("sample %d: %v, want %v", i, x[i], in[i-Delay])
		}
	}
}

// TestSuppressesNoise: steady white noise alone is brought down once the
// estimate has formed (by about 5 dB: the default noise estimate is scaled
// down, which costs speech less, see DefaultConfig).
func TestSuppressesNoise(t *testing.T) {
	x := noise(8000*3, 300, 2)
	in := power(x[8000:])
	New(DefaultConfig()).Process(x)
	if db := 10 * math.Log10(power(x[8000:])/in); db > -4 || db < -16 {
		t.Errorf("noise changed by %.1f dB, want -4 to -16", db)
	}
}

// TestImprovesNoisySpeech: speech-like sound in white noise at 10 dB comes
// out closer to the clean sound.
func TestImprovesNoisySpeech(t *testing.T) {
	const n = 8000 * 4
	clean := voiced(n, 4000)
	nz := noise(n, math.Sqrt(power(clean)/10), 3)
	x := make([]float64, n)
	for i := range x {
		x[i] = clean[i] + nz[i]
	}
	New(DefaultConfig()).Process(x)
	errIn, errOut := 0.0, 0.0
	for i := 8000; i < n; i++ {
		errIn += nz[i] * nz[i]
		d := x[i] - clean[i-Delay]
		errOut += d * d
	}
	if gain := 10 * math.Log10(errIn/errOut); gain < 3 {
		t.Errorf("SNR improved by %.1f dB, want at least 3", gain)
	}
}

// TestTransparentWhenQuiet: speech with noise 45 dB below it is passed
// unchanged once the speech level is known (none beyond cfg.None, 35 dB).
func TestTransparentWhenQuiet(t *testing.T) {
	const n = 8000 * 4
	clean := voiced(n, 4000)
	nz := noise(n, math.Sqrt(power(clean))*math.Pow(10, -45.0/20), 4)
	x := make([]float64, n)
	in := make([]float64, n)
	for i := range x {
		x[i] = clean[i] + nz[i]
		in[i] = x[i]
	}
	New(DefaultConfig()).Process(x)
	diff := 0.0
	for i := 16000; i < n; i++ {
		d := x[i] - in[i-Delay]
		diff += d * d
	}
	if db := 10 * math.Log10(diff/float64(n-16000)/power(clean)); db > -60 {
		t.Errorf("output differs from the input by %.1f dB re the speech", db)
	}
}

// TestFlush: the signal in flight is dropped, the estimates are kept.
func TestFlush(t *testing.T) {
	s := New(DefaultConfig())
	x := noise(8000*2, 300, 5)
	s.Process(x)
	noiseBefore, speechBefore := s.noise, s.speech
	s.Flush()
	if s.noise != noiseBefore || s.speech != speechBefore {
		t.Fatal("Flush changed the noise or speech estimate")
	}
	z := make([]float64, 400)
	s.Process(z)
	for i, v := range z {
		if v != 0 {
			t.Fatalf("sample %d after Flush: %v from the previous signal", i, v)
		}
	}
}

func TestExpint(t *testing.T) {
	for _, c := range []struct{ v, want float64 }{
		{0.01, 4.037930}, {0.1, 1.822924}, {0.5, 0.559774}, {1, 0.219384}, {2, 0.048901}, {5, 0.001148296},
	} {
		if got := expint(c.v); math.Abs(got/c.want-1) > 1e-4 {
			t.Errorf("E1(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}
