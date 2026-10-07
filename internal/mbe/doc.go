// Package mbe is the multi-band excitation (MBE) core shared by the codecs in
// this module: the speech analysis of TIA-102.BABA chapter 5 (pitch
// estimation and tracking, refinement, V/UV determination, spectral
// amplitudes), the spectral amplitude enhancement of chapter 8, adaptive
// smoothing (chapter 9) and the speech synthesis of chapter 11, with their
// windows (Annexes B-D and I).
package mbe

// FrameSamples is the number of 8 kHz samples per 20 ms frame.
const FrameSamples = 160

// MaxL is the largest number of harmonics any of the codecs signals.
const MaxL = 56
