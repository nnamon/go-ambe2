# Research and evaluation tooling

This directory holds the tooling used to build, check and score the Go library. The library itself is written from the published specifications (see the top-level README). The tooling here includes harnesses that run a Tytera MD-380 radio's firmware vocoder as an external black-box check.

Nothing here is needed to build or use the library. The firmware never runs inside the Go module.

None of the following is committed:

* the firmware;
* the speech corpus;
* the TIA specifications;
* the third-party sources.

`setup.sh` fetches all of them, verifies the firmware's SHA-256 (via md380tools), builds the tools and generates the reference data. A full run takes a few minutes.

```
research/setup.sh          # needs git, curl, make, cc/c++, go, python3 (pdftotext optional)
```

## Layout

| path | contents |
|---|---|
| `setup.sh`, `requirements.txt` | fetch, build and prepare everything below the line |
| `corpus/dev.txt`, `corpus/heldout.txt` | the Open Speech Repository files in the dev set (tuning) and the held-out set (never used for tuning) |
| `oracle/unicorn/md380_uc.py` | MD-380 firmware D002.032 vocoder on the Unicorn CPU emulator (Python). It encodes and decodes `.amb`/`.bits`, and is bit-exact with the qemu build on the whole corpus. |
| `oracle/md380-emu-docker/` | md380tools' `md380-emu`, plus a harness (`oracle.c`) that does not drop frames, built for qemu-user in Docker (`ambe-oracle` wrapper) |
| `oracle/dvswitch-md380-emu/` | DVSwitch's `md380-emu -S` UDP server as bridges run it, built in Docker with one `mprotect` fix so it does not crash on current systems |
| `tools/Makefile` | builds into `bin/`: the Go CLIs, `mbelib-dec` (mbelib decoder, with parameter dump), `op25-enc` (OP25's encoder) and `fec-xcheck` (mbelib FEC reference) |
| `tools/mkcorpus.sh`, `eval_go.sh`, `eval_dec.sh`, `score.py`, `score_dec.py` | corpus preparation and PESQ-NB / STOI scoring of every encoder × decoder pairing |
| `tools/param_diff.py`, `lsd.py`, `level_by_voicing.py`, `dec_divergence.py`, `ltas.py` | diagnostics: parameter agreement, spectral distance, level per voicing class, long-term spectra |
| `tools/probe_*.py`, `collect_phase.py`, `fit_*.py`, `shape_regress.py`, `amp_compare.py`, `remap_b1.py` | black-box probes of MD-380 decoder behaviour (see Findings) |
| `tools/server_compat.py` | drives `ambe-server` and DVSwitch's `md380-emu -S` with one UDP client: reply format, identity with the offline tools, cross-decoding scores, round-trip times |
| `tools/vad_fit.py` | fits the voice activity detector to the MD-380 encoder's silence decisions |
| `tools/gen_codebook.py`, `gen_imbe_windows.py`, `mbelib_tables.py` | regenerate `../internal/codebook` (from mbelib, ISC) and `../internal/imbe` (from the TIA-102.BABA annexes) |
| `tools/fw_callgraph.py` | scopes the firmware's vocoder code (169 functions, about 50 KB of Thumb-2) |

Created by `setup.sh` (git-ignored):

| path | contents |
|---|---|
| `refs/` | third-party sources: md380tools (and the firmware), DVSwitch's md380tools fork, mbelib, OP25, dsd-fme, MMDVMHost |
| `papers/` | TIA-102.BABA and the TIA-102.BABA-1 draft |
| `testdata/` | corpus sets with the MD-380 and OP25 baselines, plus cross-check data for the Go tests |
| `bin/`, `.venv/` | builds and the Python environment |

## Reproducing the results

Run from this directory after `setup.sh`:

```
tools/eval_go.sh go                                  # Go encoder, dev set
SET=testdata/heldout tools/eval_go.sh go             # Go encoder, held-out set
SET=testdata/heldout tools/eval_dec.sh godec fw go op25   # Go decoder vs MD-380 decoder and mbelib
SET=testdata/heldout .venv/bin/python tools/score.py fw op25   # MD-380 and OP25 encoder baselines
cd .. && go test ./...                               # includes the cross-checks against research/testdata
```

To check `ambe-server` against md380-emu (needs Docker):

```
docker build -f oracle/dvswitch-md380-emu/Dockerfile -t dvswitch-md380-emu .
.venv/bin/python tools/server_compat.py testdata/heldout
```

`eval_go.sh` passes extra arguments to `ambe-enc`, and `eval_dec.sh` takes decoder flags in `$DECFLAGS`. Results land next to each recording as `<tag>.amb` and `<tag>.<decoder>.raw`.

* **Held-out set:** the scores in the top-level README reproduce exactly from a fresh `setup.sh`.
* **Dev set:** it is 12 files here. During development it also included a macOS text-to-speech sample, so dev-set means differ slightly from the figures quoted during tuning.

## Findings from the MD-380 black box

Each is reproducible with the scripts named.

* **Predictor (`probe_silence.py`):** silence frames do not update the gain/spectrum predictor (TIA-102.BABA-1 behaviour; mbelib differs).
* **Silence frames (`probe_silence.py`):** they decode 12.8 dB below the standard synthesis, for every gain value tested.
* **Tone frames (`probe_tone.py`):**
  * they are synthesized from the BABA-1 Annex J MBE representation (DTMF "0" at 942.0/1334.6 Hz);
  * no enhancement is applied, and each component plays at 1.056× the eq. 68 amplitude.
* **Tone detection (`probe_tone.py`):** a tone frame is detected by the first six bits of u0, even when b0 is 120–123.
* **Voicing codewords (`probe_vuv.py`):**
  * codewords 1, 3, 11, 13, 15 and 17 decode as partially voiced;
  * 18–31 decode as unvoiced with redistributed band energy;
  * the canonical codewords match the published table. Remapping the non-canonical ones costs the MD-380 0.06 PESQ (`remap_b1.py`).
* **Phases (`probe_phase2.py`, `collect_phase.py`, `fit_phase*.py`):**
  * voiced harmonic phases are fixed in steady state but not aligned;
  * no simple envelope-derived phase model explained them.
* **Prediction coefficient (`probe_rho.py`):** spectral prediction converges like the standard's ρ = 0.65.
* **Field interpretation (`probe_fields.py`):** b2 and b5–b8 are interpreted as mbelib does.
* **Silence decisions (`vad_fit.py`):** the MD-380 encoder's silence decisions follow an adaptive energy detector; the fit agrees on 87% of held-out frames.
