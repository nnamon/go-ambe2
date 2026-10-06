package main

import (
	"encoding/binary"
	"sort"
	"time"

	ambe "github.com/nnamon/go-ambe2"
	"github.com/nnamon/go-ambe2/fec"
	"github.com/nnamon/go-ambe2/frame"
)

// Datagram sizes of the md380-emu UDP protocol (and this server's extension).
const (
	pcmBytes   = 2 * ambe.FrameSamples // 160 little-endian int16 samples
	ambe49Size = 7                     // 49-bit frame, frame.Pack7 layout
	ambe72Size = 9                     // 72-bit on-air frame (extension)
)

// config holds the protocol-level options.
type config struct {
	perClient  bool          // one encoder/decoder per client address
	idle       time.Duration // per-client state is dropped after this long unused
	maxClients int           // per-client mode: least recently used is evicted beyond this
	reply72    bool          // answer PCM with 9-byte 72-bit frames instead of 7 bytes
	encoder    ambe.Config
	decoder    ambe.DecoderConfig
}

// codec is the vocoder state of one stream.
type codec struct {
	enc  *ambe.Encoder
	dec  *ambe.Decoder
	used time.Time
}

// stats counts what the server has done.
type stats struct {
	encoded, decoded, decoded72, ignored, clients, evicted uint64
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
}

func newServer(cfg config) *server {
	return &server{cfg: cfg, now: time.Now, seen: map[string]bool{}, clients: map[string]*codec{}}
}

func (s *server) newCodec() *codec {
	return &codec{enc: ambe.NewEncoderConfig(s.cfg.encoder), dec: ambe.NewDecoderConfig(s.cfg.decoder)}
}

// codecFor returns the state for a client: the single shared state (as
// md380-emu has), or the client's own in per-client mode.
func (s *server) codecFor(client string) *codec {
	now := s.now()
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
	switch len(pkt) {
	case pcmBytes:
		var pcm [ambe.FrameSamples]int16
		for i := range pcm {
			pcm[i] = int16(binary.LittleEndian.Uint16(pkt[2*i:]))
		}
		b := s.codecFor(client).enc.Encode(&pcm)
		s.stats.encoded++
		if s.cfg.reply72 {
			c := fec.Encode(&b)
			p := fec.Pack(&c)
			return p[:]
		}
		p := b.Pack7()
		return p[:]
	case ambe49Size:
		var p [ambe49Size]byte
		copy(p[:], pkt)
		b := frame.Unpack7(p)
		s.stats.decoded++
		return pcmBytesOf(s.codecFor(client).dec.Decode(&b))
	case ambe72Size:
		var p [ambe72Size]byte
		copy(p[:], pkt)
		c := fec.Unpack(p)
		s.stats.decoded72++
		pcm, _ := s.codecFor(client).dec.Decode72(&c)
		return pcmBytesOf(pcm)
	}
	s.stats.ignored++
	return nil
}

func pcmBytesOf(pcm [ambe.FrameSamples]int16) []byte {
	out := make([]byte, pcmBytes)
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}
