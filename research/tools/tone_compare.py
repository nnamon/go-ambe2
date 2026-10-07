#!/usr/bin/env python3
"""Tone frames from the MD-380 encoder (black box) and from mbevoc-enc -tones,
side by side, for the conditions of probe_tone_detect.py.  Run from research/
after `make -C tools go`.

For each condition: tone frames out of the signal's 30 frames (with 10 silent
frames either side) and the most common (index, AD), for each encoder."""
import os
import subprocess
import sys
import tempfile
import numpy as np
sys.path.insert(0, 'tools')
from probe_tone_detect import DTMF, KNOX, PROGRESS, N, PAD, encode, tones, tone_of

ENC = os.path.join('bin', 'mbevoc-enc')


def go_encode(x):
    x = np.concatenate([np.zeros(PAD * N), x, np.zeros(PAD * N)])
    x = np.clip(np.round(x), -32768, 32767).astype('<i2')
    with tempfile.TemporaryDirectory() as d:
        raw, bits = os.path.join(d, 'in.raw'), os.path.join(d, 'out.bits')
        x.tofile(raw)
        subprocess.run([ENC, '-tones', raw, bits], check=True, capture_output=True)
        return [tone_of([int(c) for c in line.strip()]) for line in open(bits) if line.strip()]


def summary(res):
    hits = [r for r in res if r]
    return len(hits), (max(set(hits), key=hits.count) if hits else None)


stats = dict(conditions=0, both=0, same_id=0, same_ad=0, md380_only=0, go_only=0)


def compare(name, x):
    m, g = summary(encode(x)), summary(go_encode(x))
    stats['conditions'] += 1
    if m[1] and g[1]:
        stats['both'] += 1
        stats['same_id'] += m[1][0] == g[1][0]
        stats['same_ad'] += abs(m[1][1] - g[1][1]) <= 1
    elif m[1]:
        stats['md380_only'] += 1
    elif g[1]:
        stats['go_only'] += 1
    flag = '' if (m[1] and g[1] and m[1][0] == g[1][0]) or (not m[1] and not g[1]) else '  <--'
    print(f"  {name:34s} MD-380 {m[0]:2d} {str(m[1]):11s}  mbevoc {g[0]:2d} {str(g[1]):11s}{flag}")


def main():
    rng = np.random.default_rng(1)
    print("== single tones (8000 peak)")
    for f in list(range(100, 4000, 100)) + [156.25, 3812.5, 3843.75]:
        compare(f"{f} Hz", tones([f], [8000], 30))
    print("== off the 31.25 Hz grid")
    for k in (10, 32, 100):
        for d in (-16, -14, 14, 16):
            compare(f"{31.25 * k + d:.2f} Hz", tones([31.25 * k + d], [8000], 30))
    print("== level, 1000 Hz")
    for db in range(-60, 1, 6):
        compare(f"{db} dBFS", tones([1000], [32767 * 10 ** (db / 20)], 30))
    print("== Table 9 pairs (6000 peak each)")
    for table in (DTMF, KNOX, PROGRESS):
        for i, (f1, f2) in table.items():
            compare(f"{i}: {f1}/{f2} Hz", tones([f1, f2], [6000, 6000], 30))
    print("== DTMF '5' frequency offsets (both tones / high tone only)")
    for pct in (-4, -3, -2, -1.5, 1.5, 2, 3, 4, 6):
        compare(f"both {pct:+}%", tones([1336 * (1 + pct / 100), 770 * (1 + pct / 100)], [6000, 6000], 30))
    for pct in (-3, -2, 2, 3):
        compare(f"high {pct:+}%", tones([1336 * (1 + pct / 100), 770], [6000, 6000], 30))
    print("== DTMF '5' twist (high re low)")
    for db in (-14, -12, -10, -8, -4, 4, 8, 10, 12, 14):
        compare(f"{db:+} dB", tones([1336, 770], [6000 * 10 ** (db / 20), 6000], 30))
    print("== white noise")
    for snr in (30, 25, 20, 15, 10):
        x = tones([1336, 770], [6000, 6000], 30)
        compare(f"DTMF '5', SNR {snr} dB", x + rng.normal(0, np.sqrt(np.mean(x ** 2) / 10 ** (snr / 10)), len(x)))
    for snr in (30, 25, 20, 15):
        x = tones([1000], [8000], 30)
        compare(f"1000 Hz, SNR {snr} dB", x + rng.normal(0, np.sqrt(np.mean(x ** 2) / 10 ** (snr / 10)), len(x)))
    print("== DTMF '5' bursts")
    for frames in (1, 2, 3, 5, 8):
        compare(f"{frames * 20} ms", tones([1336, 770], [6000, 6000], frames))
    s = stats
    print(f"\n{s['conditions']} conditions: both detect {s['both']} (same index {s['same_id']}, AD within 1: "
          f"{s['same_ad']}); only the MD-380 detects {s['md380_only']}; only mbevoc detects {s['go_only']}")


if __name__ == '__main__':
    main()
