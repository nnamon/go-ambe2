package ecc

import "testing"

func BenchmarkDecode23Soft(b *testing.B) {
	llr := make([]float64, 23)
	for i := range llr {
		llr[i] = float64(i%5) - 2.1
	}
	for i := 0; i < b.N; i++ {
		Decode23Soft(llr)
	}
}
