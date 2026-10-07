package p25half

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/internal/mbe"
	"github.com/nnamon/mbevoc/quant"
)

func TestToneIndex(t *testing.T) {
	for _, c := range []struct {
		tone    mbe.Tone
		id, amp int
		ok      bool
	}{
		{mbe.Tone{F1: 1000, A1: 32767}, 32, 127, true},
		{mbe.Tone{F1: 1000, A1: 32767 * math.Pow(10, -12.0/20)}, 32, 110, true}, // the MD-380's AD for -12 dBFS
		{mbe.Tone{F1: 1000, A1: 32767 * math.Pow(10, -50.0/20)}, 32, 57, true},
		{mbe.Tone{F1: 1015, A1: 8000}, 32, 110, true},
		{mbe.Tone{F1: 1016, A1: 8000}, 33, 110, true},
		{mbe.Tone{F1: 140, A1: 8000}, 0, 0, false},  // index 4
		{mbe.Tone{F1: 3829, A1: 8000}, 0, 0, false}, // index 123
		{mbe.Tone{F1: 156.25, A1: 8000}, 5, 110, true},
		{mbe.Tone{F1: 3812.5, A1: 8000}, 122, 110, true},
		{mbe.Tone{F1: 1336, A1: 6000, F2: 770, A2: 6000}, 133, 106, true},
		{mbe.Tone{F1: 770, A1: 6000, F2: 1336, A2: 6000}, 133, 106, true},
		{mbe.Tone{F1: 1336 * 1.02, A1: 6000, F2: 770, A2: 6000}, 133, 106, true},
		{mbe.Tone{F1: 1336 * 1.03, A1: 6000, F2: 770, A2: 6000}, 0, 0, false},
		{mbe.Tone{F1: 1336, A1: 6000 * math.Pow(10, -8.0/20), F2: 770, A2: 6000}, 133, 101, true}, // geometric mean
		{mbe.Tone{F1: 480, A1: 6000, F2: 440, A2: 6000}, 161, 106, true},
		{mbe.Tone{F1: 483, A1: 6000, F2: 407, A2: 6000}, 0, 0, false},
		{mbe.Tone{F1: 1000, A1: 6000, F2: 1500, A2: 6000}, 0, 0, false},
	} {
		id, amp, ok := toneIndex(c.tone)
		if id != c.id || amp != c.amp || ok != c.ok {
			t.Errorf("%+v: got %d, %d, %v; want %d, %d, %v", c.tone, id, amp, ok, c.id, c.amp, c.ok)
		}
	}
}

// encodeTones encodes x with tone frames enabled and returns the frames.
func encodeTones(x []float64, tones bool) ([]frame.Bits, []Analysis) {
	cfg := DefaultConfig()
	cfg.Tones = tones
	enc := NewEncoderConfig(cfg)
	var out []frame.Bits
	var an []Analysis
	var pcm [FrameSamples]int16
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		for i := range pcm {
			pcm[i] = int16(math.Max(-32768, math.Min(32767, math.Round(x[k*FrameSamples+i]))))
		}
		out = append(out, enc.Encode(&pcm))
		an = append(an, enc.Last)
	}
	return out, an
}

func sineMix(n int, comps ...[2]float64) []float64 {
	x := make([]float64, n)
	for _, c := range comps {
		for i := range x {
			x[i] += c[1] * math.Sin(2*math.Pi*c[0]*float64(i)/8000)
		}
	}
	return x
}

// TestEncoderTones sends a DTMF digit sequence: each digit's tone frames
// carry its index and level, and without Config.Tones there are none.
func TestEncoderTones(t *testing.T) {
	digits := []struct {
		id     int
		hi, lo float64
	}{{129, 1209, 697}, {137, 1477, 852}, {128, 1336, 941}, {143, 1477, 941}, {139, 1633, 770}}
	var x []float64
	gap := make([]float64, 800) // 100 ms
	x = append(x, gap...)
	for _, d := range digits {
		x = append(x, sineMix(800, [2]float64{d.hi, 6000}, [2]float64{d.lo, 6000})...)
		x = append(x, gap...)
	}
	x = append(x, gap...)
	frames, _ := encodeTones(x, true)
	var seen []int
	for _, b := range frames {
		if !IsTone(&b) {
			continue
		}
		id, amp := ToneParams(&b)
		if amp != 106 {
			t.Errorf("tone %d: AD %d, want 106 (6000 peak per tone)", id, amp)
		}
		if len(seen) == 0 || seen[len(seen)-1] != id {
			seen = append(seen, id)
		}
	}
	if len(seen) != len(digits) {
		t.Fatalf("tone indices %v", seen)
	}
	for i, d := range digits {
		if seen[i] != d.id {
			t.Errorf("digit %d: index %d, want %d", i, seen[i], d.id)
		}
	}
	plain, _ := encodeTones(x, false)
	for _, b := range plain {
		if IsTone(&b) {
			t.Fatal("tone frame without Config.Tones")
		}
	}
}

// TestEncoderTonesKeepSync encodes speech-like sound, a DTMF digit, then
// speech-like sound again: the voice frames after the tone must dequantize,
// on a predictor that skips the tone frames as a decoder does, to exactly
// what the encoder computed.
func TestEncoderTonesKeepSync(t *testing.T) {
	x := harmonic(160*30, 150, 5000)
	x = append(x, sineMix(160*10, [2]float64{1336, 6000}, [2]float64{770, 6000})...)
	x = append(x, harmonic(160*30, 210, 5000)...)
	frames, an := encodeTones(x, true)
	dec := quant.NewPredictor()
	tones := 0
	for i, b := range frames {
		if IsTone(&b) {
			tones++
			continue
		}
		m, _ := dec.Dequantize(b.Params())
		if m != an[i].Model {
			t.Fatalf("frame %d: decoder model differs from the encoder's", i)
		}
	}
	if tones < 8 {
		t.Fatalf("%d tone frames for 200 ms of DTMF", tones)
	}
}

// TestEncoderTonesRoundTrip decodes tone frames back to sound: the tones
// come out at Annex J's frequencies (within 0.5%) and the input's level.
func TestEncoderTonesRoundTrip(t *testing.T) {
	for _, c := range [][][2]float64{
		{{1000, 8231}},              // -12 dBFS
		{{1336, 6000}, {770, 6000}}, // DTMF 5
		{{440, 4000}, {350, 4000}},  // dial tone
	} {
		x := sineMix(8000, c...)
		frames, _ := encodeTones(x, true)
		dec := NewDecoder()
		var y []float64
		for i := range frames {
			out := dec.Decode(&frames[i])
			for _, v := range out {
				y = append(y, float64(v))
			}
		}
		seg := y[2400:6400] // steady state, clear of the encoder's delay
		in := 0.0
		for _, v := range x[2400:6400] {
			in += v * v
		}
		outE := 0.0
		for _, v := range seg {
			outE += v * v
		}
		if db := 10 * math.Log10(outE/in); math.Abs(db) > 1 {
			t.Errorf("%v: level %+.2f dB", c, db)
		}
		for _, comp := range c {
			f := comp[0]
			// energy within 0.5% of f relative to the total, by correlation
			best := 0.0
			for g := f * 0.995; g <= f*1.005; g += 0.25 {
				re, im := 0.0, 0.0
				for n, v := range seg {
					re += v * math.Cos(2*math.Pi*g*float64(n)/8000)
					im += v * math.Sin(2*math.Pi*g*float64(n)/8000)
				}
				best = math.Max(best, 2*(re*re+im*im)/float64(len(seg)))
			}
			if share := best / outE; share < 0.8/float64(len(c)) {
				t.Errorf("%v: little energy near %v Hz (%.2f of the output)", c, f, share)
			}
		}
	}
}

// TestEncoderTonesRingback: 480 + 440 Hz, closer than the detector can
// separate, must never come out as some other tone.
func TestEncoderTonesRingback(t *testing.T) {
	frames, _ := encodeTones(sineMix(8000, [2]float64{480, 6000}, [2]float64{440, 6000}), true)
	for i, b := range frames {
		if IsTone(&b) {
			if id, _ := ToneParams(&b); id != 161 {
				t.Fatalf("frame %d: tone index %d", i, id)
			}
		}
	}
}

// TestEncoderTonesOnSpeech encodes the speech corpus (research/testdata,
// from research/setup.sh) with tone frames enabled: none may appear.
func TestEncoderTonesOnSpeech(t *testing.T) {
	if testing.Short() {
		t.Skip("encodes about 16 minutes of speech")
	}
	paths, _ := filepath.Glob("../research/testdata/*/*/ref.raw")
	if len(paths) == 0 {
		t.Skip("no corpus (run research/setup.sh)")
	}
	frames := 0
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		x := make([]float64, len(raw)/2)
		for i := range x {
			x[i] = float64(int16(uint16(raw[2*i]) | uint16(raw[2*i+1])<<8))
		}
		out, _ := encodeTones(x, true)
		frames += len(out)
		for i, b := range out {
			if IsTone(&b) {
				t.Errorf("%s frame %d: tone frame in speech", p, i)
			}
		}
	}
	t.Logf("%d recordings, %d frames", len(paths), frames)
}
