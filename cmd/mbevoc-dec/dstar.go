package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nnamon/mbevoc/dstar"
)

// decodeDStar decodes D-STAR AMBE frames from *.dmb, *.bits (49 '0'/'1' per
// line) or *.dv (9-byte voice data frames, with error correction) into PCM.
func decodeDStar(f *os.File, in string) ([]int16, error) {
	dec := dstar.NewDecoderConfig(dstar.DecoderConfig{StandardSynthesis: *standard, NoEnhancement: *noEnh})
	var pcm []int16
	switch {
	case strings.HasSuffix(in, ".dv"):
		r := bufio.NewReader(f)
		corrected := 0
		for {
			var p [9]byte
			if _, err := io.ReadFull(r, p[:]); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			fr := dstar.Unpack(p)
			s, e := dec.DecodeFrame(&fr)
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
			if len(line) != 49 {
				return nil, fmt.Errorf("want 49 bits per line, got %d", len(line))
			}
			var b dstar.Bits
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
		frames, err := dstar.ReadDMB(f)
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
