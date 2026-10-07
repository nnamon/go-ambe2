// Package mbevoc is the root of a pure Go implementation of the multi-band
// excitation (MBE) vocoders of digital voice radio.  It holds no code; the
// codecs are in its subpackages:
//
//   - p25half: the half-rate vocoder of TIA-102.BABA-1, compatible with
//     AMBE+2 3600x2450 (DMR, NXDN, P25 Phase 2); its frames and forward error
//     correction are in packages frame and fec
//   - p25full: the full-rate vocoder of TIA-102.BABA, compatible with IMBE
//     7200x4400 (P25 Phase 1), and EDACS ProVoice's 7100x4400 frames
//   - dstar: the D-STAR vocoder, compatible with AMBE 3600x2400
//
// AMBE is a registered trademark, and AMBE+2 and IMBE are trademarks, of
// Digital Voice Systems, Inc.  This module is not affiliated with or endorsed
// by Digital Voice Systems, Inc.
package mbevoc
