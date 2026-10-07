package main

import (
	"encoding/binary"
	"sort"
	"time"

	"github.com/nnamon/mbevoc/fec"
	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/p25half"
)

// Datagram sizes of the md380-emu UDP protocol (and this server's extension).
const (
	pcmBytes   = 2 * p25half.FrameSamples // 160 little-endian int16 samples
	ambe49Size = 7                        // 49-bit frame, frame.Pack7 layout
	ambe72Size = 9                        // 72-bit on-air frame (extension)
)

// config holds the protocol-level options.
type config struct {
	perClient  bool          // one encoder/decoder per client address
	idle       time.Duration // per-client state is dropped after this long unused
	maxClients int           // per-client mode: least recently used is evicted beyond this
	reply72    bool          // answer PCM with 9-byte 72-bit frames instead of 7 bytes
	resetGap   time.Duration // a fresh encoder (decoder) after this long without encoding (decoding); 0: never
	encoder    p25half.Config
	decoder    p25half.DecoderConfig
}

// codec is the vocoder state of one stream.
type codec struct {
	enc              *p25half.Encoder
	dec              *p25half.Decoder
	used             time.Time
	lastEnc, lastDec time.Time // last encode and decode request (zero: none yet)
}

// stats counts what the server has done.
type stats struct {
	encoded, decoded, decoded72, ignored, clients, evicted uint64

	resets                 uint64    // encoders and decoders replaced after resetGap
	lastEncode, lastDecode time.Time // last request of each kind, any client (zero: none yet)
}

// server implements the request/response protocol independently of sockets.
type server struct {
	cfg       config
	now       func() time.Time
	shared    *codec
	seen      map[string]bool // shared mode: client addresses seen (for logging; bounded)
	clients   map[string]*codec
	lastSweep time.Time
	stats     stats
	started   time.Time
	encodeUs  latency // time to answer encode requests
}

func newServer(cfg config) *server {
	return &server{cfg: cfg, now: time.Now, started: time.Now(), seen: map[string]bool{}, clients: map[string]*codec{}}
}

func (s *server) newCodec() *codec {
	return &codec{enc: p25half.NewEncoderConfig(s.cfg.encoder), dec: p25half.NewDecoderConfig(s.cfg.decoder)}
}

// codecFor returns the state for a client: the single shared state (as
// md380-emu has), or the client's own in per-client mode.
func (s *server) codecFor(client string, now time.Time) *codec {
	if !s.cfg.perClient {
		if s.shared == nil {
			s.shared = s.newCodec()
		}
		if !s.seen[client] && len(s.seen) < 1024 {
			s.seen[client] = true
			s.stats.clients++
		}
		s.shared.used = now
		return s.shared
	}
	if now.Sub(s.lastSweep) >= time.Second {
		s.sweep(now)
	}
	c, ok := s.clients[client]
	if !ok {
		if len(s.clients) >= s.cfg.maxClients {
			s.evictOldest()
		}
		c = s.newCodec()
		s.clients[client] = c
		s.stats.clients++
	}
	c.used = now
	return c
}

// sweep drops per-client state unused for longer than the idle timeout.
func (s *server) sweep(now time.Time) {
	s.lastSweep = now
	for k, c := range s.clients {
		if now.Sub(c.used) > s.cfg.idle {
			delete(s.clients, k)
			s.stats.evicted++
		}
	}
}

// stale reports whether a stream side last used at last has been idle for
// longer than the reset gap, so the next request starts a new transmission.
func (s *server) stale(last, now time.Time) bool {
	return s.cfg.resetGap > 0 && !last.IsZero() && now.Sub(last) > s.cfg.resetGap
}

// encoderFor returns the client's encoder, replacing it with a fresh one
// when the client has not encoded for longer than the reset gap.
func (s *server) encoderFor(client string, now time.Time) *p25half.Encoder {
	c := s.codecFor(client, now)
	if s.stale(c.lastEnc, now) {
		c.enc = p25half.NewEncoderConfig(s.cfg.encoder)
		s.stats.resets++
	}
	c.lastEnc, s.stats.lastEncode = now, now
	return c.enc
}

// decoderFor is encoderFor for the decoder.
func (s *server) decoderFor(client string, now time.Time) *p25half.Decoder {
	c := s.codecFor(client, now)
	if s.stale(c.lastDec, now) {
		c.dec = p25half.NewDecoderConfig(s.cfg.decoder)
		s.stats.resets++
	}
	c.lastDec, s.stats.lastDecode = now, now
	return c.dec
}

func (s *server) evictOldest() {
	keys := make([]string, 0, len(s.clients))
	for k := range s.clients {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return s.clients[keys[i]].used.Before(s.clients[keys[j]].used) })
	delete(s.clients, keys[0])
	s.stats.evicted++
}

// handle answers one datagram from client, returning nil for datagrams the
// protocol ignores.  Dispatch is on length alone, as in md380-emu:
//
//	320 bytes (PCM)          -> encode -> 7 bytes (or 9 with reply72)
//	7 bytes (49-bit frame)   -> decode -> 320 bytes PCM
//	9 bytes (72-bit frame)   -> FEC decode, decode -> 320 bytes PCM (extension)
func (s *server) handle(pkt []byte, client string) []byte {
	now := s.now()
	switch len(pkt) {
	case pcmBytes:
		start := time.Now()
		var pcm [p25half.FrameSamples]int16
		for i := range pcm {
			pcm[i] = int16(binary.LittleEndian.Uint16(pkt[2*i:]))
		}
		b := s.encoderFor(client, now).Encode(&pcm)
		s.stats.encoded++
		var reply []byte
		if s.cfg.reply72 {
			c := fec.Encode(&b)
			p := fec.Pack(&c)
			reply = p[:]
		} else {
			p := b.Pack7()
			reply = p[:]
		}
		s.encodeUs.add(time.Since(start))
		return reply
	case ambe49Size:
		var p [ambe49Size]byte
		copy(p[:], pkt)
		b := frame.Unpack7(p)
		s.stats.decoded++
		return pcmBytesOf(s.decoderFor(client, now).Decode(&b))
	case ambe72Size:
		var p [ambe72Size]byte
		copy(p[:], pkt)
		c := fec.Unpack(p)
		s.stats.decoded72++
		pcm, _ := s.decoderFor(client, now).Decode72(&c)
		return pcmBytesOf(pcm)
	}
	s.stats.ignored++
	return nil
}

func pcmBytesOf(pcm [p25half.FrameSamples]int16) []byte {
	out := make([]byte, pcmBytes)
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}
