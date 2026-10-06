// Package dsp holds the small signal-processing primitives the encoder needs.
package dsp

import (
	"math"
	"math/bits"
	"math/cmplx"
)

// FFT is a precomputed radix-2 complex FFT of a fixed power-of-two size.
type FFT struct {
	n       int
	twiddle []complex128
	rev     []int
}

// NewFFT prepares an FFT of size n (a power of two).
func NewFFT(n int) *FFT {
	if n < 2 || n&(n-1) != 0 {
		panic("dsp: FFT size must be a power of two")
	}
	f := &FFT{n: n, twiddle: make([]complex128, n/2), rev: make([]int, n)}
	for k := range f.twiddle {
		f.twiddle[k] = cmplx.Rect(1, -2*math.Pi*float64(k)/float64(n))
	}
	shift := 64 - bits.TrailingZeros(uint(n))
	for i := range f.rev {
		f.rev[i] = int(bits.Reverse64(uint64(i)) >> shift)
	}
	return f
}

// Len returns the transform size.
func (f *FFT) Len() int { return f.n }

// Transform computes the forward DFT of x in place: X[k] = sum x[n] e^{-j2πkn/N}.
func (f *FFT) Transform(x []complex128) {
	n := f.n
	for i, j := range f.rev {
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half, step := size/2, n/size
		for start := 0; start < n; start += size {
			for k := 0; k < half; k++ {
				t := f.twiddle[k*step] * x[start+k+half]
				x[start+k+half] = x[start+k] - t
				x[start+k] += t
			}
		}
	}
}

// Hamming returns a symmetric Hamming window of odd length n.
func Hamming(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

// LowpassFIR designs a Hamming-windowed sinc lowpass with the given number of
// (odd) taps and cutoff in cycles/sample, normalised to unity DC gain.
func LowpassFIR(taps int, cutoff float64) []float64 {
	h := make([]float64, taps)
	w := Hamming(taps)
	mid := taps / 2
	sum := 0.0
	for i := range h {
		x := float64(i - mid)
		v := 2 * cutoff
		if x != 0 {
			v = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}
		h[i] = v * w[i]
		sum += h[i]
	}
	for i := range h {
		h[i] /= sum
	}
	return h
}
