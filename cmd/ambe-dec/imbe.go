package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nnamon/go-ambe2/imbe"
)

// decodeIMBE decodes IMBE 7200x4400 frames from *.imb, *.bits (88 '0'/'1'
// per line), *.imbe144 (18-byte coded frames) or *.pv (18-byte EDACS
// ProVoice frames) into PCM.
func decodeIMBE(f *os.File, in string) ([]int16, error) {
	dec := imbe.NewDecoderConfig(imbe.DecoderConfig{
		StandardSynthesis: *standard,
		NoEnhancement:     *noEnh,
		NoSmoothing:       *noSmooth,
	})
	var pcm []int16
	switch {
	case strings.HasSuffix(in, ".imbe144"), strings.HasSuffix(in, ".pv"):
		pv := strings.HasSuffix(in, ".pv")
		r := bufio.NewReader(f)
		corrected := 0
		for {
			var p [18]byte
			if _, err := io.ReadFull(r, p[:]); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			var s [imbe.FrameSamples]int16
			var e imbe.Errors
			if pv {
				fr := imbe.UnpackProVoice(p)
				s, e = dec.DecodeProVoice(&fr)
			} else {
				fr := imbe.Unpack(p)
				s, e = dec.DecodeFrame(&fr)
			}
			corrected += e.Total()
			pcm = append(pcm, s[:]...)
		}
		fmt.Fprintf(os.Stderr, "%d bits corrected; ", corrected)
	case strings.HasSuffix(in, ".bits"):
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if len(line) != 88 {
				return nil, fmt.Errorf("want 88 bits per line, got %d", len(line))
			}
			var b imbe.Bits
			for i := range b {
				b[i] = line[i] - '0'
			}
			s := dec.Decode(&b)
			pcm = append(pcm, s[:]...)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	default:
		frames, err := imbe.ReadIMB(f)
		if err != nil {
			return nil, err
		}
		for i := range frames {
			s := dec.Decode(&frames[i])
			pcm = append(pcm, s[:]...)
		}
	}
	return pcm, nil
}
