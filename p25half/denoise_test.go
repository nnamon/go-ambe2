package p25half

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestEncoderDenoiseDelay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Denoise = true
	if d, base := NewEncoderConfig(cfg).Delay(), NewEncoder().Delay(); d != base+160 {
		t.Errorf("delay %d, want %d + 160", d, base)
	}
}

// TestEncoderDenoiseTones: tone frames are found on the input before
// suppression, so DTMF still comes through, clean or in noise.
func TestEncoderDenoiseTones(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for _, snr := range []float64{math.Inf(1), 25} {
		x := make([]float64, 800)
		x = append(x, sineMix(8000, [2]float64{1336, 6000}, [2]float64{770, 6000})...)
		x = append(x, make([]float64, 1600)...)
		if !math.IsInf(snr, 1) {
			sd := 6000 / math.Pow(10, snr/20) // DTMF power is 6000²: two tones of 6000²/2
			for i := range x {
				x[i] += sd * r.NormFloat64()
			}
		}
		cfg := DefaultConfig()
		cfg.Tones, cfg.Denoise = true, true
		enc := NewEncoderConfig(cfg)
		var pcm [FrameSamples]int16
		tones := 0
		for k := 0; (k+1)*FrameSamples <= len(x); k++ {
			for i := range pcm {
				pcm[i] = int16(math.Max(-32768, math.Min(32767, math.Round(x[k*FrameSamples+i]))))
			}
			if b := enc.Encode(&pcm); IsTone(&b) {
				if id, _ := ToneParams(&b); id != 133 {
					t.Fatalf("SNR %v: tone index %d", snr, id)
				}
				tones++
			}
		}
		if tones < 45 {
			t.Errorf("SNR %v dB: %d tone frames for 1 s of DTMF", snr, tones)
		}
	}
}

// TestEncoderDenoiseTonesOnSpeech: with both options, the held-out half of
// the speech corpus still yields no tone frame (tone detection reads the
// input before suppression).
func TestEncoderDenoiseTonesOnSpeech(t *testing.T) {
	if testing.Short() {
		t.Skip("encodes about 8 minutes of speech")
	}
	paths, _ := filepath.Glob("../research/testdata/heldout/*/ref.raw")
	if len(paths) == 0 {
		t.Skip("no corpus (run research/setup.sh)")
	}
	cfg := DefaultConfig()
	cfg.Tones, cfg.Denoise = true, true
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		enc := NewEncoderConfig(cfg)
		var pcm [FrameSamples]int16
		for k := 0; (k+1)*2*FrameSamples <= len(raw); k++ {
			for i := range pcm {
				j := 2 * (k*FrameSamples + i)
				pcm[i] = int16(uint16(raw[j]) | uint16(raw[j+1])<<8)
			}
			if b := enc.Encode(&pcm); IsTone(&b) {
				t.Errorf("%s frame %d: tone frame in speech", p, k)
			}
		}
	}
}
