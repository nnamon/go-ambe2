// Package dstar implements the AMBE 3600x2400 vocoder of D-STAR: 2400 bps of
// voice parameters (48 bits per 20 ms) in a 72-bit frame with forward error
// correction.
//
// D-STAR's vocoder has no public specification.  This package follows the
// reverse-engineered description in mbelib (tables, field layout, pitch
// formula and frame interleave, cross-checked with DSD and mbelib-neo); the
// model is the same as the half-rate vocoder's (TIA-102.BABA-1), so the
// encoder and decoder share this module's AMBE+2 analysis, quantizer search
// and synthesis, with D-STAR's codebook (quant.DStar).  Without access to a
// DVSI implementation of this rate the bit layout is unverified against
// real radios.
package dstar

// FrameSamples is the number of 8 kHz samples per 20 ms frame.
const FrameSamples = 160
