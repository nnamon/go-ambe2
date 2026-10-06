// Command ambe-params decodes AMBE+2 3600x2450 frames (.amb or .bits) into
// MBE model parameters, one line per frame:
//
//	index kind b0..b8 f0(Hz) L gamma voicing log2M[1..L]
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nnamon/go-ambe2/frame"
	"github.com/nnamon/go-ambe2/quant"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ambe-params in.amb|in.bits")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	var frames []frame.Bits
	if strings.HasSuffix(os.Args[1], ".bits") {
		frames, err = frame.ReadBitsText(f)
	} else {
		frames, err = frame.ReadAMB(f)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	kinds := map[quant.Kind]string{quant.Voice: "V", quant.Silence: "S", quant.Erasure: "E", quant.Tone: "T"}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	p := quant.NewPredictor()
	for i := range frames {
		b := frames[i].Params()
		m, k := p.Dequantize(b)
		fmt.Fprintf(w, "%d %s", i, kinds[k])
		for _, v := range b {
			fmt.Fprintf(w, " %d", v)
		}
		if k == quant.Voice || k == quant.Silence {
			fmt.Fprintf(w, " %.2f %d %.4f ", m.W0/(2*3.141592653589793)*8000, m.L, m.Gamma)
			for l := 1; l <= m.L; l++ {
				if m.Voiced[l] {
					w.WriteByte('1')
				} else {
					w.WriteByte('0')
				}
			}
			for l := 1; l <= m.L; l++ {
				fmt.Fprintf(w, " %.4f", m.Log2M[l])
			}
		}
		w.WriteByte('\n')
	}
}
