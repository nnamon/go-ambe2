// Command ambe-dec decodes AMBE+2 3600x2450 frames to 8 kHz mono 16-bit PCM.
// The input format follows the file name: *.amb (DSD-style 49-bit frames),
// *.bits (49 '0'/'1' per line) or *.ambe72 (9-byte FEC-coded DMR frames).
// The output is raw little-endian PCM, or a .wav file if the name ends in .wav.
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
)

func main() {
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: ambe-dec [flags] in.amb|in.bits|in.ambe72 out.raw|out.wav")
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
	dec := ambe.NewDecoderConfig(ambe.DecoderConfig{
		StandardSynthesis: *standard,
		SilenceGain:       *silGain,
		NoEnhancement:     *noEnh,
	})
	var pcm []int16
	corrected := 0
	switch {
	case strings.HasSuffix(in, ".ambe72"):
		r := bufio.NewReader(f)
		for {
			var b [9]byte
			if _, err := io.ReadFull(r, b[:]); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return err
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
			return err
		}
		for i := range frames {
			s := dec.Decode(&frames[i])
			pcm = append(pcm, s[:]...)
		}
	}
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
	fmt.Fprintf(os.Stderr, "decoded %d frames (%d bits corrected)\n", len(pcm)/ambe.FrameSamples, corrected)
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
