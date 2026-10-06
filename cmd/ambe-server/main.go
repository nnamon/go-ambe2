// Command ambe-server is a drop-in replacement for the UDP vocoder server of
// md380-emu ("md380-emu -S <port>"), which DMR/analog bridges such as
// DVSwitch's Analog_Bridge use for AMBE+2 encoding and decoding.  It speaks
// the same protocol, backed by this library instead of emulated radio
// firmware.
//
// Protocol (one UDP datagram per 20 ms frame, dispatched on length alone):
//
//	320 bytes: 160 little-endian int16 samples -> reply 7 bytes (49-bit frame)
//	7 bytes:   49-bit frame -> reply 320 bytes of PCM
//	9 bytes:   72-bit on-air frame -> reply 320 bytes of PCM (extension)
//
// Other lengths are ignored.  The 7-byte frame holds bits 0..47 MSB-first in
// bytes 0..5 and bit 48 as 0x80 in byte 6; a non-zero byte 6 reads as 1.
//
// Usage:
//
//	ambe-server [-S 2470] [-host 127.0.0.1] [-state shared|client] [flags]
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	ambe "github.com/nnamon/go-ambe2"
)

func main() {
	port := flag.Int("S", 2470, "UDP port to listen on (md380-emu's -S)")
	host := flag.String("host", "127.0.0.1", "address to listen on; 0.0.0.0 for all interfaces (md380-emu's behaviour)")
	state := flag.String("state", "shared", "vocoder state: shared (one stream, as md380-emu) or client (one per client address)")
	idle := flag.Duration("idle", 30*time.Second, "with -state client: drop a client's state after this long unused")
	maxClients := flag.Int("max-clients", 256, "with -state client: most clients kept (least recently used is dropped)")
	reply72 := flag.Bool("fec", false, "reply to PCM with 9-byte FEC-coded 72-bit frames instead of 7-byte frames")
	lookahead := flag.Int("lookahead", 2, "encoder pitch look-ahead frames: 2 (best quality, 60 ms codec delay) or 1 (39 ms)")
	silence := flag.Bool("silence", true, "send silence frames for non-speech input")
	standard := flag.Bool("standard", false, "decode with the TIA-102.BABA phase model exactly")
	silGain := flag.Float64("silence-gain", ambe.SilenceGain, "decoder amplitude factor for silence frames (1 = standard)")
	verbose := flag.Bool("v", false, "log clients and per-minute statistics")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ambe-server [flags]\n\nA drop-in replacement for md380-emu -S (UDP AMBE+2 vocoder server).\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *state != "shared" && *state != "client" {
		log.Fatalf("ambe-server: -state must be shared or client, not %q", *state)
	}

	enc := ambe.DefaultConfig()
	enc.Lookahead = *lookahead
	enc.Silence = *silence
	cfg := config{
		perClient:  *state == "client",
		idle:       *idle,
		maxClients: *maxClients,
		reply72:    *reply72,
		encoder:    enc,
		decoder:    ambe.DecoderConfig{StandardSynthesis: *standard, SilenceGain: *silGain},
	}
	if cfg.maxClients < 1 {
		cfg.maxClients = 1
	}

	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	conn, err := listen(addr)
	if err != nil {
		log.Fatalf("ambe-server: %v", err)
	}
	log.Printf("ambe-server: listening on udp %s (state %s, look-ahead %d)", conn.LocalAddr(), *state, *lookahead)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sig
		log.Printf("ambe-server: %v, shutting down", s)
		conn.Close()
	}()

	if err := serve(conn, newServer(cfg), *verbose); err != nil {
		log.Fatalf("ambe-server: %v", err)
	}
}

func listen(addr string) (*net.UDPConn, error) {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	return net.ListenUDP("udp", ua)
}

// serve answers datagrams until conn is closed.  Requests are handled in
// arrival order on one goroutine, so each client's replies come back in the
// order of its requests.
func serve(conn *net.UDPConn, s *server, verbose bool) error {
	buf := make([]byte, 2048)
	lastLog := time.Now()
	var logged stats
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		known := s.stats.clients
		reply := s.handle(buf[:n], from.String())
		if reply != nil {
			if _, err := conn.WriteToUDP(reply, from); err != nil && verbose {
				log.Printf("ambe-server: reply to %v: %v", from, err)
			}
		}
		if verbose {
			if s.stats.clients != known {
				log.Printf("ambe-server: new client %v", from)
			}
			if time.Since(lastLog) >= time.Minute {
				st := s.stats
				log.Printf("ambe-server: last minute: %d encoded, %d decoded, %d decoded (72-bit), %d ignored; %d clients seen, %d client states held",
					st.encoded-logged.encoded, st.decoded-logged.decoded, st.decoded72-logged.decoded72,
					st.ignored-logged.ignored, st.clients, len(s.clients))
				logged, lastLog = st, time.Now()
			}
		}
	}
}
