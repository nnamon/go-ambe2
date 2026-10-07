package p25half

import (
	"math"
	"math/rand"
	"testing"

	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/quant"
)

// encodeSignal encodes x (8 kHz samples) and returns the decoded models,
// dropping the frames that only cover the encoder's start-up delay.
func encodeSignal(t testing.TB, cfg Config, x []float64) ([]frame.Bits, []quant.Model, []quant.Kind) {
	enc := NewEncoderConfig(cfg)
	dec := quant.NewPredictor()
	skip := (enc.Delay() + FrameSamples - 1) / FrameSamples
	var bits []frame.Bits
	var models []quant.Model
	var kinds []quant.Kind
	var pcm [FrameSamples]int16
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		for i := range pcm {
			v := math.Round(x[k*FrameSamples+i])
			pcm[i] = int16(math.Max(-32768, math.Min(32767, v)))
		}
		b := enc.Encode(&pcm)
		m, kind := dec.Dequantize(b.Params())
		if kind == quant.Erasure || kind == quant.Tone {
			t.Fatalf("frame %d: encoder emitted a %v frame", k, kind)
		}
		if k >= skip {
			bits = append(bits, b)
			models = append(models, m)
			kinds = append(kinds, kind)
		}
	}
	return bits, models, kinds
}

// harmonic returns n samples of a periodic signal with fundamental f0 (Hz)
// and harmonics up to 3.6 kHz falling off as 1/l, at the given peak level.
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

func voicedFraction(m *quant.Model) float64 {
	n := 0
	for l := 1; l <= m.L; l++ {
		if m.Voiced[l] {
			n++
		}
	}
	return float64(n) / float64(m.L)
}

func TestEncoderTracksPitchOfHarmonicSignals(t *testing.T) {
	for _, f0 := range []float64{85, 110, 140, 200, 260, 330} {
		_, models, kinds := encodeSignal(t, DefaultConfig(), harmonic(8000, f0, 8000))
		good, voiced := 0, 0.0
		for i, m := range models {
			if kinds[i] != quant.Voice {
				t.Fatalf("f0=%v: frame %d is not a voice frame", f0, i)
			}
			got := m.W0 / (2 * math.Pi) * 8000
			if math.Abs(math.Log2(got/f0)) < 0.03 {
				good++
			}
			voiced += voicedFraction(&models[i])
		}
		voiced /= float64(len(models))
		if float64(good) < 0.95*float64(len(models)) {
			t.Errorf("f0=%v Hz: pitch within 2%% in only %d/%d frames", f0, good, len(models))
		}
		if voiced < 0.8 {
			t.Errorf("f0=%v Hz: voiced fraction %.2f", f0, voiced)
		}
	}
}

func TestEncoderNoiseIsUnvoiced(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	x := make([]float64, 16000)
	for i := range x {
		x[i] = 2000 * r.NormFloat64()
	}
	_, models, kinds := encodeSignal(t, DefaultConfig(), x)
	v := 0.0
	for i := range models {
		if kinds[i] != quant.Voice {
			t.Fatalf("frame %d of loud noise sent as %v", i, kinds[i])
		}
		v += voicedFraction(&models[i])
	}
	if v /= float64(len(models)); v > 0.15 {
		t.Errorf("white noise voiced fraction %.2f", v)
	}
}

func TestEncoderSilence(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	x := make([]float64, 16000)
	for i := 8000; i < len(x); i++ {
		x[i] = 20 * r.NormFloat64() // faint background, -64 dBFS
	}
	_, _, kinds := encodeSignal(t, DefaultConfig(), x)
	for i, k := range kinds {
		if k != quant.Silence {
			t.Fatalf("frame %d: quiet input sent as %v, want silence", i, k)
		}
	}
	cfg := DefaultConfig()
	cfg.Silence = false
	_, _, kinds = encodeSignal(t, cfg, x)
	for i, k := range kinds {
		if k != quant.Voice {
			t.Fatalf("Silence=false: frame %d sent as %v", i, k)
		}
	}
}

// TestEncoderLevelTracksInput checks a 6 dB louder input raises the decoded
// log2 amplitudes by about one.
func TestEncoderLevelTracksInput(t *testing.T) {
	mean := func(level float64) float64 {
		_, models, _ := encodeSignal(t, DefaultConfig(), harmonic(8000, 150, level))
		s, n := 0.0, 0
		for _, m := range models[5:] {
			for l := 1; l <= m.L; l++ {
				s += m.Log2M[l]
				n++
			}
		}
		return s / float64(n)
	}
	d := mean(8000) - mean(4000)
	if math.Abs(d-1) > 0.2 {
		t.Errorf("doubling the input changed mean log2 M by %.3f, want ~1", d)
	}
}

// TestEncoderDecodedAmplitudeScale checks the absolute amplitude convention:
// a voiced harmonic of amplitude A decodes to a spectral amplitude near A/2
// (TIA-102.BABA eq. 43; the synthesizer doubles it).
func TestEncoderDecodedAmplitudeScale(t *testing.T) {
	const f0, A = 125.0, 1000.0 // 25 harmonics: peak ~25000, no clipping
	x := make([]float64, 8000)
	for i := range x {
		for l := 1; l <= 25; l++ {
			x[i] += A * math.Cos(2*math.Pi*f0*float64(l*i)/8000+float64(l))
		}
	}
	_, models, _ := encodeSignal(t, DefaultConfig(), x)
	s, n := 0.0, 0
	for _, m := range models[5:] {
		for l := 2; l <= 20 && l <= m.L; l++ {
			s += m.Log2M[l]
			n++
		}
	}
	if got := s / float64(n); math.Abs(got-math.Log2(A/2)) > 0.5 {
		t.Errorf("mean decoded log2 M %.2f, want ~%.2f", got, math.Log2(A/2))
	}
}

func TestEncoderDeterministic(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	x := harmonic(8000, 180, 5000)
	for i := range x {
		x[i] += 300 * r.NormFloat64()
	}
	a, _, _ := encodeSignal(t, DefaultConfig(), x)
	b, _, _ := encodeSignal(t, DefaultConfig(), x)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("frame %d differs between runs", i)
		}
	}
}

// TestEncodersInParallel runs separate encoders on separate goroutines (they
// share read-only tables) and checks each matches a serial run.  Run with
// -race to check the sharing.
func TestEncodersInParallel(t *testing.T) {
	x := harmonic(160*40, 160, 6000)
	encode := func() []frame.Bits {
		enc := NewEncoder()
		var out []frame.Bits
		var pcm [FrameSamples]int16
		for k := 0; (k+1)*FrameSamples <= len(x); k++ {
			for i := range pcm {
				pcm[i] = int16(math.Round(x[k*FrameSamples+i]))
			}
			out = append(out, enc.Encode(&pcm))
		}
		return out
	}
	want := encode()
	errs := make(chan string, 4)
	for g := 0; g < 4; g++ {
		go func() {
			got := encode()
			for i := range want {
				if got[i] != want[i] {
					errs <- "frame differs from the serial run"
					return
				}
			}
			errs <- ""
		}()
	}
	for g := 0; g < 4; g++ {
		if e := <-errs; e != "" {
			t.Error(e)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	x := harmonic(160*50, 140, 6000)
	enc := NewEncoder()
	var pcm [FrameSamples]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := i % 50
		for j := range pcm {
			pcm[j] = int16(x[k*FrameSamples+j])
		}
		enc.Encode(&pcm)
	}
}
