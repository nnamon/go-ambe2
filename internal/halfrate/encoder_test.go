package halfrate

import (
	"math/rand"
	"testing"

	"github.com/nnamon/mbevoc/internal/mbe"
	"github.com/nnamon/mbevoc/quant"
)

func noiseFrames(n int, rms float64, seed int64) [][mbe.FrameSamples]int16 {
	r := rand.New(rand.NewSource(seed))
	out := make([][mbe.FrameSamples]int16, n)
	for k := range out {
		for i := range out[k] {
			out[k][i] = int16(rms * r.NormFloat64())
		}
	}
	return out
}

// TestResetWithoutDenoise: Reset is the same as a new encoder.
func TestResetWithoutDenoise(t *testing.T) {
	a := NewEncoder(DefaultConfig(), quant.AMBE2)
	for _, f := range noiseFrames(30, 2000, 1) {
		a.Encode(&f)
	}
	a.Reset()
	b := NewEncoder(DefaultConfig(), quant.AMBE2)
	for i, f := range noiseFrames(40, 2000, 2) {
		if pa, pb := a.Encode(&f), b.Encode(&f); pa != pb {
			t.Fatalf("frame %d after Reset differs from a new encoder", i)
		}
	}
}

// TestResetKeepsNoiseEstimate: with Denoise, Reset keeps the suppressor and
// its estimates but drops the signal in flight.
func TestResetKeepsNoiseEstimate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Denoise = true
	e := NewEncoder(cfg, quant.AMBE2)
	for _, f := range noiseFrames(100, 300, 3) {
		e.Encode(&f)
	}
	ns := e.ns
	e.Reset()
	if e.ns != ns {
		t.Fatal("Reset replaced the suppressor")
	}
	fresh := NewEncoder(cfg, quant.AMBE2)
	if e.raw != fresh.raw || e.Delay() != fresh.Delay() {
		t.Fatal("Reset kept the input history")
	}
}

// TestSamplesBeforeSuppression: with Denoise, Samples (for tone detection)
// returns the input before suppression, aligned like the front end's.
func TestSamplesBeforeSuppression(t *testing.T) {
	plain := NewEncoder(DefaultConfig(), quant.AMBE2)
	cfg := DefaultConfig()
	cfg.Denoise = true
	dn := NewEncoder(cfg, quant.AMBE2)
	frames := noiseFrames(20, 3000, 4)
	// The suppressing encoder analyses each frame denoise.Delay samples
	// (exactly one frame) later than the plain one.
	var want [][]float64
	for i, f := range frames {
		plain.Encode(&f)
		dn.Encode(&f)
		want = append(want, append([]float64(nil), plain.Samples(mbe.ToneWindow)...))
		if i >= 6 {
			got := dn.Samples(mbe.ToneWindow)
			for j := range got {
				if got[j] != want[i-1][j] {
					t.Fatalf("frame %d sample %d: %v, want %v", i, j, got[j], want[i-1][j])
				}
			}
		}
	}
}
