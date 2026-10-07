# mbevoc — from-scratch Go MBE vocoders for digital voice radio

A pure Go library (no cgo, no emulation) of encoders and decoders for the multi-band excitation (MBE) vocoders of digital voice radio:

| vocoder | used by | package | parameter frame | coded frame |
|---|---|---|---|---|
| AMBE+2 3600x2450 ("half rate") | DMR, NXDN, P25 Phase 2 | `p25half` | 49 bits | 72 bits |
| IMBE 7200x4400 ("full rate") | P25 Phase 1 | `p25full` | 88 bits | 144 bits |
| IMBE 7100x4400 | EDACS ProVoice | `p25full` (`ProVoiceFrame`) | 88 bits | 142 bits |
| AMBE 3600x2400 | D-STAR | `dstar` | 49 bits (48 used) | 72 bits (9-byte voice data) |

For each, the encoder turns 8 kHz 16-bit PCM into frames and the decoder turns frames back into PCM, with error correction (hard- or soft-decision), frame repeats and muting.

The vocoder names say which formats this module is compatible with. AMBE is a registered trademark, and AMBE+2 and IMBE are trademarks, of Digital Voice Systems, Inc. (DVSI). This project is not affiliated with or endorsed by DVSI; see Patents and trademarks.

AMBE+2 and IMBE are written from their published specifications, TIA-102.BABA-1 and TIA-102.BABA, which build on Griffin and Lim's multi-band excitation model. The AMBE+2 codebooks come from mbelib. D-STAR's rate and ProVoice have no public specification. They follow the reverse-engineered descriptions in mbelib and DSD, checked against mbelib-neo and real-world frames.

For AMBE+2, a Tytera MD-380 radio's firmware vocoder served only as an external check, run outside this module and treated as a black box. It was used to score the output and to settle a few behaviours the standards leave open. It also showed that real radios place four bits of the frame differently from the draft standard (see Provenance and Deviations).

```
go get github.com/nnamon/mbevoc                     # library
go install github.com/nnamon/mbevoc/cmd/...@latest  # mbevoc-enc, mbevoc-dec, mbevoc-params, ambe-server

mbevoc-enc speech.wav speech.amb      # AMBE+2: 49-bit frames, DSD .amb container
mbevoc-enc speech.raw speech.ambe72   # AMBE+2: 72-bit DMR frames, 9 bytes each
mbevoc-enc speech.wav speech.imb      # IMBE (P25 Phase 1): 88-bit frames, DSD .imb container
mbevoc-enc speech.wav speech.imbe144  # IMBE: 144-bit P25 frames, 18 bytes each
mbevoc-enc speech.wav speech.pv       # IMBE: 142-bit EDACS ProVoice frames, 18 bytes each
mbevoc-enc speech.wav speech.dmb      # D-STAR: 49-bit frames, dsd-fme .dmb container
mbevoc-enc speech.wav speech.dv       # D-STAR: 72-bit frames as 9-byte voice data
mbevoc-dec speech.imb speech.wav      # decode any of these to PCM / WAV
mbevoc-params speech.amb              # AMBE+2 frames to MBE model parameters
ambe-server -S 2470                   # AMBE+2 UDP vocoder server, drop-in for md380-emu -S (see below)
```

The codec follows the file name; `-codec ambe2|imbe|dstar` overrides it (for `.bits` text files, for example).

```go
import (
	"github.com/nnamon/mbevoc/dstar"
	"github.com/nnamon/mbevoc/fec"
	"github.com/nnamon/mbevoc/p25full"
	"github.com/nnamon/mbevoc/p25half"
)

var pcm [p25half.FrameSamples]int16 // 160 samples = 20 ms, the same for every codec

// AMBE+2 (DMR, NXDN, P25 Phase 2)
enc, dec := p25half.NewEncoder(), p25half.NewDecoder()
bits := enc.Encode(&pcm)          // frame.Bits (49 bits)
air := fec.Encode(&bits)          // fec.Bits72, fec.Pack -> [9]byte
out := dec.Decode(&bits)          // [160]int16 from an error-free 49-bit frame
out, errs := dec.Decode72(&air)   // from an on-air frame, with FEC, repeats and muting
dtmf0 := p25half.ToneFrame(128, 100) // tone frame: DTMF "0", 100/127 amplitude

// IMBE (P25 Phase 1, EDACS ProVoice)
ienc, idec := p25full.NewEncoder(), p25full.NewDecoder()
ib := ienc.Encode(&pcm)           // p25full.Bits (88 bits)
p25 := ib.Encode()                // p25full.Frame (144 bits, Pack -> [18]byte)
out, ierr := idec.DecodeFrame(&p25)
pv := ib.EncodeProVoice()         // p25full.ProVoiceFrame (142 bits)
out, ierr = idec.DecodeProVoice(&pv)

// D-STAR
denc, ddec := dstar.NewEncoder(), dstar.NewDecoder()
db := denc.Encode(&pcm)           // dstar.Bits (49 bits)
df := db.Encode()                 // dstar.Frame (72 bits, Pack -> 9-byte voice data)
out, derr := ddec.DecodeFrame(&df)

// Soft decisions (log-likelihood ratios, positive = 0) for any coded frame:
// fec.DecodeSoft / Decoder.Decode72Soft, p25full.SoftFrame / SoftProVoiceFrame,
// dstar.SoftFrame, and the matching Decoder methods.
```

## Compared with mbelib and mbelib-neo

[mbelib](https://github.com/szechyjs/mbelib) is the decoder behind DSD and most open-source receivers. [mbelib-neo](https://github.com/arancormonk/mbelib-neo) is its maintained, performance-focused fork.

| | this library | mbelib-neo | mbelib |
|---|---|---|---|
| AMBE+2 3600x2450 | encode + decode | decode | decode |
| IMBE 7200x4400 (P25 Phase 1) | encode + decode | decode | decode |
| IMBE 7100x4400 (ProVoice) | encode + decode | decode | decode |
| AMBE 3600x2400 (D-STAR) | encode + decode | encode + decode | decode |
| soft-decision FEC | exact maximum likelihood, all formats | yes | no |
| IMBE adaptive smoothing (TIA-102.BABA ch. 9) | yes | yes | no |
| tone frames | AMBE+2: decode, and build with `ToneFrame`; D-STAR: single tones | decode | detected, played as silence |
| licence | not yet chosen | GPL-2.0-or-later | ISC |

Where this library and the C libraries disagree, each reading was checked against the specifications, OP25 and real frames. The differences matter when decoding real traffic:

* **AMBE+2 b3/b4 bit placement:** mbelib and mbelib-neo follow the draft standard's Table 8. The MD-380's DVSI vocoder, and so presumably every DVSI radio, places these four bits differently (see Deviations). Both libraries therefore misread four bits of every frame from real radios.
* **P25 Hamming correction:** upstream mbelib's `mbe_hamming1511` corrects a single error in bits 14–8 or 2–0 of a [15,11] word, but miscorrects one in bits 7–3, leaving the data wrong. mbelib-neo has fixed this (`research/tools`, checked on all 2,048 words × 15 positions).
* **IMBE Annex F:** both have 0.068 for the G5 step size at L = 23. TIA-102.BABA and OP25 have 0.058, which every other 4-bit G5 entry also has.

Quality on the same held-out speech, PESQ-NB / STOI. The reference decoders are scored from their float output, since their int16 output clips loud passages.

| frames encoded by | decoded by this library | mbelib | mbelib-neo |
|---|---|---|---|
| this library, AMBE+2 | 3.151 / 0.824 | 2.890 / 0.778 | 2.678 / 0.782 |
| this library, IMBE | 3.218 / 0.829 | 3.045 / 0.798 | 2.691 / 0.792 |
| this library, D-STAR | 3.136 / 0.823 | 2.951 / 0.795 | 2.740 / 0.796 |
| mbelib-neo, D-STAR | 2.052 / 0.703 | 2.197 / 0.697 | 2.243 / 0.712 |

mbelib-neo's decoders are much faster, at 7–8 µs a frame against 28–32 µs here (see Streaming and speed).

## Packages

| package | contents |
|---|---|
| `p25half` | AMBE+2 3600x2450 (TIA-102.BABA-1). `Encoder`: speech analysis, voice activity detection, quantization. `Decoder`: enhancement and synthesis, frame repeat and muting, tone frames (`ToneFrame`, `ToneParams`, `IsTone`), `Decode72` / `Decode72Soft` |
| `quant` | half-rate parameter quantizer: `Predictor.Quantize` (encoder search) and `Predictor.Dequantize` (decoder-side reconstruction, shared so the encoder tracks the decoder exactly), for a `Codebook`: `AMBE2` or `DStar` |
| `fec` | AMBE+2 49 ↔ 72-bit channel coding: [24,12]/[23,12] Golay, PN modulation, Annex H / DMR interleave, with hard and soft error-correcting decode |
| `frame` | the AMBE+2 49-bit frame: b0..b8 bit layout (with conversions to and from the draft standard's, `FromDraftLayout` / `ToDraftLayout`), DSD `.amb`, text and 7-byte wire (`Pack7`) forms |
| `p25full` | IMBE 7200x4400 (TIA-102.BABA): `Encoder`, `Decoder` (with error estimation, repeats, muting and adaptive smoothing), the 88-bit `Bits` and their quantizer values, the 144-bit P25 `Frame`, the 142-bit EDACS `ProVoiceFrame`, soft-decision frames, DSD `.imb` files |
| `dstar` | D-STAR AMBE 3600x2400: `Encoder`, `Decoder`, the 49-bit `Bits`, the 72-bit `Frame` and 9-byte voice data, soft-decision frames, dsd-fme `.dmb` files |
| `internal/halfrate` | the encoder and decoder engine shared by `p25half` and `dstar` |
| `internal/mbe` | the MBE core shared by all codecs: TIA-102.BABA speech analysis (pitch estimation and tracking, refinement, V/UV, amplitudes), spectral enhancement, speech synthesis, and the windows w_I, w_R, w_S and pitch lowpass (generated from the spec's Annexes B–D and I) |
| `internal/ecc` | Golay [23,12]/[24,12], the P25 [15,11] Hamming code, the PN sequence, and exact maximum-likelihood soft decoders |
| `internal/codebook` | quantizer and frame tables: AMBE+2 and D-STAR codebooks generated from mbelib (ISC licence, see `LICENSE.mbelib`), IMBE tables from TIA-102.BABA's annexes, D-STAR and ProVoice frame orders from DSD (ISC licence, see `LICENSE.dsd`) |
| `internal/dsp` | FFT and small helpers |
| `cmd/mbevoc-enc`, `cmd/mbevoc-dec`, `cmd/mbevoc-params` | file encoder and decoder for every codec, AMBE+2 parameter dump |
| `cmd/ambe-server` | UDP vocoder server, protocol-compatible with `md380-emu -S` |

## Provenance

The implementation was written from the published literature:

* **TIA-102.BABA** (IMBE vocoder description), chapter 5, for the speech analysis: high-pass filter, the E(P) pitch criterion, look-back/look-ahead tracking, quarter-sample refinement, V/UV thresholds, and the amplitude estimators. Chapter 8 for spectral amplitude enhancement, and chapter 11 for speech synthesis: windowed-noise unvoiced synthesis with weighted overlap-add, and voiced synthesis with phase tracking. Windows and filter taps come from its Annexes B–D and I (`research/tools/gen_imbe_windows.py`).
* **TIA-102.BABA-1** (half-rate vocoder addendum) for:
  * clauses 4–5: the quantizer equations, frame types, bit prioritisation (except the placement of four bits, see Deviations), Golay coding, PN modulation and interleave (Annex H);
  * clauses 5.5–5.7: error estimation, frame repeats and muting;
  * clause 7 and Annex J: tone frames.
* **Codebooks** (Annexes A–G) come from mbelib's `ambe3600x2450_const.h`, which is ISC-licensed (`research/tools/gen_codebook.py`). Every entry matches the draft's annexes (`research/tools/spec_tables.py`).
* **IMBE 7200x4400** follows TIA-102.BABA throughout: chapter 6 for quantization, chapter 7 for bit prioritisation, Golay and Hamming coding, bit modulation, interleaving (Annex H), error estimation, repeats and muting, chapter 8 for enhancement and chapter 9 for adaptive smoothing. Its tables are transcribed from Annexes E, F, G, H and J and Tables 3–4 (`research/tools/gen_imbe_tables.py`). They are checked entry by entry against mbelib, and Annex F also against OP25.
* **D-STAR AMBE 3600x2400** has no public specification. The codebooks and field layout come from mbelib (ISC), and the frame interleave from DSD (ISC; identical in mbelib-neo). The vocoder model, quantizer search and synthesis are the half-rate ones.
* **EDACS ProVoice** has no public specification either. The parameter order and frame coding follow mbelib, and the frame bit order follows DSD (both ISC).
* **The multi-band excitation model** behind both specifications is D. W. Griffin and J. S. Lim's ("Multiband Excitation Vocoder", IEEE Trans. ASSP 36(8), 1988).
* **Test vectors and cross-checks:**
  * the silence and tone frames published in NXDN TS 1-A §7.2.1 (the silence frame also appears in MMDVMHost);
  * the bit layouts in mbelib and OP25, which follow the draft standard's;
  * DSD's DMR deinterleave tables;
  * IMBE: the encoding example of TIA-102.BABA chapter 10, mbelib's bit-prioritisation table, DSD's P25 interleave, OP25's P25 frame coder, and mbelib's and mbelib-neo's decoders;
  * D-STAR: the null AMBE frame D-STAR gateways send (MMDVMHost), mbelib's decoder, and mbelib-neo's frame coder;
  * ProVoice: mbelib-neo's decoder.

This module contains no firmware, firmware-derived code, or firmware-extracted data.

A Tytera MD-380 radio's firmware vocoder was used only as a **black-box check**, run outside this module (Docker/qemu or Unicorn under `research/oracle`). The harnesses write a frame or 20 ms of PCM to the vocoder's input buffer, call its encode or decode entry point, and read its output buffer. It was used to:

* score quality in both directions: Go-encoded frames through the MD-380 decoder, and MD-380-encoded frames through the Go decoder;
* confirm behaviour where the published sources disagree (for example, the specification and mbelib on silence frames), or where the sources and real radios disagree (the bit placement below);
* choose settings the standards leave open. Each is marked **[MD-380]** below.

One research script, `research/tools/fw_callgraph.py`, disassembles the firmware to measure how much code its vocoder is (169 functions, about 50 KB). Nothing from that scan was used in this module. The firmware is not in this repository; `research/setup.sh` downloads it through md380tools for local use.

## Deviations from / additions to the standards

Frame format:

* **b3/b4 bit placement [MD-380].** The draft TIA-102.BABA-1 (v1.0.5, Table 8) puts the LSB of b3 at frame bit 40 (û3 bit 8) and the three LSBs of b4 at bits 41–43. The MD-380's DVSI vocoder puts b4's three LSBs at bits 40–42 and b3's LSB at bit 43, when encoding and when decoding. This library follows the MD-380, since radios use DVSI's vocoder. mbelib, DSD and OP25 follow Table 8. They therefore misread these four bits in frames from DVSI vocoders, and DVSI decoders misread OP25's. `frame.FromDraftLayout` and `Bits.ToDraftLayout` convert between the two. Evidence, all black-box (`research/tools/probe_bit_map.py`, `probe_field_sweep.py`, `probe_tone_layout.py`):
  * with this placement, flipping a bit of a frame changes the MD-380 decoder's output the way flipping the same bit changes this decoder's, for 48 of the 49 bits. The 49th is a gain bit whose flip only changes the level, which this test cannot place. With Table 8's placement, bits 40–43 match bits 41, 42, 43 and 40 instead;
  * the MD-380 encoder writes tone frames exactly as Table 10 lays them out, in the same 49-bit buffer. That buffer is therefore in the standard's û0..û3 order, so the difference is in how DVSI packs voice frames, not in how the firmware stores them;
  * on the held-out set, reading the bits this way raises the Go decoder on MD-380 frames by 0.087 PESQ, the MD-380 decoder on Go frames by 0.108, and the MD-380 decoder on OP25's frames by 0.092.
  * The radio's own FEC and air interface were not observed. The conclusion rests on the radio applying the same channel coding to voice and tone frames.

AMBE+2 encoder and quantizer:

* **Silence frames do not update the predictors.** This follows BABA-1 §4.3/4.4: gain and spectral prediction come from the last voice frame. mbelib instead updates on silence and resets on erasure or tone frames. A black-box check confirmed that the MD-380 decoder behaves as the standard says. `Predictor.MbelibCompat` reproduces mbelib's behaviour, for cross-checking only.
* **Pitch tracking applies the sub-multiple test (BABA eq. 18–20) to the look-back estimate as well**, and floors the ratio's denominator at 0.05. Without these, strongly periodic input locks onto a multiple of the true period (in the unit tests, 2×, 3× or 5× the period).
* **V/UV and amplitudes are analysed at the refined fundamental for the decoder's L.** BABA-1 requires the encoder to use the decoder's L (the L of the chosen b0).
* **Amplitude estimator follows the final, quantized voicing of each harmonic** (`MatchedAmplitudes`), not the analysis decision.
* **Weighted spectral quantization.** The PRBA, HOC and gain search minimises Σ w_l (Λ̃_l − Λ_l)² with w_l = (M_l / max M)^0.5 (`WeightPower`). Candidates are the 32 best pairs from an exact closed-form unweighted search over all 512×128 (b3, b4) pairs.
* **b1 candidates [MD-380].** b1 is chosen from the 17 codewords BABA-1 allows, minus the table's exact duplicates (1, 3, 13, 15). The MD-380 decoder renders those duplicates as partially voiced rather than as tabulated. It also gives codewords 17–31 meanings beyond the table, which is why only the standard-consistent entries are used.
* **Silence frames carry b1 = 16 [MD-380]** (all unvoiced), as the MD-380 encoder does. BABA-1 says 0, and decoders ignore b1 in silence frames.
* **Voice activity detection [MD-380]** is not specified by the standards. It is an energy detector: noise-floor tracking, 15 dB margin, 4-frame hangover, 1-frame look-ahead. Its parameters were fitted to the MD-380 encoder's silence decisions, and agree with them on 87% of held-out frames. Set `Silence: false` to always send voice frames.
* **Tones.** The encoder does no tone detection, but `ToneFrame` builds tone frames for applications that send DTMF and similar tones. The encoder never emits erasure frames.

AMBE+2 decoder:

* **Tone frame detection:** tone frames are recognised by the first six bits of u0 being all ones (BABA-1 §7), not by b0 = 126/127 alone. A tone frame's b0 can be anywhere in 120–127.
* **Tone synthesis [MD-380]:** tones are synthesized from their BABA-1 Annex J MBE representation. The MD-380 does the same: its DTMF tones come out at Annex J's quantized frequencies, for example 942/1334.6 Hz for "0". The standard leaves the rest open. Following the MD-380, the decoder applies no enhancement to tones, uses continuous phase, and plays them at 1.056× the eq. 68 amplitude. Measured tone frequencies and amplitudes match the MD-380 within about 1%.
* **Silence frames [MD-380]:** these are played at `SilenceGain` = 0.228 (−12.8 dB) of the standard synthesis. The MD-380 does this consistently for every gain value tested. `DecoderConfig{SilenceGain: 1}` restores the standard behaviour.
* **Voiced phases [MD-380]:** by default each harmonic gets a fixed pseudo-random phase offset, and all harmonics (not only those below the 8th) are synthesized with continuous amplitude and phase interpolation. Phase-aligned harmonics have a high crest factor, and the MD-380's steady-state harmonic phases are fixed but not aligned. This scores slightly higher, and its output is closer to the MD-380's. `DecoderConfig{StandardSynthesis: true}` gives the TIA-102.BABA phase model exactly.
* **Repeats and muting:** a frame is repeated when it is an erasure, an invalid tone, or has more than three errors in c0 (detected by parity), or when ε0 ≥ 2 and ε_T ≥ 6. The fourth consecutive repeat, or ε_R > 0.096, mutes to ±5 comfort noise.
* **Not implemented:** the IMBE adaptive smoothing of chapter 9, which BABA-1 does not adopt for the half-rate vocoder.

IMBE (P25 Phase 1):

* **Annex F at L = 23:** the G5 step size is the specification's 0.058, as in OP25. mbelib and mbelib-neo have 0.068.
* **The encoding example of chapter 10 has three misprints.** Its stated fundamental, 2π/35.125, gives L = 15 by eq. 31, not the example's 16. In Table 10, û7 bit 6 must be bit 0 of b16 (not bit 1), and û7 bit 0 must be bit 0 of b18 (not "bit 0" in 1-based numbering), as §7.1's scanning procedure and every other entry imply. The tests use the corrected example.
* **Initial state:** ξ_max starts at Annex A's 100000. The AMBE+2 encoder keeps eq. 41's floor of 20000.
* **Voiced phases:** by default dispersed, with all harmonics interpolated, as in the AMBE+2 decoder. `DecoderConfig{StandardSynthesis: true}` gives chapter 11 exactly. Through this decoder the default scores 3.218 against 3.205 on Go-encoded frames.
* **Adaptive smoothing** follows eq. 112–116 as printed. That includes the amplitude threshold τ_M, which changes by 6000 − 300·ε_T per frame whenever ε_R > 0.005 or ε_T > 6, so it grows unless ε_T exceeds 20. On clean frames smoothing has no effect.
* **Reserved b0 (208–255)** repeats the previous frame (§7.7). mbelib calls 216–219 silence, which TIA-102.BABA does not define.
* **Frame repeats and muting** follow §7.7–7.8 exactly. Unlike the half-rate rules, repeats alone never mute; only ε_R > 0.0875 does.

D-STAR:

* **Frame coding:** the 48 parameter bits are two extended [24,12] Golay codewords plus 24 unprotected bits, c1 modulated by 24 PN bits. That structure is what D-STAR's 2.4 kbps of voice in 3.6 kbps with FEC implies (JARL's system description gives only the rates). In mbelib's 49-bit description, c1's parity bit sits in place of the unused information bit 24, which mbelib ignores. This library writes and checks it, as mbelib-neo writes it. The null AMBE frame decodes with both parity bits consistent.
* **Frame codes:** b0 is classified as in AMBE+2, with 124–125 for silence, 120–123 for erasure and 126–127 for tones. The null AMBE frame has b0 = 124. mbelib decodes every non-tone b0 as voice.
* **The encoder sends only voice frames.** How D-STAR radios encode silence is not known, and mbelib-neo's encoder uses a tone frame for it.
* **Tone frames:** single tones (index 5–122 at 31.25·index Hz) are synthesized as mbelib-neo does. Other indices, including mbelib-neo's silence tone 128, play as silence. The tone-frame bit layout is mbelib's, which it marks as partly inferred.
* **Unverified:** without a DVSI implementation of this rate (an AMBE-3000 chip runs it), the field layout and the c1 parity bit rest on mbelib, mbelib-neo and the null frame.

ProVoice:

* **Hamming code:** syndromes are derived from mbelib's parity-check masks. mbelib's correction table assumes otherwise and miscorrects some single errors; mbelib-neo uses a corrected table.
* **Unverified:** the parity bits that complete c0 and c1 (no decoder reads them) are written as even parity, and c1's is modulated like the rest of c1. Separating the two frames that a ProVoice voice burst interleaves is left to the receiver.

## Measured quality

The corpus is Open Speech Repository Harvard sentences at 8 kHz:
* dev set: 12 files, about 7 minutes;
* held-out set: 12 files, about 7.6 minutes, never used for tuning.

Every combination of encoder (rows) and decoder (columns) was scored against the input as PESQ-NB / STOI. Scripts: `research/tools/eval_go.sh`, `research/tools/eval_dec.sh` and `research/tools/score_dec.py`; see `research/README.md` to reproduce.

| held-out set | MD-380 decoder | **this decoder** | mbelib decoder |
|---|---|---|---|
| MD-380 encoder (DVSI AMBE+2) | 3.114 / 0.804 | 2.950 / 0.802 | 2.616 / 0.712 |
| **this encoder** | **3.204 / 0.825** | **3.151 / 0.824** | 2.832 / 0.776 |
| OP25 encoder, as sent | 2.817 / 0.773 | 2.840 / 0.773 | 1.959 / 0.706 |
| OP25 encoder, through `FromDraftLayout` | 2.909 / 0.788 | 2.938 / 0.789 | 1.939 / 0.696 |

mbelib and OP25 use the draft bit placement (see Deviations), so the mbelib column misreads four bits of every frame except OP25's as sent. The MD-380 and this decoder misread the same bits in OP25's frames as sent.

* **Encoder:** through the MD-380 decoder, Go-encoded frames score 3.204 / 0.825, above the MD-380's own encoder at 3.114 / 0.804.
* **Decoder:** it beats mbelib on every bitstream, and the MD-380 decoder on OP25's. The MD-380 decoder is ahead by 0.053 PESQ on Go-encoded frames and by 0.164 on DVSI-encoded frames, with STOI within 0.002 on both.
* **Where the decoder gap is:** scored on one frame class at a time (`research/tools/masked_pesq.py`), this decoder is within 0.011 of the MD-380's, or ahead of it, on all-voiced and silence frames. It trails on frames with unvoiced bands: by 0.31 on unvoiced and 0.13 on mixed-voicing frames of DVSI streams, and by 0.20 and 0.04 on Go streams.
  * The spectral envelope is not the problem. On DVSI streams this decoder's envelope error against the input is 0.66 dB lower than the MD-380's on unvoiced frames, and 0.14 dB higher on mixed ones. Its voiced and unvoiced components are timed like the MD-380's.
  * None of these closed it: the shape of the noise crossfade, ±2 dB of unvoiced gain, no enhancement of unvoiced bands, high- or low-band gain, or a smoothed noise spectrum. None raised the score by more than 0.025 on either kind of stream. The cause is not known.
  * With `StandardSynthesis` and `SilenceGain: 1`, DVSI-encoded frames score 2.893 / 0.766.

* Output level through the MD-380 decoder is 0.98× the input.
* On strongly voiced speech, the chosen pitch is within 3.5% of the firmware encoder's in 93% of frames.

The mbelib column here (and below) scores mbelib's float output at unity gain. Its int16 output applies a gain of 7 and clips loud passages, which costs it up to 0.85 PESQ on OP25's loud frames.

IMBE 7200x4400 (P25 Phase 1), held-out set (`research/tools/eval_imbe.sh`):

| encoder | **this decoder** | OP25 decoder | mbelib decoder | mbelib-neo decoder |
|---|---|---|---|---|
| **this encoder** | **3.218 / 0.829** | 3.076 / 0.811 | 3.045 / 0.798 | 2.691 / 0.792 |
| OP25 encoder (imbe_vocoder) | 3.007 / 0.792 | 3.080 / 0.798 | 2.786 / 0.757 | 2.341 / 0.748 |

* **Encoder:** through OP25's decoder, Go-encoded frames match OP25's own encoder on PESQ, 3.076 against 3.080, and beat it on STOI, 0.811 against 0.798.
* **Decoder:** on OP25's frames, OP25's decoder is 0.07 PESQ ahead of this one. No setting tried closed that gap: standard synthesis, no enhancement, or no smoothing (which has no effect on clean frames).
* **ProVoice** frames carry the same 88 bits. Their encode and decode is bit-exact with the P25 path on clean frames, so they score the same.

D-STAR AMBE 3600x2400, held-out set (`research/tools/eval_dstar.sh`):

| encoder | **this decoder** | mbelib decoder | mbelib-neo decoder |
|---|---|---|---|
| **this encoder** | **3.136 / 0.823** | 2.951 / 0.795 | 2.740 / 0.796 |
| mbelib-neo encoder | 2.052 / 0.703 | 2.197 / 0.697 | 2.243 / 0.712 |

* **Encoder:** through mbelib-neo's decoder, Go-encoded frames score 0.50 PESQ above mbelib-neo's own encoder.
* **No DVSI reference:** there is none for this rate here, so these scores say nothing about agreement with real D-STAR radios.

Soft-decision decoding was tested in simulation: random frames sent as BPSK through Gaussian noise. At σ = 0.6, frames with errors in their protected bits drop:

* AMBE+2: from 59 to 3 of 1,500;
* D-STAR: from 64 to 1 of 1,500;
* P25 (σ = 0.55): from 114 to 39 of 400.

## Streaming and speed

The API is frame-at-a-time. `Encoder.Encode` takes 160 samples (20 ms) and `Decoder.Decode` / `Decode72` take one frame. Each keeps the inter-frame state of one stream, so a bridge creates one encoder and one decoder per stream (or per timeslot). Instances are not safe for concurrent use, but separate instances run in parallel.

Speed per 20 ms frame, measured on one core of an Apple M4:

| | encode | decode |
|---|---|---|
| **this library, AMBE+2** | **214 µs (93× real time)** | **28 µs (710× real time)** |
| **this library, IMBE** | **50 µs** | **28 µs** |
| **this library, D-STAR** | **240 µs** | **32 µs** |
| OP25 (C++), AMBE+2 / IMBE | 309 / 321 µs | – / 73 µs |
| mbelib (C), AMBE+2 / IMBE / D-STAR | – | 111 / 137 / 130 µs |
| mbelib-neo (C), AMBE+2 / IMBE / D-STAR | – / – / 7 µs | 7 / 8 / 8 µs |
| MD-380 firmware under qemu-user (md380-emu), AMBE+2 | 233 µs | 60 µs |
| MD-380 firmware under Unicorn (Python harness), AMBE+2 | 1,364 µs | 493 µs |

* **IMBE encodes faster** because its quantizers are scalar; AMBE+2 and D-STAR search 65,536 PRBA codebook pairs a frame.
* **Soft-decision decoding** adds about 5 µs per Golay word.

* **How measured:**
  * the same 47-second recording (2,346 frames), best of 7 runs of each command-line tool, CPU time including file I/O;
  * the qemu figures were timed inside a Docker container (linux/arm64), excluding container start-up;
  * Go 1.24.3;
  * the C references built with `-O2`, mbelib-neo without its optional SIMD and fast-math flags.
* **Worst case:** the slowest single frame over 500-frame runs of adversarial input (full-scale noise, square waves, a Nyquist-rate tone, DC, clicks, sweeps) was 0.52 ms to encode and 0.20 ms to decode, so no input comes close to the 20 ms frame budget.
* **Memory:** about 54 KiB per encoder and 14 KiB per decoder, plus 64 KiB of analysis tables shared by all encoders. Creating an encoder takes about 5 µs, and a decoder about 10 µs.
* **History:** the AMBE+2 encoder started at 603 µs per frame. Table-driven DCT cosines, sparse-table window minima in the pitch tracker and an expanded PRBA search criterion brought it to about 215 µs, with output byte-identical across the evaluation corpus at each step.

End-to-end latency (encoder plus decoder, measured):

| | delay | held-out PESQ-NB / STOI (MD-380 decoder) |
|---|---|---|
| this library, `Lookahead: 2` (default) | 60 ms | 3.204 / 0.825 |
| this library, `Lookahead: 1` | 39 ms | 3.179 / 0.823 |
| MD-380 encoder and decoder | 45 ms | 3.114 / 0.804 |

The encoder's own algorithmic delay is 160 + 160·`Lookahead` samples: the pitch window plus the look-ahead frames. The decoder adds one frame. `Lookahead: 0` (20 ms of encoder delay) is available but tracks pitch noticeably worse.

## ambe-server: a drop-in replacement for md380-emu

DMR bridges that need software AMBE+2 commonly run `md380-emu -S 2470`, the MD-380 firmware vocoder under qemu-user with a small UDP server. DVSwitch's Analog_Bridge is one example. `ambe-server` speaks the same protocol, backed by this library: no emulator, no firmware and no qemu. It is a static binary for Linux (x86-64, arm64, ARMv6/v7), macOS and Windows.

```
go install github.com/nnamon/mbevoc/cmd/ambe-server@latest
ambe-server -S 2470                        # listens on 127.0.0.1:2470
```

### Protocol

One UDP datagram carries one 20 ms frame, and requests are dispatched on datagram length alone, as in md380-emu:

| request | reply |
|---|---|
| 320 bytes: 160 little-endian int16 samples, 8 kHz | 7 bytes: the encoded 49-bit frame |
| 7 bytes: a 49-bit frame | 320 bytes: the decoded samples |
| 9 bytes: a 72-bit on-air frame (extension) | 320 bytes: the decoded samples, after FEC |

* **Ignored datagrams:** any other length gets no reply.
* **7-byte frame layout:** bits 0–47 MSB-first in bytes 0–5, and bit 48 as `0x80` in byte 6 (`frame.Bits.Pack7`). Any non-zero byte 6 reads as 1, as in md380-emu. Fields are placed as the MD-380 places them (see Deviations), so md380-emu and `ambe-server` read each other's frames identically.
* **Reply order:** each reply goes to the requesting address. Requests are handled in arrival order, so a client's replies come back in its request order.
* **9-byte extension:** md380-emu ignores 9-byte datagrams, so existing clients are unaffected by it.

### Options

| flag | default | meaning |
|---|---|---|
| `-S` | `2470` | UDP port (same flag as md380-emu) |
| `-host` | `127.0.0.1` | listen address. md380-emu listens on all interfaces; use `-host 0.0.0.0` to do the same, for example inside a container. |
| `-state` | `shared` | `shared`: one encoder and one decoder for all clients, as md380-emu has. `client`: separate state per client address (IP and port), so several bridges or timeslots can use one server without mixing their audio. |
| `-idle`, `-max-clients` | `30s`, `256` | with `-state client`: drop a client's state after this long unused, and keep at most this many (least recently used goes first) |
| `-lookahead` | `2` | encoder look-ahead: `2` for best quality (60 ms codec delay), `1` for 39 ms (−0.025 PESQ) |
| `-silence` | `true` | send silence frames for non-speech input |
| `-fec` | `false` | reply to PCM with 9-byte FEC-coded 72-bit frames instead of 7-byte frames |
| `-standard`, `-silence-gain` | off, `0.228` | decoder synthesis options (see `DecoderConfig`) |
| `-v` | off | log new clients and per-minute counts |

`-state client` keys state on the client's source port. A client that opens a new socket for every frame would get a fresh encoder each time, so use the default `shared` mode for such clients.

### Deploying

**systemd:** replace the emulator in the existing unit.

```
# before: ExecStart=/opt/md380-emu/qemu-arm-static /opt/md380-emu/md380-emu -S 2470
ExecStart=/usr/local/bin/ambe-server -S 2470
```

**DVSwitch Analog_Bridge:** point it at the server in `Analog_Bridge.ini`. The ini describes this as the emulator "for AMBE72 (DMR/YSFN/NXDN)".

```
useEmulator = true
emulatorAddress = 127.0.0.1:2470
```

**Docker:** it has to listen on all interfaces inside the container.

```dockerfile
FROM golang:1.24 AS build
RUN CGO_ENABLED=0 go install github.com/nnamon/mbevoc/cmd/ambe-server@latest
FROM gcr.io/distroless/static-debian12
COPY --from=build /go/bin/ambe-server /ambe-server
EXPOSE 2470/udp
ENTRYPOINT ["/ambe-server", "-host", "0.0.0.0", "-S", "2470"]
```

Run it with `docker run -p 127.0.0.1:2470:2470/udp …` so the port is only reachable from the host.

### Verified against md380-emu

DVSwitch's `md380-emu -S` was built from DVSwitch/md380tools (`research/oracle/dvswitch-md380-emu`). One protocol client then drove both servers over UDP with all 12 held-out recordings, 23,466 frames (`research/tools/server_compat.py`):

* **Frame identity:** every md380-emu reply equals the offline firmware oracle's frame, and every `ambe-server` reply equals `mbevoc-enc`'s frame, byte for byte.
* **Reply format:** replies always had the documented sizes, and byte 6 was always `0x00` or `0x80`.
* **Cross-decoding:** each server's frames were decoded by the other. The scores are exactly the library's offline figures (PESQ-NB / STOI), confirming the two speak the same wire format:

| encoded by | decoded by md380-emu | decoded by ambe-server |
|---|---|---|
| md380-emu | 3.114 / 0.804 | 2.950 / 0.802 |
| ambe-server | 3.204 / 0.825 | 3.151 / 0.824 |

* **Encode round trip:** `ambe-server` had a median of 0.27 ms (p99 0.55 ms). md380-emu under qemu-user in Docker had 0.40 ms (p99 0.61 ms).

As shipped, DVSwitch's md380-emu crashed with a segmentation fault on its first request in this setup. Its server mode calls into the linked firmware without first making that memory executable, which upstream's file modes do with `mprotect`, so current kernels and qemu refuse to run it. This may be the cause of md380tools issue #925. The test build adds that one `mprotect` call.

### Differences from md380-emu that matter in a bridge

* **Audio from radios to the analog side:** decoding frames from real radios (DVSI's encoder) scores 2.950 / 0.802 here against md380-emu's 3.114 / 0.804. Audio towards radios scores higher than md380-emu's own: 3.204 / 0.825 against 3.114 / 0.804, both through the MD-380 decoder.
* **Tones:** md380-emu's encoder detects steady tones and sends tone frames. `ambe-server` has no tone detection, so DTMF and other tones on the analog side go out as voice frames. It does decode tone frames, and `ToneFrame` builds them for applications that need to send tones.
* **Codec delay:** 60 ms by default against md380-emu's 45 ms, or 39 ms with `-lookahead 1`.
* **Exposure:** it listens on loopback only unless `-host` says otherwise. md380-emu answers anyone on the network who reaches its port.
* **Not yet tested:** a live Analog_Bridge deployment, and ARM boards such as the Raspberry Pi, where speed has not been measured.

## Tests

`go test ./...` is self-contained. It covers:

* codebook layout;
* the quantizer: optimality against random alternatives, and encoder/decoder sync;
* Golay and PN, plus the published silence and tone vectors from NXDN TS 1-A §7.2.1 (the silence vector also matches MMDVMHost's `DMR_SILENCE_DATA`);
* encoder behaviour on synthetic signals: pitch, voicing, noise, silence, level, and determinism;
* the full codec loop: level and pitch through encode and decode;
* `Decode72` against `Decode`, and the repeat-then-mute sequence;
* tone frames: frequencies, levels and ID 255;
* the silence-frame level, and decoder determinism;
* `ambe-server`: the protocol replies, both state modes, idle expiry and eviction, and a real UDP round trip;
* Golay and Hamming codes against the generator matrices printed in TIA-102.BABA, and soft decoding against hard;
* IMBE: bit allocation and prioritisation against the chapter 10 example, frame coding with injected errors, `.imb` files, pitch, level, voicing and sync through the codec, repeats and muting under sustained corruption;
* D-STAR: frame coding with injected errors, the null AMBE frame, `.dmb` files, and the codec loop;
* soft decoding of every format through simulated noise.

Some tests also cross-check against reference data produced outside this module, and skip when it is absent:

* the bit layout: mbelib's and OP25's sources must give the draft layout that `FromDraftLayout` converts from;
* `Dequantize`, against mbelib's reconstruction of 50,552 frames (24 bitstreams);
* `fec.Decode`, against mbelib on 20,000 random 72-bit frames;
* IMBE: the bit prioritisation against mbelib's table for every L, and Annex H against DSD's P25 interleave;
* IMBE: frame coding against OP25's coder on 2,000 random frames;
* IMBE: parameter reconstruction against mbelib on 12,450 frames from this encoder and OP25's;
* IMBE: decoding of 3,000 random P25 frames and 3,000 random ProVoice frames against mbelib-neo, including error correction;
* D-STAR: the field layout against mbelib's decoder source, and 9-byte frames against mbelib-neo's coder on 2,000 random frames;
* D-STAR: parameter reconstruction against mbelib on 6,225 frames.

## Patents and trademarks

This section records what was checked. It is not legal advice: check the position for your jurisdiction, and take advice before distributing or deploying this module commercially.

**Patents.** Digital Voice Systems, Inc. (DVSI) holds patents on its vocoders. These are the ones found in force in October 2026 (Google Patents), with their claims compared against this module:

| patent | claims | in force until | this module |
|---|---|---|---|
| US 8,359,197, "Half-rate vocoder" | encoding (claim 1) and decoding (claim 42) with pitch, voicing and gain bits combined in a first error-protected codeword; dependent claims add AMBE+2's 4+4+4-bit c0, Golay coding, PN scrambling keyed from c0, tone frames and frame repeats | 2028-05-20 in the US. Its European counterpart, EP 1 465 158, expired on 2024-03-26 | **`p25half`, the AMBE+2 modes of the commands, and `ambe-server` fall within claims 1 and 42**, as any AMBE+2 3600x2450 implementation does |
| US 12,462,814, "Bit error correction in digital speech" | soft decoding that tries several candidates for the first codeword and keeps the one with the least total distance across all the frame's codes | 2044-05-07 | outside the published claims: every soft decoder here decodes c0 on its own, then the other codewords |
| US 12,451,151, "Tone frame detector for digital speech" | finding tone frames by their distance to candidate tone frames, against thresholds | 2042-06-14 | outside the published claims: tone frames are recognised by an exact six-bit pattern (BABA-1 §7), and the tone index by a majority vote of its copies |
| US 11,990,144 | non-voice data carried in voice frames | 2041 | not implemented |
| US 11,270,714 | spectral parameters sent for only some subframes, the rest interpolated | 2040-01-08 | not implemented |
| US 12,254,895 | detecting a speaker's face mask | 2042-11-13 | not implemented |
| US 8,036,886 | estimating pulsed excitation | 2029-10-02 | not implemented |

* **D-STAR, IMBE and ProVoice:** D-STAR's first codeword carries no voicing bits, so it does not match US 8,359,197's claims. All three formats predate that patent's 2003 priority date. The DVSI patents found from their era have expired: US 5,226,084, 6,199,037, 6,377,916, 6,912,495, 7,634,399, 7,970,606, 8,315,860 and 8,595,002.
* **Not to be added while the patents are in force:**
  * soft decoding that chooses c0 by how well the other codewords then decode (US 12,462,814);
  * recognising tone frames by their distance to the nearest valid tone frame (US 12,451,151).
* **Limits of this check:**
  * DVSI's full patent list was not enumerated, so it may be incomplete;
  * for US 12,462,814 and US 12,451,151, the claims read were those of the published applications (US 2025/0118309 and US 2023/0326473);
  * outside the US, only US 8,359,197's European counterpart was checked.

**Trademarks.** AMBE is a registered trademark, and AMBE+2 and IMBE are trademarks, of Digital Voice Systems, Inc. They are used here only to name the formats this module is compatible with. This project is not affiliated with or endorsed by DVSI.
