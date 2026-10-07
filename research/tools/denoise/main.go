// Command denoise runs the encoders' noise suppressor (internal/denoise) on
// its own: 8 kHz 16-bit raw PCM in, the same out, time-aligned with the
// input (the suppressor's delay removed).  For research/tools/eval_noise.py.
//
//	denoise [-floor dB] in.raw out.raw
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"

	"github.com/nnamon/mbevoc/internal/denoise"
)

func main() {
	cfg := denoise.DefaultConfig()
	flag.Float64Var(&cfg.Floor, "floor", cfg.Floor, "gain floor (dB)")
	flag.Float64Var(&cfg.Bias, "bias", cfg.Bias, "noise estimate scale")
	flag.Float64Var(&cfg.Delta, "delta", cfg.Delta, "speech presence threshold (power ratio to the minimum)")
	flag.Float64Var(&cfg.AlphaD, "alpha-d", cfg.AlphaD, "noise averaging factor")
	flag.IntVar(&cfg.MinWindow, "min-window", cfg.MinWindow, "minimum-tracking sub-window (10 ms frames)")
	flag.Float64Var(&cfg.AlphaDD, "alpha-dd", cfg.AlphaDD, "decision-directed smoothing")
	flag.Float64Var(&cfg.Full, "full", cfg.Full, "long-term SNR (dB) at or below which suppression is full")
	flag.Float64Var(&cfg.None, "none", cfg.None, "long-term SNR (dB) at or above which there is no suppression")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: denoise [-floor dB] in.raw out.raw")
		os.Exit(2)
	}
	raw, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	n := len(raw) / 2
	x := make([]float64, (n+denoise.Delay+79)/80*80)
	for i := 0; i < n; i++ {
		x[i] = float64(int16(binary.LittleEndian.Uint16(raw[2*i:])))
	}
	denoise.New(cfg).Process(x)
	out := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		v := math.Max(-32768, math.Min(32767, math.Round(x[i+denoise.Delay])))
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(v)))
	}
	if err := os.WriteFile(flag.Arg(1), out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
