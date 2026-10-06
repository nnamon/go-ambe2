package frame

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestLayoutIsPermutation(t *testing.T) {
	seen := map[slot]bool{}
	total := 0
	for _, w := range Widths {
		total += w
	}
	if total != 49 {
		t.Fatalf("widths sum to %d", total)
	}
	for pos, s := range layout {
		if int(s.sig) >= Widths[s.field] {
			t.Errorf("pos %d: sig %d out of range for b%d", pos, s.sig, s.field)
		}
		if seen[s] {
			t.Errorf("pos %d: duplicate %+v", pos, s)
		}
		seen[s] = true
	}
}

func TestParamsRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 10000; n++ {
		var p Params
		for i, w := range Widths {
			p[i] = uint16(r.Intn(1 << w))
		}
		b := p.Bits()
		if got := b.Params(); got != p {
			t.Fatalf("round trip %v -> %v", p, got)
		}
		rec := b.Pack8(0)
		b2, st := Unpack8(rec)
		if b2 != b || st != 0 {
			t.Fatalf("pack8 round trip")
		}
	}
}

// refsDir locates the reference sources cloned by research/setup.sh.
func refsDir(t *testing.T) string {
	d, _ := filepath.Abs("../research/refs")
	if _, err := os.Stat(d); err != nil {
		t.Skip("reference sources not present at", d)
	}
	return d
}

// draftLayout is the draft Table 8 layout as seen through FromDraftLayout:
// a bit at position pos of a draft-layout frame lands where FromDraftLayout
// puts it, and means what our layout says there.
func draftLayout() (d [49]slot) {
	for pos := range d {
		var b Bits
		b[pos] = 1
		c := FromDraftLayout(b)
		for q := range c {
			if c[q] == 1 {
				d[pos] = layout[q]
			}
		}
	}
	return d
}

func TestDraftLayoutConversion(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for n := 0; n < 1000; n++ {
		var b Bits
		for i := range b {
			b[i] = uint8(r.Intn(2))
		}
		if FromDraftLayout(b.ToDraftLayout()) != b || FromDraftLayout(b).ToDraftLayout() != b {
			t.Fatal("conversions are not inverses")
		}
	}
	d := draftLayout()
	for pos := range d {
		want := layout[pos]
		switch pos {
		case 40:
			want = slot{3, 0}
		case 41, 42, 43:
			want = slot{4, uint8(43 - pos)}
		}
		if d[pos] != want {
			t.Errorf("draft pos %d = %+v, want %+v", pos, d[pos], want)
		}
	}
}

// TestLayoutMatchesMbelib parses the bit assignments straight out of mbelib's
// mbe_decodeAmbe2450Parms and OP25's encode_49bit and checks that they are
// the draft Table 8 layout, which differs from ours only in b3 and b4's low
// bits (see the package comment).
func TestLayoutMatchesMbelib(t *testing.T) {
	refs := refsDir(t)
	layout := draftLayout()
	src, err := os.ReadFile(filepath.Join(refs, "mbelib/ambe3600x2450.c"))
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(src, []byte("mbe_decodeAmbe2450Parms (char"))
	j := bytes.Index(src[i:], []byte("\nmbe_"))
	fn := src[i : i+j]
	re := regexp.MustCompile(`\bb(\d)\s*\|=\s*ambe_d\[(\d+)\](?:\s*<<\s*(\d+))?\s*;`)
	got := map[int]slot{}
	for _, m := range re.FindAllSubmatch(fn, -1) {
		f, _ := strconv.Atoi(string(m[1]))
		pos, _ := strconv.Atoi(string(m[2]))
		sig := 0
		if len(m[3]) > 0 {
			sig, _ = strconv.Atoi(string(m[3]))
		}
		got[pos] = slot{uint8(f), uint8(sig)}
	}
	if len(got) != 49 {
		t.Fatalf("parsed %d positions from mbelib", len(got))
	}
	for pos := 0; pos < 49; pos++ {
		if got[pos] != layout[pos] {
			t.Errorf("mbelib pos %d = %+v, ours %+v", pos, got[pos], layout[pos])
		}
	}

	op25, err := os.ReadFile(filepath.Join(refs, "op25/op25/gr-op25_repeater/lib/ambe_encoder.cc"))
	if err != nil {
		t.Fatal(err)
	}
	re2 := regexp.MustCompile(`outp\[(\d+)\]\s*=\s*\(?b\[(\d)\](?:\s*>>\s*(\d+))?\)?\s*&\s*1;`)
	n := 0
	for _, m := range re2.FindAllSubmatch(op25, -1) {
		pos, _ := strconv.Atoi(string(m[1]))
		f, _ := strconv.Atoi(string(m[2]))
		sig := 0
		if len(m[3]) > 0 {
			sig, _ = strconv.Atoi(string(m[3]))
		}
		if pos < 49 {
			n++
			if (slot{uint8(f), uint8(sig)}) != layout[pos] {
				t.Errorf("op25 pos %d = b%d bit %d, ours %+v", pos, f, sig, layout[pos])
			}
		}
	}
	if n != 49 {
		t.Errorf("parsed %d positions from op25", n)
	}
}

// TestAMBMatchesBitsText checks that .amb and .bits renderings of the same
// reference bitstream (written independently by research/oracle) agree, and
// that WriteAMB reproduces the .amb file byte for byte.
func TestAMBMatchesBitsText(t *testing.T) {
	dir, _ := filepath.Abs("../research/testdata/speech")
	af, err := os.Open(filepath.Join(dir, "oracle.amb"))
	if err != nil {
		t.Skip("no oracle output:", err)
	}
	defer af.Close()
	tf, err := os.Open(filepath.Join(dir, "oracle.bits"))
	if err != nil {
		t.Skip(err)
	}
	defer tf.Close()
	a, err := ReadAMB(af)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReadBitsText(tf)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("frame counts %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("frame %d differs", i)
		}
	}
	var buf bytes.Buffer
	if err := WriteAMB(&buf, a); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(filepath.Join(dir, "oracle.amb"))
	if !bytes.Equal(buf.Bytes(), orig) {
		t.Fatal("WriteAMB does not reproduce oracle .amb byte-for-byte")
	}
}

func TestPack7(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for n := 0; n < 5000; n++ {
		var b Bits
		for i := range b {
			b[i] = uint8(r.Intn(2))
		}
		p := b.Pack7()
		if p[6]&0x7F != 0 || (p[6] == 0x80) != (b[48] == 1) {
			t.Fatalf("byte 6 = %#x for bit 48 = %d", p[6], b[48])
		}
		if Unpack7(p) != b {
			t.Fatal("Pack7/Unpack7 round trip failed")
		}
		// The 7-byte form is the .amb record without its status byte, except
		// for where bit 48 sits.
		rec := b.Pack8(0)
		for i := 0; i < 6; i++ {
			if p[i] != rec[1+i] {
				t.Fatalf("byte %d differs from the .amb record", i)
			}
		}
	}
	// md380-emu reads any non-zero byte 6 as a one.
	var p [7]byte
	p[6] = 0x01
	if Unpack7(p)[48] != 1 {
		t.Fatal("byte 6 = 0x01 must decode as bit 48 = 1")
	}
}
