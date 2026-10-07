// Command mbevoc-enc encodes 8 kHz mono 16-bit PCM (raw little-endian, or a
// canonical .wav) into AMBE+2 3600x2450 frames.  The output format follows
// the output file name:
//
//	*.amb    DSD-style .amb file of 49-bit voice frames
//	*.bits   one line of 49 '0'/'1' per frame
//	*.ambe72 9 bytes per frame: the 72-bit FEC-coded, interleaved frame as
//	         carried in DMR voice bursts (first transmitted bit = MSB)
//
// With -codec imbe (the default for *.imb, *.imbe144 and *.pv outputs) it encodes
// IMBE 7200x4400 (P25 Phase 1) frames instead: *.imb (DSD container of 88-bit
// frames), *.bits (88 '0'/'1' per line), *.imbe144 (18-byte coded frames) or
// *.pv (18-byte EDACS ProVoice frames).
// With -codec dstar (the default for *.dmb and *.dv) it encodes D-STAR AMBE
// frames: *.dmb (dsd-fme container), *.bits or *.dv (9-byte voice data).
//
// -draft-layout writes b3 and b4 as the TIA-102.BABA-1 draft's Table 8 places
// them, for decoders that follow it (mbelib, DSD); see package frame.
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

	"github.com/nnamon/mbevoc/fec"
	"github.com/nnamon/mbevoc/frame"
	"github.com/nnamon/mbevoc/p25half"
)

func main() {
	cfg := p25half.DefaultConfig()
	flag.IntVar(&cfg.Lookahead, "lookahead", cfg.Lookahead, "pitch-tracking look-ahead frames (0..2)")
	flag.IntVar(&cfg.RefineMinBin, "refine-min-bin", cfg.RefineMinBin, "first DFT bin of the pitch refinement error")
	flag.BoolVar(&cfg.MatchedAmplitudes, "matched-amps", cfg.MatchedAmplitudes, "estimate amplitudes per final voicing")
	flag.BoolVar(&cfg.Silence, "silence", cfg.Silence, "send silence frames for non-speech")
	flag.Float64Var(&cfg.SilenceAttenuation, "silence-atten", cfg.SilenceAttenuation, "comfort-noise attenuation in silence frames (dB)")
	flag.Float64Var(&cfg.GainOffset, "gain", cfg.GainOffset, "log2 gain offset added to all amplitudes")
	flag.Float64Var(&cfg.VoicingScale, "vscale", cfg.VoicingScale, "V/UV threshold scale (0 = 1)")
	flag.Float64Var(&cfg.WeightPower, "wpow", cfg.WeightPower, "quantizer amplitude-weighting power (0 = unweighted)")
	trace := flag.Bool("trace", false, "print per-frame analysis to stderr")
	codec := flag.String("codec", "", "ambe2, imbe or dstar (default: by output file name)")
	draft := flag.Bool("draft-layout", false, "place b3/b4 bits as the draft standard's Table 8 does (for mbelib/DSD), not as DVSI radios do")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: mbevoc-enc [flags] in.raw|in.wav out.amb|out.bits|out.ambe72|out.imb|out.imbe144|out.pv|out.dmb|out.dv\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if *codec == "" {
		*codec = "ambe2"
		if o := flag.Arg(1); strings.HasSuffix(o, ".imb") || strings.HasSuffix(o, ".imbe144") || strings.HasSuffix(o, ".pv") {
			*codec = "imbe"
		} else if strings.HasSuffix(o, ".dmb") || strings.HasSuffix(o, ".dv") {
			*codec = "dstar"
		}
	}
	var err error
	switch *codec {
	case "ambe2":
		err = run(cfg, flag.Arg(0), flag.Arg(1), *trace, *draft)
	case "imbe", "dstar":
		var f *os.File
		var r *bufio.Reader
		if f, r, err = openPCM(flag.Arg(0)); err == nil {
			if *codec == "imbe" {
				err = encodeIMBE(r, flag.Arg(1), cfg.Lookahead)
			} else {
				err = encodeDStar(r, flag.Arg(1), cfg.Lookahead)
			}
			f.Close()
		}
	default:
		err = fmt.Errorf("unknown codec %q", *codec)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mbevoc-enc:", err)
		os.Exit(1)
	}
}

func openPCM(in string) (*os.File, *bufio.Reader, error) {
	f, err := os.Open(in)
	if err != nil {
		return nil, nil, err
	}
	r := bufio.NewReader(f)
	if strings.HasSuffix(strings.ToLower(in), ".wav") {
		if err := skipWAVHeader(r); err != nil {
			f.Close()
			return nil, nil, err
		}
	}
	return f, r, nil
}

func run(cfg p25half.Config, in, out string, trace, draft bool) error {
	f, r, err := openPCM(in)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := p25half.NewEncoderConfig(cfg)
	var frames []frame.Bits
	var pcm [p25half.FrameSamples]int16
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
	if draft {
		for i := range frames {
			if !p25half.IsTone(&frames[i]) {
				frames[i] = frames[i].ToDraftLayout()
			}
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
