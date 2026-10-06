// Command ambe-enc encodes 8 kHz mono 16-bit PCM (raw little-endian, or a
// canonical .wav) into AMBE+2 3600x2450 frames.  The output format follows
// the output file name:
//
//	*.amb    DSD-style .amb file of 49-bit voice frames
//	*.bits   one line of 49 '0'/'1' per frame
//	*.ambe72 9 bytes per frame: the 72-bit FEC-coded, interleaved frame as
//	         carried in DMR voice bursts (first transmitted bit = MSB)
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	ambe "github.com/nnamon/go-ambe2"
	"github.com/nnamon/go-ambe2/fec"
	"github.com/nnamon/go-ambe2/frame"
)

func main() {
	cfg := ambe.DefaultConfig()
	flag.IntVar(&cfg.Lookahead, "lookahead", cfg.Lookahead, "pitch-tracking look-ahead frames (0..2)")
	flag.IntVar(&cfg.RefineMinBin, "refine-min-bin", cfg.RefineMinBin, "first DFT bin of the pitch refinement error")
	flag.BoolVar(&cfg.MatchedAmplitudes, "matched-amps", cfg.MatchedAmplitudes, "estimate amplitudes per final voicing")
	flag.BoolVar(&cfg.Silence, "silence", cfg.Silence, "send silence frames for non-speech")
	flag.Float64Var(&cfg.SilenceAttenuation, "silence-atten", cfg.SilenceAttenuation, "comfort-noise attenuation in silence frames (dB)")
	flag.Float64Var(&cfg.GainOffset, "gain", cfg.GainOffset, "log2 gain offset added to all amplitudes")
	flag.Float64Var(&cfg.VoicingScale, "vscale", cfg.VoicingScale, "V/UV threshold scale (0 = 1)")
	flag.Float64Var(&cfg.WeightPower, "wpow", cfg.WeightPower, "quantizer amplitude-weighting power (0 = unweighted)")
	trace := flag.Bool("trace", false, "print per-frame analysis to stderr")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ambe-enc [flags] in.raw|in.wav out.amb|out.bits|out.ambe72\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(cfg, flag.Arg(0), flag.Arg(1), *trace); err != nil {
		fmt.Fprintln(os.Stderr, "ambe-enc:", err)
		os.Exit(1)
	}
}

func run(cfg ambe.Config, in, out string, trace bool) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	if strings.HasSuffix(strings.ToLower(in), ".wav") {
		if err := skipWAVHeader(r); err != nil {
			return err
		}
	}
	enc := ambe.NewEncoderConfig(cfg)
	var frames []frame.Bits
	var pcm [ambe.FrameSamples]int16
	for {
		if err := binary.Read(r, binary.LittleEndian, &pcm); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return err
		}
		frames = append(frames, enc.Encode(&pcm))
		if trace {
			a := enc.Last
			fmt.Fprintf(os.Stderr, "%d PI=%.1f EI=%.3f w0=%.4f xi0=%.0f sil=%v b=%v\n",
				len(frames)-1, a.PInit, a.EInit, a.W0, a.Xi0, a.Silence, a.Params)
		}
	}
	o, err := os.Create(out)
	if err != nil {
		return err
	}
	defer o.Close()
	if strings.HasSuffix(out, ".ambe72") {
		w := bufio.NewWriter(o)
		for i := range frames {
			c := fec.Encode(&frames[i])
			p := fec.Pack(&c)
			w.Write(p[:])
		}
		if err := w.Flush(); err != nil {
			return err
		}
	} else if strings.HasSuffix(out, ".bits") {
		w := bufio.NewWriter(o)
		for _, b := range frames {
			fmt.Fprintln(w, b.String())
		}
		if err := w.Flush(); err != nil {
			return err
		}
	} else if err := frame.WriteAMB(o, frames); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "encoded %d frames (delay %d samples)\n", len(frames), enc.Delay())
	return o.Close()
}

// skipWAVHeader advances r to the start of the "data" chunk of a RIFF/WAVE
// file and checks it is 8 kHz mono 16-bit PCM.
func skipWAVHeader(r *bufio.Reader) error {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return errors.New("not a RIFF/WAVE file")
	}
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return err
		}
		size := binary.LittleEndian.Uint32(ch[4:])
		switch string(ch[0:4]) {
		case "fmt ":
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return err
			}
			format, chans := binary.LittleEndian.Uint16(b[0:]), binary.LittleEndian.Uint16(b[2:])
			rate, bitsPer := binary.LittleEndian.Uint32(b[4:]), binary.LittleEndian.Uint16(b[14:])
			if format != 1 || chans != 1 || rate != 8000 || bitsPer != 16 {
				return fmt.Errorf("need 8 kHz mono 16-bit PCM, got format=%d ch=%d rate=%d bits=%d", format, chans, rate, bitsPer)
			}
		case "data":
			return nil
		default:
			if _, err := r.Discard(int(size + size&1)); err != nil {
				return err
			}
		}
	}
}
