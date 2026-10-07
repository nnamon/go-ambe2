// Package imbe implements the IMBE 7200x4400 full-rate vocoder of P25
// Phase 1 (TIA-102.BABA), written from the specification:
//
//   - Encoder: the speech analysis of chapter 5 (shared with this module's
//     AMBE+2 encoder), the parameter quantization of chapter 6, and the bit
//     prioritization of §7.1, giving an 88-bit parameter frame (Bits).
//   - Frame coding: the [23,12] Golay and [15,11] Hamming coding, bit
//     modulation and interleaving of §7.3-7.5, giving the 144-bit frame
//     carried in P25 Phase 1 voice LDUs (Frame).
//   - Decoder: error estimation, frame repeats and muting (§7.6-7.8),
//     parameter reconstruction (chapter 6), spectral amplitude enhancement
//     (chapter 8), adaptive smoothing (chapter 9) and speech synthesis
//     (chapter 11).
//
// The quantizer tables come from the specification's annexes (see
// internal/codebook/imbe.go).
package imbe

// FrameSamples is the number of 8 kHz samples per 20 ms frame.
const FrameSamples = 160
