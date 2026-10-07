package p25full

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// IMBMagic is the 4-byte header of a DSD .imb file.
const IMBMagic = ".imb"

// ReadIMB reads a DSD .imb stream: the header, then per frame a status byte
// (DSD writes its error count there) and the 88 bits MSB-first in 11 bytes.
// A trailing partial record is an error.
func ReadIMB(r io.Reader) ([]Bits, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("imbe: reading .imb header: %w", err)
	}
	if string(hdr[:]) != IMBMagic {
		return nil, fmt.Errorf("imbe: bad .imb magic %q", hdr[:])
	}
	br := bufio.NewReader(r)
	var frames []Bits
	for {
		var rec [12]byte
		_, err := io.ReadFull(br, rec[:])
		if errors.Is(err, io.EOF) {
			return frames, nil
		}
		if err != nil {
			return frames, fmt.Errorf("imbe: truncated .imb record %d: %w", len(frames), err)
		}
		var b Bits
		for i := range b {
			b[i] = (rec[1+i/8] >> (7 - uint(i%8))) & 1
		}
		frames = append(frames, b)
	}
}

// WriteIMB writes frames as a .imb stream with status 0.
func WriteIMB(w io.Writer, frames []Bits) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(IMBMagic)
	for _, b := range frames {
		var rec [12]byte
		for i, v := range b {
			rec[1+i/8] |= (v & 1) << (7 - uint(i%8))
		}
		bw.Write(rec[:])
	}
	return bw.Flush()
}
