#!/usr/bin/env python3
"""How does the MD-380 encoder decide to send tone frames (TIA-102.BABA-1
clause 7.1 leaves the method open)?  Black-box sweeps: synthetic tones in,
the encoder's frames out; nothing inside the firmware is read.

  probe_tone_detect.py [section ...]   (default: all)

Sections: single, grid, level, pairs, offset, twist, duration, noise, timing.
Each signal is 10 frames of silence, the test signal, 10 frames of silence,
on a fresh vocoder.  A frame is a tone frame when its first six bits are
ones; its index is the majority of the four copies and its amplitude AD
comes from u0 and u3 (Table 10)."""
import sys
import numpy as np
sys.path.insert(0, 'oracle/unicorn')
from md380_uc import Md380Vocoder

FS, N = 8000, 160
PAD = 10
DTMF = {128: (1336, 941), 129: (1209, 697), 130: (1336, 697), 131: (1477, 697), 132: (1209, 770),
        133: (1336, 770), 134: (1477, 770), 135: (1209, 852), 136: (1336, 852), 137: (1477, 852),
        138: (1633, 697), 139: (1633, 770), 140: (1633, 852), 141: (1633, 941), 142: (1209, 941),
        143: (1477, 941)}
KNOX = {144: (1162, 820), 145: (1052, 606), 146: (1162, 606), 147: (1279, 606), 148: (1052, 672),
        149: (1162, 672), 150: (1279, 672), 151: (1052, 743), 152: (1162, 743), 153: (1279, 743),
        154: (1430, 606), 155: (1430, 672), 156: (1430, 743), 157: (1430, 820), 158: (1052, 820),
        159: (1279, 820)}
PROGRESS = {160: (440, 350), 161: (480, 440), 162: (620, 480), 163: (490, 350)}


def num(bits):
    return int(''.join(map(str, bits)), 2)


def tone_of(b):
    """(index, AD) of a tone frame, or None for any other frame."""
    if b[:6] != [1] * 6:
        return None
    u1, u2, u3 = b[12:24], b[24:35], b[35:49]
    ids = [num(u1[:8]), num(u1[8:] + u2[:4]), num(u2[4:] + [u3[0]]), num(u3[1:9])]
    idx = max(set(ids), key=ids.count)
    return idx, num(b[6:12]) << 1 | u3[9]


def encode(x):
    """Per-frame tone_of() for signal x (float samples) with silent padding."""
    x = np.concatenate([np.zeros(PAD * N), x, np.zeros(PAD * N)])
    x = np.clip(np.round(x), -32768, 32767).astype(int)
    v = Md380Vocoder()
    return [tone_of(v.encode_frame(x[k * N:(k + 1) * N].tolist())) for k in range(len(x) // N)]


def tones(freqs, amps, frames, phase=0.0):
    t = np.arange(frames * N) / FS
    return sum(a * np.sin(2 * np.pi * f * t + phase * i) for i, (f, a) in enumerate(zip(freqs, amps)))


def summary(res):
    """(first tone frame - signal start, tone frame count, most common (id, AD))."""
    hits = [(k - PAD, r) for k, r in enumerate(res) if r]
    if not hits:
        return None, 0, None
    vals = [r for _, r in hits]
    return hits[0][0], len(hits), max(set(vals), key=vals.count)


def single():
    print("== single tones, 30 frames at 8000 peak: f -> first frame, count, (ID, AD)")
    for f in list(range(100, 4000, 50)) + [156.25, 187.5, 3812.5, 3843.75]:
        s = summary(encode(tones([f], [8000], 30)))
        print(f"  {f:8.2f} Hz  ID {f / 31.25:6.2f}: {s}")


def grid():
    print("== offsets from the 31.25 Hz grid: k, delta -> count, (ID, AD)")
    for k in (10, 32, 64, 100):
        row = []
        for d in range(-16, 17, 2):
            s = summary(encode(tones([31.25 * k + d], [8000], 30)))
            row.append(f"{d:+d}:{s[1]}/{s[2][0] if s[2] else '-'}")
        print(f"  k={k:3d} ({31.25 * k:7.2f} Hz) " + " ".join(row))


def level():
    print("== level of a 1000 Hz tone (ID 32): peak dBFS -> count, (ID, AD)")
    for db in range(-60, 1, 2):
        s = summary(encode(tones([1000], [32767 * 10 ** (db / 20)], 30)))
        print(f"  {db:4d} dBFS: {s}")
    print("== level of DTMF '5' (each tone): peak dBFS -> count, (ID, AD)")
    for db in range(-60, -5, 2):
        a = 32767 * 10 ** (db / 20)
        s = summary(encode(tones([1336, 770], [a, a], 30)))
        print(f"  {db:4d} dBFS: {s}")


def pairs():
    print("== Table 9 pairs, 30 frames, 6000 peak each: ID -> count, (ID, AD)")
    for name, table in (("DTMF", DTMF), ("KNOX", KNOX), ("call progress", PROGRESS)):
        for i, (f1, f2) in table.items():
            s = summary(encode(tones([f1, f2], [6000, 6000], 30)))
            print(f"  {name:13s} {i}: {f1}/{f2} Hz -> {s}")


def offset():
    print("== DTMF '5' (1336/770) with both tones offset: percent -> count, (ID, AD)")
    for pct in (-6, -4, -3, -2, -1.5, -1, 0, 1, 1.5, 2, 3, 4, 6):
        s = summary(encode(tones([1336 * (1 + pct / 100), 770 * (1 + pct / 100)], [6000, 6000], 30)))
        print(f"  {pct:+5.1f}%: {s}")
    print("== DTMF '5' with only the high tone offset")
    for pct in (-4, -3, -2, -1.5, -1, 1, 1.5, 2, 3, 4):
        s = summary(encode(tones([1336 * (1 + pct / 100), 770], [6000, 6000], 30)))
        print(f"  {pct:+5.1f}%: {s}")


def twist():
    print("== DTMF '5', high tone level relative to the low (6000 peak): dB -> count, (ID, AD)")
    for db in range(-20, 21, 2):
        s = summary(encode(tones([1336, 770], [6000 * 10 ** (db / 20), 6000], 30)))
        print(f"  {db:+3d} dB: {s}")


def duration():
    print("== DTMF '5' bursts: samples (start offset) -> tone frames and their positions")
    for start in (0, 80):
        for frames in (1, 2, 3, 4, 5, 6, 8):
            x = np.concatenate([np.zeros(start), tones([1336, 770], [6000, 6000], frames)])
            res = encode(x)
            pos = [k - PAD for k, r in enumerate(res) if r]
            print(f"  {frames * 20:3d} ms at +{start:2d} samples: {len(pos)} tone frames at {pos}")


def noise():
    rng = np.random.default_rng(1)
    print("== DTMF '5' + white noise: SNR (dB) -> tone frames of 30, (ID, AD)")
    for snr in (40, 30, 25, 20, 15, 10, 5, 0):
        x = tones([1336, 770], [6000, 6000], 30)
        p = np.mean(x ** 2) / 10 ** (snr / 10)
        s = summary(encode(x + rng.normal(0, np.sqrt(p), len(x))))
        print(f"  {snr:3d} dB: {s}")
    print("== 1000 Hz + white noise")
    for snr in (40, 30, 25, 20, 15, 10, 5, 0):
        x = tones([1000], [8000], 30)
        p = np.mean(x ** 2) / 10 ** (snr / 10)
        s = summary(encode(x + rng.normal(0, np.sqrt(p), len(x))))
        print(f"  {snr:3d} dB: {s}")


def timing():
    print("== frame types around a 30-frame DTMF '5' (T tone, . other), from 3 frames before")
    for start in (0, 40, 80, 120):
        x = np.concatenate([np.zeros(start), tones([1336, 770], [6000, 6000], 30)])
        res = encode(x)
        print(f"  start +{start:3d} samples: " + "".join("T" if r else "." for r in res[PAD - 3:PAD + 36]))
    print("== the same with speech-like harmonics before and after (150 Hz, 12 harmonics)")
    t = np.arange(20 * N) / FS
    voice = sum(2000 / l * np.sin(2 * np.pi * 150 * l * t) for l in range(1, 13))
    x = np.concatenate([voice, tones([1336, 770], [6000, 6000], 30), voice])
    res = encode(x)
    print("  " + "".join("T" if r else "." for r in res[PAD + 17:PAD + 55]))


SECTIONS = dict(single=single, grid=grid, level=level, pairs=pairs, offset=offset, twist=twist,
                duration=duration, noise=noise, timing=timing)
if __name__ == "__main__":
    for name in sys.argv[1:] or SECTIONS:
        SECTIONS[name]()
