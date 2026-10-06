# ambe — a from-scratch Go AMBE+2 3600x2450 encoder and decoder

A pure Go library (no cgo, no emulation) for the AMBE+2 "half-rate" vocoder used by DMR, NXDN (EHR) and P25 Phase 2:

* **Encode:** 8 kHz 16-bit PCM to 49-bit voice frames (2450 bps), optionally FEC-coded and interleaved into the 72-bit (3600 bps) frames carried on air.
* **Decode:** either frame form back to PCM. 72-bit frames get error correction, frame repeats, muting, and tone frames (DTMF, KNOX, call progress).

It is written from the published literature: the TIA-102.BABA (IMBE) and TIA-102.BABA-1 (half-rate) specifications, which build on Griffin and Lim's multi-band excitation model, with codebooks taken from mbelib. A Tytera MD-380 radio's firmware vocoder served only as a black-box check, run outside this module: it was used to score the output and to settle a few behaviours the standards leave open (see Provenance).

```
go get github.com/nnamon/go-ambe2                     # library
go install github.com/nnamon/go-ambe2/cmd/...@latest  # ambe-enc, ambe-dec, ambe-params, ambe-server

ambe-enc speech.wav speech.amb        # 49-bit frames, DSD .amb container
ambe-enc speech.raw speech.ambe72     # 72-bit DMR frames, 9 bytes each
ambe-dec speech.amb speech.wav        # decode (.amb, .bits or .ambe72) to PCM / WAV
ambe-params speech.amb                # decode frames to MBE model parameters
ambe-server -S 2470                   # UDP vocoder server, drop-in for md380-emu -S (see below)
```

```go
import (
	ambe "github.com/nnamon/go-ambe2"
	"github.com/nnamon/go-ambe2/fec"
)

enc := ambe.NewEncoder()
var pcm [ambe.FrameSamples]int16 // 160 samples = 20 ms
bits := enc.Encode(&pcm)         // frame.Bits (49 bits)
air := fec.Encode(&bits)         // fec.Bits72, fec.Pack -> [9]byte

dec := ambe.NewDecoder()
out := dec.Decode(&bits)          // [160]int16 from an error-free 49-bit frame
out, errs := dec.Decode72(&air)   // from an on-air frame, with FEC, repeats and muting
dtmf0 := ambe.ToneFrame(128, 100) // tone frame: DTMF "0", 100/127 amplitude
```

## Packages

| package | contents |
|---|---|
| `ambe` | `Encoder`: MBE speech analysis (pitch estimation and tracking, refinement, V/UV, spectral amplitudes), voice activity detection, framing. `Decoder`: spectral enhancement and MBE speech synthesis, frame repeat and muting, tone frames (`ToneFrame`, `ToneParams`, `IsTone`) |
| `quant` | half-rate parameter quantizer: `Predictor.Quantize` (encoder search) and `Predictor.Dequantize` (decoder-side reconstruction, shared so the encoder tracks the decoder exactly) |
| `fec` | 49 ↔ 72-bit channel coding: [24,12]/[23,12] Golay, PN modulation, Annex H / DMR interleave, with error-correcting decode |
| `frame` | the 49-bit frame: b0..b8 bit layout, DSD `.amb`, text and 7-byte wire (`Pack7`) forms |
| `internal/codebook` | quantizer tables (generated from mbelib, ISC licence, see `LICENSE.mbelib`) |
| `internal/imbe` | TIA-102.BABA windows w_I, w_R, w_S and the pitch lowpass (generated from the spec's Annexes B–D and I) |
| `internal/dsp` | FFT and small helpers |
| `cmd/ambe-enc`, `cmd/ambe-dec`, `cmd/ambe-params` | file encoder, decoder and parameter dump |
| `cmd/ambe-server` | UDP vocoder server, protocol-compatible with `md380-emu -S` |

## Provenance

The implementation was written from the published literature:

* **TIA-102.BABA** (IMBE vocoder description), chapter 5, for the speech analysis: high-pass filter, the E(P) pitch criterion, look-back/look-ahead tracking, quarter-sample refinement, V/UV thresholds, and the amplitude estimators. Chapter 8 for spectral amplitude enhancement, and chapter 11 for speech synthesis: windowed-noise unvoiced synthesis with weighted overlap-add, and voiced synthesis with phase tracking. Windows and filter taps come from its Annexes B–D and I (`research/tools/gen_imbe_windows.py`).
* **TIA-102.BABA-1** (half-rate vocoder addendum) for:
  * clauses 4–5: the quantizer equations, frame types, bit prioritisation, Golay coding, PN modulation and interleave (Annex H);
  * clauses 5.5–5.7: error estimation, frame repeats and muting;
  * clause 7 and Annex J: tone frames.
* **Codebooks** (Annexes A–G) come from mbelib's `ambe3600x2450_const.h`, which is ISC-licensed (`research/tools/gen_codebook.py`).
* **The multi-band excitation model** behind both specifications is D. W. Griffin and J. S. Lim's ("Multiband Excitation Vocoder", IEEE Trans. ASSP 36(8), 1988).
* **Test vectors and cross-checks:**
  * the silence and tone frames published in NXDN TS 1-A §7.2.1 (the silence frame also appears in MMDVMHost);
  * the bit layouts in mbelib and OP25;
  * DSD's DMR deinterleave tables.

This module contains no firmware, firmware-derived code, or firmware-extracted data.

A Tytera MD-380 radio's firmware vocoder was used only as a **black-box check**, run outside this module (Docker/qemu or Unicorn under `research/oracle`). It was used to:

* score quality in both directions: Go-encoded frames through the MD-380 decoder, and MD-380-encoded frames through the Go decoder;
* confirm behaviour where the published sources disagree (for example, the specification and mbelib on silence frames);
* choose settings the standards leave open. Each is marked **[MD-380]** below.

## Deviations from / additions to the standards

Encoder and quantizer:

* **Silence frames do not update the predictors.** This follows BABA-1 §4.3/4.4: gain and spectral prediction come from the last voice frame. mbelib instead updates on silence and resets on erasure or tone frames. A black-box check confirmed that the MD-380 decoder behaves as the standard says. `Predictor.MbelibCompat` reproduces mbelib's behaviour, for cross-checking only.
* **Pitch tracking applies the sub-multiple test (BABA eq. 18–20) to the look-back estimate as well**, and floors the ratio's denominator at 0.05. Without these, strongly periodic input locks onto a multiple of the true period (in the unit tests, 2×, 3× or 5× the period).
* **V/UV and amplitudes are analysed at the refined fundamental for the decoder's L.** BABA-1 requires the encoder to use the decoder's L (the L of the chosen b0).
* **Amplitude estimator follows the final, quantized voicing of each harmonic** (`MatchedAmplitudes`), not the analysis decision.
* **Weighted spectral quantization.** The PRBA, HOC and gain search minimises Σ w_l (Λ̃_l − Λ_l)² with w_l = (M_l / max M)^0.5 (`WeightPower`). Candidates are the 32 best pairs from an exact closed-form unweighted search over all 512×128 (b3, b4) pairs.
* **b1 candidates [MD-380].** b1 is chosen from the 17 codewords BABA-1 allows, minus the table's exact duplicates (1, 3, 13, 15). The MD-380 decoder renders those duplicates as partially voiced rather than as tabulated. It also gives codewords 17–31 meanings beyond the table, which is why only the standard-consistent entries are used.
* **Silence frames carry b1 = 16 [MD-380]** (all unvoiced), as the MD-380 encoder does. BABA-1 says 0, and decoders ignore b1 in silence frames.
* **Voice activity detection [MD-380]** is not specified by the standards. It is an energy detector: noise-floor tracking, 15 dB margin, 4-frame hangover, 1-frame look-ahead. Its parameters were fitted to the MD-380 encoder's silence decisions, and agree with them on 87% of held-out frames. Set `Silence: false` to always send voice frames.
* **Tones.** The encoder does no tone detection, but `ToneFrame` builds tone frames for applications that send DTMF and similar tones. The encoder never emits erasure frames.

Decoder:

* **Tone frame detection:** tone frames are recognised by the first six bits of u0 being all ones (BABA-1 §7), not by b0 = 126/127 alone. A tone frame's b0 can be anywhere in 120–127.
* **Tone synthesis [MD-380]:** tones are synthesized from their BABA-1 Annex J MBE representation. The MD-380 does the same: its DTMF tones come out at Annex J's quantized frequencies, for example 942/1334.6 Hz for "0". The standard leaves the rest open. Following the MD-380, the decoder applies no enhancement to tones, uses continuous phase, and plays them at 1.056× the eq. 68 amplitude. Measured tone frequencies and amplitudes match the MD-380 within about 1%.
* **Silence frames [MD-380]:** these are played at `SilenceGain` = 0.228 (−12.8 dB) of the standard synthesis. The MD-380 does this consistently for every gain value tested. `DecoderConfig{SilenceGain: 1}` restores the standard behaviour.
* **Voiced phases [MD-380]:** by default each harmonic gets a fixed pseudo-random phase offset, and all harmonics (not only those below the 8th) are synthesized with continuous amplitude and phase interpolation. Phase-aligned harmonics have a high crest factor, and the MD-380's steady-state harmonic phases are fixed but not aligned. This scores slightly higher, and its output is closer to the MD-380's. `DecoderConfig{StandardSynthesis: true}` gives the TIA-102.BABA phase model exactly.
* **Repeats and muting:** a frame is repeated when it is an erasure, an invalid tone, or has more than three errors in c0 (detected by parity), or when ε0 ≥ 2 and ε_T ≥ 6. The fourth consecutive repeat, or ε_R > 0.096, mutes to ±5 comfort noise.
* **Not implemented:** the IMBE adaptive smoothing of chapter 9, which BABA-1 does not adopt for the half-rate vocoder, and soft-decision FEC decoding.

## Measured quality

The corpus is Open Speech Repository Harvard sentences at 8 kHz:
* dev set: 13 files, about 7 minutes;
* held-out set: 12 files, about 7.6 minutes, never used for tuning.

Every combination of encoder (rows) and decoder (columns) was scored against the input as PESQ-NB / STOI. Scripts: `research/tools/eval_go.sh`, `research/tools/eval_dec.sh` and `research/tools/score_dec.py`; see `research/README.md` to reproduce.

| held-out set | MD-380 decoder | **this decoder** | mbelib decoder |
|---|---|---|---|
| MD-380 encoder (DVSI AMBE+2) | 3.114 / 0.804 | 2.863 / 0.786 | 2.616 / 0.712 |
| **this encoder** | 3.096 / 0.807 | **3.151 / 0.824** | 2.920 / 0.794 |
| OP25 encoder | 2.817 / 0.773 | 2.938 / 0.789 | 1.959 / 0.706 |

* **Encoder:** through the MD-380 decoder, Go-encoded frames score within 0.02 PESQ of the MD-380's own encoder, with higher STOI.
* **Decoder:** it beats mbelib on every bitstream. It also beats the MD-380 decoder on Go and OP25 bitstreams.
* **Decoder vs DVSI streams:** on DVSI-encoded bitstreams the Go decoder is 0.25 PESQ / 0.018 STOI below the MD-380's own decoder.
  * The [MD-380] settings above recover part of that gap: with `StandardSynthesis` and `SilenceGain: 1` the score is 2.802 / 0.750.
  * The rest was not attributable to any single mechanism tested: spectral reconstruction, prediction, phase model, harmonic interpolation, noise synthesis, spectral tilt, and the codewords DVSI's encoder uses beyond the standard table (worth 0.06 to the MD-380 itself).

* Output level through the MD-380 decoder is 0.98× the input.
* On strongly voiced speech, the chosen pitch is within 3.5% of the firmware encoder's in 93% of frames.

## Streaming and speed

The API is frame-at-a-time. `Encoder.Encode` takes 160 samples (20 ms) and `Decoder.Decode` / `Decode72` take one frame. Each keeps the inter-frame state of one stream, so a bridge creates one encoder and one decoder per stream (or per timeslot). Instances are not safe for concurrent use, but separate instances run in parallel.

Speed per 20 ms frame, measured on one core of an Apple M4:

| | encode | decode |
|---|---|---|
| **this library** | **218 µs (92× real time)** | **23 µs (880× real time)** |
| OP25 encoder (C++) / mbelib decoder (C) | 311 µs | 124 µs |
| MD-380 firmware under qemu-user (md380-emu) | 233 µs | 60 µs |
| MD-380 firmware under Unicorn (Python harness) | 1,364 µs | 493 µs |

* **How measured:**
  * the same 47-second recording (2,346 frames), best of 7 runs of each command-line tool, CPU time including file I/O;
  * the qemu figures were timed inside a Docker container (linux/arm64), excluding container start-up;
  * Go 1.24.3.
* **Worst case:** the slowest single frame over 500-frame runs of adversarial input (full-scale noise, square waves, a Nyquist-rate tone, DC, clicks, sweeps) was 0.52 ms to encode and 0.20 ms to decode, so no input comes close to the 20 ms frame budget.
* **Memory:** about 118 KiB per encoder and 14 KiB per decoder.
* **History:** the encoder started at 603 µs per frame. Table-driven DCT cosines, sparse-table window minima in the pitch tracker and an expanded PRBA search criterion brought it to 218 µs, with output byte-identical across the evaluation corpus at each step.

End-to-end latency (encoder plus decoder, measured):

| | delay | held-out PESQ-NB / STOI (MD-380 decoder) |
|---|---|---|
| this library, `Lookahead: 2` (default) | 60 ms | 3.096 / 0.807 |
| this library, `Lookahead: 1` | 39 ms | 3.084 / 0.806 |
| MD-380 encoder and decoder | 45 ms | 3.114 / 0.804 |

The encoder's own algorithmic delay is 160 + 160·`Lookahead` samples: the pitch window plus the look-ahead frames. The decoder adds one frame. `Lookahead: 0` (20 ms of encoder delay) is available but tracks pitch noticeably worse.

## ambe-server: a drop-in replacement for md380-emu

DMR bridges that need software AMBE+2 commonly run `md380-emu -S 2470`, the MD-380 firmware vocoder under qemu-user with a small UDP server. DVSwitch's Analog_Bridge is one example. `ambe-server` speaks the same protocol, backed by this library: no emulator, no firmware and no qemu. It is a static binary for Linux (x86-64, arm64, ARMv6/v7), macOS and Windows.

```
go install github.com/nnamon/go-ambe2/cmd/ambe-server@latest
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
* **7-byte frame layout:** bits 0–47 MSB-first in bytes 0–5, and bit 48 as `0x80` in byte 6 (`frame.Bits.Pack7`). Any non-zero byte 6 reads as 1, as in md380-emu.
* **Reply order:** each reply goes to the requesting address. Requests are handled in arrival order, so a client's replies come back in its request order.
* **9-byte extension:** md380-emu ignores 9-byte datagrams, so existing clients are unaffected by it.

### Options

| flag | default | meaning |
|---|---|---|
| `-S` | `2470` | UDP port (same flag as md380-emu) |
| `-host` | `127.0.0.1` | listen address. md380-emu listens on all interfaces; use `-host 0.0.0.0` to do the same, for example inside a container. |
| `-state` | `shared` | `shared`: one encoder and one decoder for all clients, as md380-emu has. `client`: separate state per client address (IP and port), so several bridges or timeslots can use one server without mixing their audio. |
| `-idle`, `-max-clients` | `30s`, `256` | with `-state client`: drop a client's state after this long unused, and keep at most this many (least recently used goes first) |
| `-lookahead` | `2` | encoder look-ahead: `2` for best quality (60 ms codec delay), `1` for 39 ms (−0.012 PESQ) |
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
RUN CGO_ENABLED=0 go install github.com/nnamon/go-ambe2/cmd/ambe-server@latest
FROM gcr.io/distroless/static-debian12
COPY --from=build /go/bin/ambe-server /ambe-server
EXPOSE 2470/udp
ENTRYPOINT ["/ambe-server", "-host", "0.0.0.0", "-S", "2470"]
```

Run it with `docker run -p 127.0.0.1:2470:2470/udp …` so the port is only reachable from the host.

### Verified against md380-emu

DVSwitch's `md380-emu -S` was built from DVSwitch/md380tools (`research/oracle/dvswitch-md380-emu`). One protocol client then drove both servers over UDP with all 12 held-out recordings, 23,466 frames (`research/tools/server_compat.py`):

* **Frame identity:** every md380-emu reply equals the offline firmware oracle's frame, and every `ambe-server` reply equals `ambe-enc`'s frame, byte for byte.
* **Reply format:** replies always had the documented sizes, and byte 6 was always `0x00` or `0x80`.
* **Cross-decoding:** each server's frames were decoded by the other. The scores are exactly the library's offline figures (PESQ-NB / STOI), confirming the two speak the same wire format:

| encoded by | decoded by md380-emu | decoded by ambe-server |
|---|---|---|
| md380-emu | 3.114 / 0.804 | 2.863 / 0.786 |
| ambe-server | 3.096 / 0.807 | 3.151 / 0.824 |

* **Encode round trip:** `ambe-server` had a median of 0.26 ms (p99 0.36 ms). md380-emu under qemu-user in Docker had 0.36 ms (p99 0.54 ms).

As shipped, DVSwitch's md380-emu crashed with a segmentation fault on its first request in this setup. Its server mode calls into the linked firmware without first making that memory executable, which upstream's file modes do with `mprotect`, so current kernels and qemu refuse to run it. This may be the cause of md380tools issue #925. The test build adds that one `mprotect` call.

### Differences from md380-emu that matter in a bridge

* **Audio from radios to the analog side:** decoding frames from real radios (DVSI's encoder) scores 2.863 / 0.786 here against md380-emu's 3.114 / 0.804. Audio towards radios is on par.
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
* `ambe-server`: the protocol replies, both state modes, idle expiry and eviction, and a real UDP round trip.

Some tests also cross-check against reference data produced outside this module, and skip when it is absent:

* the bit layout, against the mbelib and OP25 sources;
* `Dequantize`, against mbelib's reconstruction of 25,510 frames;
* `fec.Decode`, against mbelib on 20,000 random 72-bit frames.

## Patent note

DVSI holds patents on AMBE+2. Google Patents lists US 8,359,197 ("Half-rate vocoder", which claims this frame format) as active until 2028-05-20. Check the patent position for your jurisdiction before distributing or deploying. This is not legal advice.
