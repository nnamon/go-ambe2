#!/usr/bin/env python3
"""Evaluation helpers for AMBE+2 3600x2450 work.

  ambe_eval.py fields  A.amb|A.bits B.amb|B.bits      per-field agreement of two bitstreams
  ambe_eval.py dump    A.amb|A.bits                   print b0..b8 per frame
  ambe_eval.py quality ref.raw deg.raw [deg2.raw ...]  PESQ-NB / STOI / delay vs reference

PCM files are 8 kHz mono s16le.  Fields:
  b0 pitch 7b, b1 V/UV 5b, b2 gain 5b, b3 PRBA24 9b, b4 PRBA58 7b,
  b5 HOC1 5b, b6 HOC2 4b, b7 HOC3 4b, b8 HOC4 3b
LAYOUT is the MD-380's (DVSI's) placement, as in the Go frame package: b4 bits
2..0 at positions 40..42 and b3 bit 0 at 43 (tools/probe_bit_map.py).  mbelib
and OP25 use the TIA-102.BABA-1 draft's Table 8 placement, DRAFT_LAYOUT,
which has b3 bit 0 at 40 and b4 bits 2..0 at 41..43.
"""
import sys

import numpy as np

WIDTHS = [7, 5, 5, 9, 7, 5, 4, 4, 3]
NAMES = ["b0 pitch", "b1 vuv", "b2 gain", "b3 prba24", "b4 prba58",
         "b5 hoc1", "b6 hoc2", "b7 hoc3", "b8 hoc4"]
# (field, bit-significance) for each of the 49 positions.
def _layout(b3_lsb, b4_lsbs):
    out = {}
    for f, w, msb_pos, lsb_pos in [
        (0, 7, [0, 1, 2, 3], [37, 38, 39]),
        (1, 5, [4, 5, 6, 7], [35]),
        (2, 5, [8, 9, 10, 11], [36]),
        (3, 9, [12, 13, 14, 15, 16, 17, 18, 19], b3_lsb),
        (4, 7, [20, 21, 22, 23], b4_lsbs),
        (5, 5, [24, 25, 26, 27], [44]),
        (6, 4, [28, 29, 30], [45]),
        (7, 4, [31, 32, 33], [46]),
        (8, 3, [34], [47, 48]),
    ]:
        for i, pos in enumerate(msb_pos + lsb_pos):
            out[pos] = (f, w - 1 - i)
    assert sorted(out) == list(range(49))
    return out


LAYOUT = _layout([43], [40, 41, 42])
DRAFT_LAYOUT = _layout([40], [41, 42, 43])


def read_frames(path):
    """Returns a list of 49-element bit lists."""
    if path.endswith(".bits"):
        return [[int(c) for c in line.strip()] for line in open(path) if len(line.strip()) >= 49]
    data = open(path, "rb").read()
    if data[:4] != b".amb":
        raise SystemExit(f"{path}: bad .amb magic")
    out = []
    for k in range(4, len(data) - 7, 8):
        p = data[k:k + 8]
        out.append([(p[1 + i // 8] >> (7 - i % 8)) & 1 for i in range(48)] + [p[7] & 1])
    return out


def fields_to_bits(f, layout=None):
    """b0..b8 -> 49 bits (LAYOUT unless another layout is given)."""
    b = [0] * 49
    for pos, (fi, sig) in (layout or LAYOUT).items():
        b[pos] = (f[fi] >> sig) & 1
    return b


def go_decode_runs(runs, dec="bin/mbevoc-dec"):
    """Decode each run (a list of 49-bit lists) with a fresh Go decoder; returns
    one float PCM array per run."""
    import subprocess
    import tempfile
    out = []
    with tempfile.TemporaryDirectory(prefix="ambe-") as tmp:
        for k, run in enumerate(runs):
            src, dst = f"{tmp}/{k}.bits", f"{tmp}/{k}.raw"
            with open(src, "w") as fh:
                fh.write("".join("".join(map(str, b)) + "\n" for b in run))
            subprocess.run([dec, src, dst], check=True, capture_output=True)
            out.append(np.fromfile(dst, "<i2").astype(np.float64))
    return out


def to_fields(bits):
    b = [0] * 9
    for pos, (f, sig) in LAYOUT.items():
        b[f] |= bits[pos] << sig
    return b


def cmd_dump(path):
    for k, bits in enumerate(read_frames(path)):
        print(k, " ".join(f"{v:3d}" for v in to_fields(bits)))


def cmd_fields(a, b):
    fa = [to_fields(x) for x in read_frames(a)]
    fb = [to_fields(x) for x in read_frames(b)]
    n = min(len(fa), len(fb))
    print(f"frames: {len(fa)} vs {len(fb)} (comparing {n})")
    fa, fb = np.array(fa[:n]), np.array(fb[:n])
    bits_a = np.array(read_frames(a)[:n]); bits_b = np.array(read_frames(b)[:n])
    print(f"identical frames: {np.all(fa == fb, axis=1).sum()}/{n}   bit agreement: {(bits_a == bits_b).mean():.3f}")
    for i, name in enumerate(NAMES):
        eq = (fa[:, i] == fb[:, i]).mean()
        mad = np.abs(fa[:, i] - fb[:, i]).mean()
        print(f"  {name:10s} exact {eq:6.1%}   mean|diff| {mad:6.2f}")


def read_pcm(path):
    return np.fromfile(path, dtype="<i2").astype(np.float64)


def estimate_delay(ref, deg, max_lag=800):
    """Lag (samples) of deg relative to ref, from smoothed-envelope cross-correlation
    (vocoders don't preserve phase, so raw-waveform xcorr is unreliable)."""
    def env(x):
        k = np.ones(80) / 80
        return np.convolve(np.abs(x), k, mode="same")
    r, d = env(ref), env(deg)
    n = min(len(r), len(d))
    r, d = r[:n] - r[:n].mean(), d[:n] - d[:n].mean()
    best, best_lag = -np.inf, 0
    for lag in range(0, max_lag + 1):
        c = np.dot(r[:n - lag], d[lag:n])
        if c > best:
            best, best_lag = c, lag
    return best_lag


def cmd_quality(ref_path, *deg_paths):
    from pesq import pesq
    from pystoi import stoi
    ref = read_pcm(ref_path)
    for p in deg_paths:
        deg = read_pcm(p)
        lag = estimate_delay(ref, deg)
        d = deg[lag:]
        n = min(len(ref), len(d))
        r, d = ref[:n], d[:n]
        try:
            pq = pesq(8000, r.astype(np.int16), d.astype(np.int16), "nb")
        except Exception as e:  # noqa: BLE001
            pq = float("nan")
            print(f"  pesq failed: {e}")
        st = stoi(r, d, 8000, extended=False)
        rms = np.sqrt(np.mean(d ** 2)) / max(1e-9, np.sqrt(np.mean(r ** 2)))
        print(f"{p}: delay {lag} samples ({lag / 8:.1f} ms)  PESQ-NB {pq:.3f}  STOI {st:.3f}  level ratio {rms:.2f}")


if __name__ == "__main__":
    if len(sys.argv) < 3:
        raise SystemExit(__doc__)
    {"fields": cmd_fields, "dump": cmd_dump, "quality": cmd_quality}[sys.argv[1]](*sys.argv[2:])
