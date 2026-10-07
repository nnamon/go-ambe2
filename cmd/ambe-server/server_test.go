package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"net"
	"testing"
	"time"

	"github.com/nnamon/mbevoc/fec"
	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/p25half"
)

func testConfig() config {
	return config{idle: 30 * time.Second, maxClients: 4, encoder: p25half.DefaultConfig()}
}

// speech-like test signal: a harmonic tone with a slowly varying pitch.
func pcmFrames(n int, f0 float64) [][]byte {
	out := make([][]byte, n)
	t := 0
	for k := range out {
		b := make([]byte, pcmBytes)
		for i := 0; i < p25half.FrameSamples; i++ {
			f := f0 * (1 + 0.1*math.Sin(float64(t)/4000))
			v := 0.0
			for l := 1; float64(l)*f < 3500; l++ {
				v += math.Cos(2*math.Pi*f*float64(l)*float64(t)/8000) / float64(l)
			}
			binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(3000*v)))
			t++
		}
		out[k] = b
	}
	return out
}

func toPCM(b []byte) (p [p25half.FrameSamples]int16) {
	for i := range p {
		p[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return p
}

// TestEncodeReply checks 320-byte requests are answered with the library's
// frames in the 7-byte wire form, carrying encoder state across requests.
func TestEncodeReply(t *testing.T) {
	s := newServer(testConfig())
	ref := p25half.NewEncoder()
	for k, req := range pcmFrames(60, 140) {
		got := s.handle(req, "a")
		pcm := toPCM(req)
		b := ref.Encode(&pcm)
		want := b.Pack7()
		if !bytes.Equal(got, want[:]) {
			t.Fatalf("frame %d: reply %x, want %x", k, got, want)
		}
	}
}

// TestDecodeReply checks 7-byte requests are decoded with state carried
// across requests and answered with 320 bytes of little-endian PCM.
func TestDecodeReply(t *testing.T) {
	s := newServer(testConfig())
	enc, ref := p25half.NewEncoder(), p25half.NewDecoder()
	for k, req := range pcmFrames(60, 180) {
		pcm := toPCM(req)
		b := enc.Encode(&pcm)
		p := b.Pack7()
		got := s.handle(p[:], "a")
		if want := pcmBytesOf(ref.Decode(&b)); !bytes.Equal(got, want) {
			t.Fatalf("frame %d: decoded PCM differs", k)
		}
	}
}

// TestDecode72Reply checks the 9-byte extension, and that -fec replies are
// FEC-coded versions of the same frames.
func TestDecode72Reply(t *testing.T) {
	cfg := testConfig()
	cfg.reply72 = true
	s := newServer(cfg)
	enc, ref := p25half.NewEncoder(), p25half.NewDecoder()
	for k, req := range pcmFrames(40, 120) {
		reply := s.handle(req, "a")
		if len(reply) != ambe72Size {
			t.Fatalf("frame %d: -fec reply is %d bytes", k, len(reply))
		}
		pcm := toPCM(req)
		b := enc.Encode(&pcm)
		var p [9]byte
		copy(p[:], reply)
		c := fec.Unpack(p)
		if got, e := fec.Decode(&c); got != b || e.Total() != 0 {
			t.Fatalf("frame %d: 72-bit reply does not carry the encoded frame", k)
		}
		want, _ := ref.Decode72(&c)
		if got := s.handle(reply, "b"); !bytes.Equal(got, pcmBytesOf(want)) {
			t.Fatalf("frame %d: 9-byte decode differs", k)
		}
	}
}

func TestIgnoredLengths(t *testing.T) {
	s := newServer(testConfig())
	for _, n := range []int{0, 1, 6, 8, 10, 160, 319, 321, 640, 1500} {
		if r := s.handle(make([]byte, n), "a"); r != nil {
			t.Errorf("%d-byte datagram answered with %d bytes", n, len(r))
		}
	}
	if s.stats.ignored != 10 {
		t.Errorf("ignored count %d", s.stats.ignored)
	}
}

// TestStateModes checks that per-client mode keeps interleaved streams
// apart, while shared mode (md380-emu's behaviour) mixes them.
func TestStateModes(t *testing.T) {
	a, b := pcmFrames(40, 110), pcmFrames(40, 230)
	alone := func(frames [][]byte) [][]byte {
		s := newServer(testConfig())
		var out [][]byte
		for _, f := range frames {
			out = append(out, s.handle(f, "x"))
		}
		return out
	}
	wantA, wantB := alone(a), alone(b)
	run := func(perClient bool) (gotA, gotB [][]byte) {
		cfg := testConfig()
		cfg.perClient = perClient
		s := newServer(cfg)
		for i := range a {
			gotA = append(gotA, s.handle(a[i], "10.0.0.1:5000"))
			gotB = append(gotB, s.handle(b[i], "10.0.0.2:5000"))
		}
		return
	}
	gotA, gotB := run(true)
	for i := range wantA {
		if !bytes.Equal(gotA[i], wantA[i]) || !bytes.Equal(gotB[i], wantB[i]) {
			t.Fatalf("per-client mode: frame %d differs from encoding the stream alone", i)
		}
	}
	gotA, _ = run(false)
	same := 0
	for i := range wantA {
		if bytes.Equal(gotA[i], wantA[i]) {
			same++
		}
	}
	if same == len(wantA) {
		t.Fatal("shared mode unexpectedly kept the streams apart")
	}
}

func TestIdleExpiryAndEviction(t *testing.T) {
	cfg := testConfig()
	cfg.perClient = true
	cfg.idle = 5 * time.Second
	cfg.maxClients = 3
	s := newServer(cfg)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	req := pcmFrames(1, 150)[0]
	for _, c := range []string{"a", "b", "c"} {
		s.handle(req, c)
		now = now.Add(time.Second)
	}
	s.handle(req, "d") // beyond max-clients: evicts "a", the least recently used
	if _, ok := s.clients["a"]; ok || len(s.clients) != 3 {
		t.Fatalf("eviction: clients %v", keys(s.clients))
	}
	now = now.Add(10 * time.Second)
	s.handle(req, "e") // sweep drops everything idle for more than 5 s
	if len(s.clients) != 1 {
		t.Fatalf("idle sweep: clients %v", keys(s.clients))
	}
}

func keys(m map[string]*codec) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

// TestUDP runs the real socket loop: encode, decode, and silence for an
// ignored length.
func TestUDP(t *testing.T) {
	conn, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- serve(conn, newServer(testConfig()), loopConfig{}) }()
	defer func() {
		conn.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	cli, err := net.DialUDP("udp", nil, conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	roundTrip := func(req []byte) []byte {
		if _, err := cli.Write(req); err != nil {
			t.Fatal(err)
		}
		cli.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 2048)
		n, err := cli.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		return buf[:n]
	}
	var last []byte
	for _, f := range pcmFrames(10, 150) {
		last = roundTrip(f)
		if len(last) != ambe49Size {
			t.Fatalf("encode reply %d bytes", len(last))
		}
	}
	if pcm := roundTrip(last); len(pcm) != pcmBytes {
		t.Fatalf("decode reply %d bytes", len(pcm))
	}
	// An ignored datagram gets no reply.
	cli.Write(make([]byte, 100))
	cli.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if n, err := cli.Read(make([]byte, 2048)); err == nil {
		t.Fatalf("got a %d-byte reply to an ignored datagram", n)
	}
	var p [7]byte
	copy(p[:], last)
	if b := frame.Unpack7(p); b.Pack7() != p {
		t.Fatal("wire frame did not round-trip")
	}
}
