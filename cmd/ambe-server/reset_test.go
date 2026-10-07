package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/nnamon/mbevoc/p25half"
)

const framePeriod = 20 * time.Millisecond

// clocked returns a server on a fake clock and a function that advances it.
func clocked(cfg config) (*server, func(time.Duration)) {
	s := newServer(cfg)
	now := time.Unix(1_000_000, 0)
	s.now = func() time.Time { return now }
	return s, func(d time.Duration) { now = now.Add(d) }
}

// encodeAll encodes frames on a new server, the reference for a fresh encoder.
func encodeAll(frames [][]byte) [][]byte {
	s := newServer(testConfig())
	var out [][]byte
	for _, f := range frames {
		out = append(out, s.handle(f, "x"))
	}
	return out
}

// wireFrames encodes PCM into 7-byte frames for decode requests.
func wireFrames(frames [][]byte) [][]byte {
	enc := p25half.NewEncoder()
	var out [][]byte
	for _, f := range frames {
		pcm := toPCM(f)
		b := enc.Encode(&pcm)
		p := b.Pack7()
		out = append(out, p[:])
	}
	return out
}

// decodeAll decodes frames on a new server, the reference for a fresh decoder.
func decodeAll(frames [][]byte) [][]byte {
	return encodeAll(frames) // handle dispatches on length
}

func sameFrames(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// TestResetGapEncoder encodes transmission A, pauses, then encodes B: with a
// reset gap B is encoded exactly as by a brand-new encoder; without one, A's
// buffered tail and encoder state carry into B.
func TestResetGapEncoder(t *testing.T) {
	a, b := pcmFrames(30, 120), pcmFrames(30, 210)
	want := encodeAll(b)
	run := func(gap time.Duration) ([][]byte, uint64) {
		cfg := testConfig()
		cfg.resetGap = gap
		s, advance := clocked(cfg)
		for _, f := range a {
			s.handle(f, "x")
			advance(framePeriod)
		}
		advance(time.Second)
		var got [][]byte
		for _, f := range b {
			got = append(got, s.handle(f, "x"))
			advance(framePeriod)
		}
		return got, s.stats.resets
	}
	got, resets := run(200 * time.Millisecond)
	if !sameFrames(got, want) {
		t.Error("with -reset-gap, transmission B differs from B on a fresh encoder")
	}
	if resets != 1 {
		t.Errorf("with -reset-gap: %d resets, want 1", resets)
	}
	got, resets = run(0)
	if sameFrames(got, want) {
		t.Error("without -reset-gap, transmission B unexpectedly matches a fresh encoder")
	}
	if resets != 0 {
		t.Errorf("without -reset-gap: %d resets", resets)
	}
}

// TestResetGapDecoder is TestResetGapEncoder for the decoder.
func TestResetGapDecoder(t *testing.T) {
	a, b := wireFrames(pcmFrames(30, 140)), wireFrames(pcmFrames(30, 190))
	want := decodeAll(b)
	cfg := testConfig()
	cfg.resetGap = 200 * time.Millisecond
	s, advance := clocked(cfg)
	for _, f := range a {
		s.handle(f, "x")
		advance(framePeriod)
	}
	advance(time.Second)
	var got [][]byte
	for _, f := range b {
		got = append(got, s.handle(f, "x"))
		advance(framePeriod)
	}
	if !sameFrames(got, want) {
		t.Error("transmission B differs from B on a fresh decoder")
	}
	if s.stats.resets != 1 {
		t.Errorf("%d resets, want 1", s.stats.resets)
	}
}

// TestResetGapSides checks that only the side that paused is reset: a
// decode pause in the middle of continuous encoding resets the decoder and
// leaves the encoder's output untouched, and the other way round.
func TestResetGapSides(t *testing.T) {
	const n = 120
	pcm := pcmFrames(n, 150)
	wire := wireFrames(pcmFrames(n, 230))
	paused := func(i int) bool { return i >= 40 && i < 80 } // 800 ms without requests of one kind

	cases := []struct {
		name               string
		pauseEnc, pauseDec bool
	}{
		{"decode pause", false, true},
		{"encode pause", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.resetGap = 200 * time.Millisecond
			s, advance := clocked(cfg)
			var enc, dec [][]byte
			for i := 0; i < n; i++ {
				if !(c.pauseEnc && paused(i)) {
					enc = append(enc, s.handle(pcm[i], "x"))
				}
				if !(c.pauseDec && paused(i)) {
					dec = append(dec, s.handle(wire[i], "x"))
				}
				advance(framePeriod)
			}
			if s.stats.resets != 1 {
				t.Errorf("%d resets, want 1", s.stats.resets)
			}
			// The side that ran continuously matches an uninterrupted stream.
			// The side that paused matches a fresh stream from the resumption.
			if c.pauseDec {
				if !sameFrames(enc, encodeAll(pcm)) {
					t.Error("continuous encoding was disturbed by the decode pause")
				}
				if !sameFrames(dec[40:], decodeAll(wire[80:])) {
					t.Error("decoding after the pause differs from a fresh decoder")
				}
			} else {
				if !sameFrames(dec, decodeAll(wire)) {
					t.Error("continuous decoding was disturbed by the encode pause")
				}
				if !sameFrames(enc[40:], encodeAll(pcm[80:])) {
					t.Error("encoding after the pause differs from a fresh encoder")
				}
			}
		})
	}
}

// TestResetGapPerClient checks that in per-client mode the gap is measured
// per client: a client that keeps talking is never reset.
func TestResetGapPerClient(t *testing.T) {
	a, b := pcmFrames(100, 120), pcmFrames(100, 200)
	cfg := testConfig()
	cfg.perClient = true
	cfg.resetGap = 200 * time.Millisecond
	s, advance := clocked(cfg)
	var gotA, gotB [][]byte
	for i := range a {
		gotA = append(gotA, s.handle(a[i], "10.0.0.1:5000"))
		if i < 30 || i >= 60 {
			gotB = append(gotB, s.handle(b[i], "10.0.0.2:5000"))
		}
		advance(framePeriod)
	}
	if s.stats.resets != 1 {
		t.Errorf("%d resets, want 1", s.stats.resets)
	}
	if !sameFrames(gotA, encodeAll(a)) {
		t.Error("the continuously talking client was disturbed")
	}
	if !sameFrames(gotB[30:], encodeAll(b[60:])) {
		t.Error("the paused client did not get a fresh encoder")
	}
}

// TestResetGapBoundary checks that only a gap longer than -reset-gap resets.
func TestResetGapBoundary(t *testing.T) {
	const gap = 200 * time.Millisecond
	f := pcmFrames(1, 150)[0]
	for _, c := range []struct {
		pause time.Duration
		reset uint64
	}{{gap - time.Millisecond, 0}, {gap, 0}, {gap + time.Nanosecond, 1}} {
		cfg := testConfig()
		cfg.resetGap = gap
		s, advance := clocked(cfg)
		s.handle(f, "x")
		advance(c.pause)
		s.handle(f, "x")
		if s.stats.resets != c.reset {
			t.Errorf("pause %v: %d resets, want %d", c.pause, s.stats.resets, c.reset)
		}
	}
}
