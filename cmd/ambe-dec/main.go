// Command ambe-dec decodes AMBE+2 3600x2450 frames to 8 kHz mono 16-bit PCM.
// The input format follows the file name: *.amb (DSD-style 49-bit frames),
// *.bits (49 '0'/'1' per line) or *.ambe72 (9-byte FEC-coded DMR frames).
// The output is raw little-endian PCM, or a .wav file if the name ends in .wav.
//
// -draft-layout reads 49-bit frames whose b3 and b4 follow the TIA-102.BABA-1
// draft's Table 8, as OP25's encoder writes them; see package frame.
//
// With -codec imbe (the default for *.imb, *.imbe144 and *.pv inputs) it decodes
// IMBE 7200x4400 (P25 Phase 1) frames: *.imb, *.bits (88 '0'/'1' per line) or
// *.imbe144 or *.pv (18-byte P25 or EDACS ProVoice coded frames, with error
// correction).  With -codec dstar
// (the default for *.dmb and *.dv) it decodes D-STAR AMBE frames: *.dmb,
// *.bits or *.dv (9-byte voice data, with error correction).
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

var (
	standard = flag.Bool("standard", false, "use the TIA-102.BABA phase model exactly")
	silGain  = flag.Float64("silence-gain", ambe.SilenceGain, "amplitude factor for silence frames (1 = standard)")
	noEnh    = flag.Bool("no-enhance", false, "disable spectral amplitude enhancement")
	codec    = flag.String("codec", "", "ambe2, imbe or dstar (default: by input file name)")
	noSmooth = flag.Bool("no-smoothing", false, "IMBE: disable adaptive smoothing")
	draft    = flag.Bool("draft-layout", false, "49-bit input places b3/b4 bits as the draft standard's Table 8 does (OP25's encoder)")
)

func main() {
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: ambe-dec [flags] in.amb|in.bits|in.ambe72|in.imb|in.imbe144|in.dmb|in.dv out.raw|out.wav")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Arg(1)); err != nil {
		fmt.Fprintln(os.Stderr, "ambe-dec:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	if *codec == "" {
		*codec = "ambe2"
		if strings.HasSuffix(in, ".imb") || strings.HasSuffix(in, ".imbe144") || strings.HasSuffix(in, ".pv") {
			*codec = "imbe"
		} else if strings.HasSuffix(in, ".dmb") || strings.HasSuffix(in, ".dv") {
			*codec = "dstar"
		}
	}
	var pcm []int16
	switch *codec {
	case "imbe":
		pcm, err = decodeIMBE(f, in)
	case "dstar":
		pcm, err = decodeDStar(f, in)
	case "ambe2":
		pcm, err = decodeAMBE2(f, in)
	default:
		err = fmt.Errorf("unknown codec %q", *codec)
	}
	if err != nil {
		return err
	}
	return writePCM(out, pcm)
}

func decodeAMBE2(f *os.File, in string) ([]int16, error) {
	var err error
	dec := ambe.NewDecoderConfig(ambe.DecoderConfig{
		StandardSynthesis: *standard,
		SilenceGain:       *silGain,
		NoEnhancement:     *noEnh,
	})
	var pcm []int16
	corrected := 0
	switch {
	case strings.HasSuffix(in, ".ambe72") && *draft:
		return nil, errors.New("-draft-layout applies to .amb and .bits input only")
	case strings.HasSuffix(in, ".ambe72"):
		r := bufio.NewReader(f)
		for {
			var b [9]byte
			if _, err := io.ReadFull(r, b[:]); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			c := fec.Unpack(b)
			s, e := dec.Decode72(&c)
			corrected += e.Total()
			pcm = append(pcm, s[:]...)
		}
	default:
		var frames []frame.Bits
		if strings.HasSuffix(in, ".bits") {
			frames, err = frame.ReadBitsText(f)
		} else {
			frames, err = frame.ReadAMB(f)
		}
		if err != nil {
			return nil, err
		}
		for i := range frames {
			if *draft && !ambe.IsTone(&frames[i]) {
				frames[i] = frame.FromDraftLayout(frames[i])
			}
			s := dec.Decode(&frames[i])
			pcm = append(pcm, s[:]...)
		}
	}
	fmt.Fprintf(os.Stderr, "%d bits corrected; ", corrected)
	return pcm, nil
}

func writePCM(out string, pcm []int16) error {
	o, err := os.Create(out)
	if err != nil {
		return err
	}
	defer o.Close()
	w := bufio.NewWriter(o)
	if strings.HasSuffix(strings.ToLower(out), ".wav") {
		writeWAVHeader(w, len(pcm))
	}
	if err := binary.Write(w, binary.LittleEndian, pcm); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "decoded %d frames\n", len(pcm)/ambe.FrameSamples)
	return o.Close()
}

func writeWAVHeader(w io.Writer, samples int) {
	data := uint32(2 * samples)
	h := []any{
		[4]byte{'R', 'I', 'F', 'F'}, 36 + data, [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, data,
	}
	for _, v := range h {
		binary.Write(w, binary.LittleEndian, v)
	}
}
