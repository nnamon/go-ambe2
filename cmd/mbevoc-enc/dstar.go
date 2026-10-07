package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nnamon/mbevoc/dstar"
)

// encodeDStar encodes PCM from r into D-STAR AMBE frames written to out:
// *.dmb (dsd-fme container of 49-bit frames), *.bits (49 '0'/'1' per line) or
// *.dv (9 bytes per frame: the 72-bit coded frame as D-STAR voice data).
func encodeDStar(r *bufio.Reader, out string, lookahead int) error {
	cfg := dstar.DefaultConfig()
	cfg.Lookahead = lookahead
	enc := dstar.NewEncoderConfig(cfg)
	var frames []dstar.Bits
	var pcm [dstar.FrameSamples]int16
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
	case strings.HasSuffix(out, ".dv"):
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
		if err := dstar.WriteDMB(w, frames); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "encoded %d D-STAR frames (delay %d samples)\n", len(frames), enc.Delay())
	return o.Close()
}
