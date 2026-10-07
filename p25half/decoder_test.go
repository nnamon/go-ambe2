package p25half

import (
	"math"
	"math/rand"
	"testing"

	"github.com/nnamon/mbevoc/fec"
	"github.com/nnamon/mbevoc/frame"
)

func rms(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		s += v * v
	}
	return math.Sqrt(s / float64(len(x)))
}

// codec runs x through the encoder and the decoder.
func codec(x []float64) []float64 {
	enc, dec := NewEncoder(), NewDecoder()
	var out []float64
	var pcm [FrameSamples]int16
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		for i := range pcm {
			pcm[i] = int16(math.Max(-32768, math.Min(32767, math.Round(x[k*FrameSamples+i]))))
		}
		b := enc.Encode(&pcm)
		y := dec.Decode(&b)
		for _, v := range y {
			out = append(out, float64(v))
		}
	}
	return out
}

// peakFreq returns the frequency (Hz) of the strongest spectral peak of x.
func peakFreq(x []float64, lo, hi float64) (float64, float64) {
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
	return bestF, best
}

func TestCodecRoundTripLevelAndPitch(t *testing.T) {
	for _, f0 := range []float64{110, 180, 250} {
		x := harmonic(16000, f0, 6000)
		y := codec(x)
		in, out := rms(x[4000:]), rms(y[4800:])
		if r := out / in; r < 0.7 || r > 1.4 {
			t.Errorf("f0=%v: output/input level %.2f", f0, r)
		}
		f, _ := peakFreq(y[8000:12000], f0*0.8, f0*1.2)
		if math.Abs(f-f0) > f0*0.03 {
			t.Errorf("f0=%v: decoded fundamental at %.1f Hz", f0, f)
		}
	}
}

func TestCodecNoiseLevel(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	x := make([]float64, 16000)
	for i := range x {
		x[i] = 1500 * r.NormFloat64()
	}
	y := codec(x)
	if ratio := rms(y[4800:]) / rms(x[4000:]); ratio < 0.5 || ratio > 1.5 {
		t.Errorf("noise output/input level %.2f", ratio)
	}
}

// TestDecode72MatchesDecode checks the FEC path is transparent without errors.
func TestDecode72MatchesDecode(t *testing.T) {
	x := harmonic(8000, 150, 5000)
	enc := NewEncoder()
	d49, d72 := NewDecoder(), NewDecoder()
	var pcm [FrameSamples]int16
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		for i := range pcm {
			pcm[i] = int16(x[k*FrameSamples+i])
		}
		b := enc.Encode(&pcm)
		c := fec.Encode(&b)
		a := d49.Decode(&b)
		y, e := d72.Decode72(&c)
		if e.Total() != 0 || a != y {
			t.Fatalf("frame %d: Decode72 differs from Decode (errors %+v)", k, e)
		}
	}
}

// TestRepeatThenMute feeds uncorrectable frames after good ones: the decoder
// must repeat (non-silent output) three times, then mute to |x| <= 5.
func TestRepeatThenMute(t *testing.T) {
	x := harmonic(4800, 150, 5000)
	enc, dec := NewEncoder(), NewDecoder()
	var pcm [FrameSamples]int16
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		for i := range pcm {
			pcm[i] = int16(x[k*FrameSamples+i])
		}
		b := enc.Encode(&pcm)
		c := fec.Encode(&b)
		dec.Decode72(&c)
	}
	// Corrupt c0 heavily: flip 8 of its bits (positions of c0 in the interleave).
	var bad fec.Bits72
	{
		var b frame.Bits
		bad = fec.Encode(&b)
		for _, i := range []int{0, 2, 4, 6, 8, 10, 12, 14} {
			bad[i] ^= 1
		}
	}
	for n := 1; n <= 6; n++ {
		y, e := dec.Decode72(&bad)
		peak := 0
		for _, v := range y {
			if a := int(math.Abs(float64(v))); a > peak {
				peak = a
			}
		}
		if n < 4 && peak <= 5 {
			t.Fatalf("repeat %d (errors %+v): output muted too early", n, e)
		}
		if n >= 4 && peak > 5 {
			t.Fatalf("frame %d of corrupted input: peak %d, want muted", n, peak)
		}
	}
}

func TestToneFrames(t *testing.T) {
	for _, tc := range []struct {
		id    int
		freqs []float64
	}{
		{32, []float64{1000}},
		{5, []float64{156.25}},
		{128, []float64{942, 1334.5}},  // DTMF "0" per Annex J (78.5 Hz x 12, 17)
		{160, []float64{351.1, 438.9}}, // call progress per Annex J (87.78 Hz x 4, 5)
	} {
		b := ToneFrame(tc.id, 100)
		if !IsTone(&b) {
			t.Fatalf("id %d: not recognised as a tone frame", tc.id)
		}
		if id, amp := ToneParams(&b); id != tc.id || amp != 100 {
			t.Fatalf("ToneParams = %d, %d", id, amp)
		}
		dec := NewDecoder()
		var y []float64
		for k := 0; k < 20; k++ {
			s := dec.Decode(&b)
			for _, v := range s {
				y = append(y, float64(v))
			}
		}
		seg := y[1600:3200]
		want := toneLevel * 32768 * math.Pow(10, 0.03555*float64(100-127))
		for _, f := range tc.freqs {
			got, _ := peakFreq(seg, f-15, f+15)
			if math.Abs(got-f) > 1.5 {
				t.Errorf("id %d: component near %.1f Hz found at %.1f Hz", tc.id, f, got)
			}
		}
		wantRMS := want * math.Sqrt(float64(len(tc.freqs))/2)
		if r := rms(seg) / wantRMS; r < 0.97 || r > 1.03 {
			t.Errorf("id %d: tone level %.3f of expected", tc.id, r)
		}
	}
	// Index 255 is a zero-amplitude tone.
	b := ToneFrame(255, 100)
	dec := NewDecoder()
	for k := 0; k < 5; k++ {
		y := dec.Decode(&b)
		for _, v := range y {
			if v != 0 {
				t.Fatalf("ID 255 produced sample %d", v)
			}
		}
	}
}

func TestSilenceFramesAreQuiet(t *testing.T) {
	// Speech-level voice frames, then silence frames carrying the same gain:
	// silence must come out SilenceGain times the standard level.
	x := harmonic(4800, 150, 5000)
	enc := NewEncoder()
	var frames []frame.Bits
	var pcm [FrameSamples]int16
	for k := 0; (k+1)*FrameSamples <= len(x); k++ {
		for i := range pcm {
			pcm[i] = int16(x[k*FrameSamples+i])
		}
		frames = append(frames, enc.Encode(&pcm))
	}
	p := frames[len(frames)-1].Params()
	p[0], p[1] = 124, 16
	sil := p.Bits()
	level := func(cfg DecoderConfig) float64 {
		d := NewDecoderConfig(cfg)
		for i := range frames {
			d.Decode(&frames[i])
		}
		var y []float64
		for k := 0; k < 10; k++ {
			s := d.Decode(&sil)
			for _, v := range s {
				y = append(y, float64(v))
			}
		}
		return rms(y[480:])
	}
	std, def := level(DecoderConfig{SilenceGain: 1}), level(DecoderConfig{})
	if r := def / std; math.Abs(r-SilenceGain) > 0.05 {
		t.Errorf("silence level ratio %.3f, want %.3f", r, SilenceGain)
	}
}

func TestDecoderDeterministic(t *testing.T) {
	x := harmonic(8000, 200, 4000)
	a, b := codec(x), codec(x)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs between runs", i)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	x := harmonic(160*50, 140, 6000)
	enc := NewEncoder()
	var frames []frame.Bits
	var pcm [FrameSamples]int16
	for k := 0; k < 50; k++ {
		for j := range pcm {
			pcm[j] = int16(x[k*FrameSamples+j])
		}
		frames = append(frames, enc.Encode(&pcm))
	}
	dec := NewDecoder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(&frames[i%50])
	}
}
