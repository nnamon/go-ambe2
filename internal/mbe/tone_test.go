package mbe

import (
	"math"
	"math/rand"
	"testing"
)

// sines returns ToneWindow samples of a sum of sinusoids (frequency Hz, peak).
func sines(c ...[2]float64) []float64 {
	x := make([]float64, ToneWindow)
	for i, s := range c {
		for n := range x {
			x[n] += s[1] * math.Sin(2*math.Pi*s[0]*float64(n)/8000+0.9*float64(i))
		}
	}
	return x
}

func TestToneDetectorSingle(t *testing.T) {
	d := NewToneDetector()
	for f := 160.0; f < 3830; f += 97.3 {
		for _, a := range []float64{500, 8000, 30000} {
			tn, ok := d.Detect(sines([2]float64{f, a}))
			if !ok || tn.F2 != 0 {
				t.Fatalf("%.1f Hz at %v: detected %v, %+v", f, a, ok, tn)
			}
			if math.Abs(tn.F1-f) > 1 || math.Abs(20*math.Log10(tn.A1/a)) > 0.3 {
				t.Errorf("%.1f Hz at %v: estimated %.2f Hz at %.0f", f, a, tn.F1, tn.A1)
			}
		}
	}
}

// pairs are TIA-102.BABA-1 Table 9's dual tones, without 480/440 Hz (too
// close together to separate in ToneWindow samples).
var pairs = [][2]float64{
	{1336, 941}, {1209, 697}, {1336, 697}, {1477, 697}, {1209, 770}, {1336, 770}, {1477, 770}, {1209, 852},
	{1336, 852}, {1477, 852}, {1633, 697}, {1633, 770}, {1633, 852}, {1633, 941}, {1209, 941}, {1477, 941},
	{1162, 820}, {1052, 606}, {1162, 606}, {1279, 606}, {1052, 672}, {1162, 672}, {1279, 672}, {1052, 743},
	{1162, 743}, {1279, 743}, {1430, 606}, {1430, 672}, {1430, 743}, {1430, 820}, {1052, 820}, {1279, 820},
	{440, 350}, {620, 480}, {490, 350},
}

func TestToneDetectorDual(t *testing.T) {
	d := NewToneDetector()
	for _, p := range pairs {
		for _, twist := range []float64{-8, 0, 8} {
			a := 6000 * math.Pow(10, twist/20)
			tn, ok := d.Detect(sines([2]float64{p[0], a}, [2]float64{p[1], 6000}))
			if !ok || tn.F2 == 0 {
				t.Fatalf("%v Hz, twist %v dB: detected %v, %+v", p, twist, ok, tn)
			}
			hi, lo := math.Max(tn.F1, tn.F2), math.Min(tn.F1, tn.F2)
			if math.Abs(hi/p[0]-1) > 0.005 || math.Abs(lo/p[1]-1) > 0.005 {
				t.Errorf("%v Hz: estimated %.1f/%.1f Hz", p, hi, lo)
			}
		}
	}
}

func TestToneDetectorNoise(t *testing.T) {
	d := NewToneDetector()
	r := rand.New(rand.NewSource(1))
	noisy := func(x []float64, snr float64) []float64 {
		p := 0.0
		for _, v := range x {
			p += v * v
		}
		sd := math.Sqrt(p / float64(len(x)) / math.Pow(10, snr/10))
		for i := range x {
			x[i] += sd * r.NormFloat64()
		}
		return x
	}
	for i := 0; i < 20; i++ {
		if _, ok := d.Detect(noisy(sines([2]float64{1000, 8000}), 30)); !ok {
			t.Fatal("1000 Hz at 30 dB SNR not detected")
		}
		if _, ok := d.Detect(noisy(sines([2]float64{1336, 6000}, [2]float64{770, 6000}), 20)); !ok {
			t.Fatal("DTMF at 20 dB SNR not detected")
		}
	}
}

// TestToneDetectorClosePair checks that 480 + 440 Hz (closer than a lobe
// width) never passes as a single tone, whatever the phase and twist.
func TestToneDetectorClosePair(t *testing.T) {
	d := NewToneDetector()
	for twist := -10.0; twist <= 10; twist += 2 {
		for ph := 0.0; ph < 2*math.Pi; ph += 0.2 {
			x := make([]float64, ToneWindow)
			for n := range x {
				x[n] = 6000*math.Pow(10, twist/20)*math.Sin(2*math.Pi*480*float64(n)/8000+ph) + 6000*math.Sin(2*math.Pi*440*float64(n)/8000)
			}
			if tn, ok := d.Detect(x); ok && tn.F2 == 0 {
				t.Fatalf("twist %v dB, phase %.1f: a single tone at %.1f Hz", twist, ph, tn.F1)
			}
		}
	}
}

func TestToneDetectorRejects(t *testing.T) {
	d := NewToneDetector()
	r := rand.New(rand.NewSource(2))
	voiced := func(f0, h2 float64) []float64 {
		// first harmonic dominant, as in the purest voiced speech frames
		return sines([2]float64{f0, 8000}, [2]float64{2 * f0, 8000 * math.Pow(10, h2/20)}, [2]float64{3 * f0, 8000 * math.Pow(10, (h2-6)/20)})
	}
	noise := make([]float64, ToneWindow)
	for i := range noise {
		noise[i] = 3000 * r.NormFloat64()
	}
	chirp := make([]float64, ToneWindow)
	for n := range chirp {
		tt := float64(n) / 8000
		chirp[n] = 8000 * math.Sin(2*math.Pi*(800*tt+0.5*10000*tt*tt)) // 800-1200 Hz in 40 ms
	}
	square := make([]float64, ToneWindow)
	for n := range square {
		square[n] = 8000 * math.Copysign(1, math.Sin(2*math.Pi*500*float64(n)/8000+0.1))
	}
	for name, x := range map[string][]float64{
		"silence":                make([]float64, ToneWindow),
		"tone at -55 dBFS":       sines([2]float64{1000, 32767 * math.Pow(10, -55.0/20)}),
		"voiced, h2 at -20 dB":   voiced(190, -20),
		"voiced, h2 at -26 dB":   voiced(210, -26),
		"white noise":            noise,
		"three tones":            sines([2]float64{697, 6000}, [2]float64{1209, 6000}, [2]float64{2000, 6000}),
		"DTMF, 14 dB twist":      sines([2]float64{1336, 6000 * math.Pow(10, -14.0/20)}, [2]float64{770, 6000}),
		"chirp 800-1200 Hz":      chirp,
		"square wave":            square,
		"tone with 15 dB SNR":    nil, // filled below
		"two tones, 12 dB twist": sines([2]float64{1000, 8000}, [2]float64{1800, 8000 * math.Pow(10, -12.0/20)}),
	} {
		if x == nil {
			x = sines([2]float64{1000, 8000})
			for i := range x {
				x[i] += 8000 / math.Sqrt2 * math.Pow(10, -15.0/20) * r.NormFloat64()
			}
		}
		if tn, ok := d.Detect(x); ok {
			t.Errorf("%s: detected %+v", name, tn)
		}
	}
}
