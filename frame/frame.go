// Package frame handles the AMBE+2 3600x2450 (DMR/NXDN/P25p2 half-rate) 49-bit
// voice-parameter frame: the mapping between the transmitted bit order and the
// nine quantizer indices b0..b8, and the DSD-style ".amb" file container.
//
// The bit order is the one used by mbelib's ambe_d[49] (mbe_decodeAmbe2450Parms),
// OP25's encode_49bit, DSD's .amb files and the MD-380 firmware's 49-short buffers.
// Positions 0..11 form FEC vector C0 (Golay 24,12), 12..23 C1 (Golay 23,12 +
// PRNG whitening), 24..34 C2 and 35..48 C3 (both unprotected).
package frame

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Params holds the quantizer indices b0..b8.
//
//	b0 fundamental frequency (7 bits; 120..127 are erasure/silence/tone codes)
//	b1 voicing decisions     (5 bits)
//	b2 gain delta            (5 bits)
//	b3 PRBA 2..4             (9 bits)
//	b4 PRBA 5..8             (7 bits)
//	b5..b8 higher-order DCT coefficients (5, 4, 4, 3 bits)
type Params [9]uint16

// Widths gives the bit width of each of b0..b8.
var Widths = [9]int{7, 5, 5, 9, 7, 5, 4, 4, 3}

// Bits is a frame as 49 bits (each 0 or 1) in transmission order.
type Bits [49]uint8

type slot struct{ field, sig uint8 }

// layout[pos] says which field and bit significance position pos carries.
var layout [49]slot

func init() {
	for _, f := range []struct {
		field    uint8
		msb, lsb []int
	}{
		{0, []int{0, 1, 2, 3}, []int{37, 38, 39}},
		{1, []int{4, 5, 6, 7}, []int{35}},
		{2, []int{8, 9, 10, 11}, []int{36}},
		{3, []int{12, 13, 14, 15, 16, 17, 18, 19}, []int{40}},
		{4, []int{20, 21, 22, 23}, []int{41, 42, 43}},
		{5, []int{24, 25, 26, 27}, []int{44}},
		{6, []int{28, 29, 30}, []int{45}},
		{7, []int{31, 32, 33}, []int{46}},
		{8, []int{34}, []int{47, 48}},
	} {
		w := Widths[f.field]
		for i, pos := range append(append([]int{}, f.msb...), f.lsb...) {
			layout[pos] = slot{f.field, uint8(w - 1 - i)}
		}
	}
}

// Params extracts b0..b8.
func (b *Bits) Params() Params {
	var p Params
	for pos, s := range layout {
		p[s.field] |= uint16(b[pos]&1) << s.sig
	}
	return p
}

// Bits packs b0..b8 into transmission order. Out-of-range values are masked.
func (p Params) Bits() Bits {
	var b Bits
	for pos, s := range layout {
		b[pos] = uint8(p[s.field]>>s.sig) & 1
	}
	return b
}

func (b Bits) String() string {
	var sb strings.Builder
	for _, v := range b {
		sb.WriteByte('0' + v&1)
	}
	return sb.String()
}

// ParseBits parses a 49-character string of '0'/'1'.
func ParseBits(s string) (Bits, error) {
	var b Bits
	if len(s) != 49 {
		return b, fmt.Errorf("frame: want 49 bits, got %d chars", len(s))
	}
	for i := range s {
		switch s[i] {
		case '0':
		case '1':
			b[i] = 1
		default:
			return b, fmt.Errorf("frame: bad bit char %q at %d", s[i], i)
		}
	}
	return b, nil
}

// AMBMagic is the 4-byte header of a DSD .amb file.
const AMBMagic = ".amb"

// Pack8 encodes a frame as an 8-byte .amb record: a status byte (0 = OK),
// bits 0..47 MSB-first in bytes 1..6, and bit 48 in the LSB of byte 7.
func (b *Bits) Pack8(status byte) [8]byte {
	var out [8]byte
	out[0] = status
	for i := 0; i < 48; i++ {
		out[1+i/8] |= (b[i] & 1) << (7 - i%8)
	}
	out[7] = b[48] & 1
	return out
}

// Unpack8 decodes an 8-byte .amb record, returning the frame and status byte.
func Unpack8(rec [8]byte) (Bits, byte) {
	var b Bits
	for i := 0; i < 48; i++ {
		b[i] = (rec[1+i/8] >> (7 - i%8)) & 1
	}
	b[48] = rec[7] & 1
	return b, rec[0]
}

// ReadAMB reads a whole .amb stream. A trailing partial record is an error.
func ReadAMB(r io.Reader) ([]Bits, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("frame: reading .amb header: %w", err)
	}
	if string(hdr[:]) != AMBMagic {
		return nil, fmt.Errorf("frame: bad .amb magic %q", hdr[:])
	}
	var frames []Bits
	br := bufio.NewReader(r)
	for {
		var rec [8]byte
		_, err := io.ReadFull(br, rec[:])
		if errors.Is(err, io.EOF) {
			return frames, nil
		}
		if err != nil {
			return frames, fmt.Errorf("frame: truncated .amb record %d: %w", len(frames), err)
		}
		b, _ := Unpack8(rec)
		frames = append(frames, b)
	}
}

// WriteAMB writes frames as a .amb stream with status 0.
func WriteAMB(w io.Writer, frames []Bits) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(AMBMagic)
	for i := range frames {
		rec := frames[i].Pack8(0)
		bw.Write(rec[:])
	}
	return bw.Flush()
}

// ReadBitsText reads the one-frame-per-line '0'/'1' text format.
func ReadBitsText(r io.Reader) ([]Bits, error) {
	var frames []Bits
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := string(bytes.TrimSpace(sc.Bytes()))
		if line == "" {
			continue
		}
		b, err := ParseBits(line)
		if err != nil {
			return frames, fmt.Errorf("line %d: %w", len(frames)+1, err)
		}
		frames = append(frames, b)
	}
	return frames, sc.Err()
}
