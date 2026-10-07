package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nnamon/mbevoc/p25full"
)

// encodeIMBE encodes PCM from r into IMBE 7200x4400 frames written to out:
// *.imb (DSD container of 88-bit frames), *.bits (88 '0'/'1' per line) or
// *.imbe144 (18 bytes per frame: the 144-bit coded frame, first bit in the MSB)
// or *.pv (18 bytes per frame: the 142-bit EDACS ProVoice frame).
func encodeIMBE(r *bufio.Reader, out string, lookahead int) error {
	cfg := p25full.DefaultConfig()
	cfg.Lookahead = lookahead
	enc := p25full.NewEncoderConfig(cfg)
	var frames []p25full.Bits
	var pcm [p25full.FrameSamples]int16
	for {
		if err := binary.Read(r, binary.LittleEndian, &pcm); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return err
		}
		frames = append(frames, enc.Encode(&pcm))
	}
	o, err := os.Create(out)
	if err != nil {
		return err
	}
	defer o.Close()
	w := bufio.NewWriter(o)
	switch {
	case strings.HasSuffix(out, ".pv"):
		for i := range frames {
			f := frames[i].EncodeProVoice()
			p := f.Pack()
			w.Write(p[:])
		}
	case strings.HasSuffix(out, ".imbe144"):
		for i := range frames {
			f := frames[i].Encode()
			p := f.Pack()
			w.Write(p[:])
		}
	case strings.HasSuffix(out, ".bits"):
		for _, b := range frames {
			for _, v := range b {
				w.WriteByte('0' + v&1)
			}
			w.WriteByte('\n')
		}
	default:
		if err := p25full.WriteIMB(w, frames); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "encoded %d IMBE frames (delay %d samples)\n", len(frames), enc.Delay())
	return o.Close()
}
