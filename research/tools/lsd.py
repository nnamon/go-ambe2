#!/usr/bin/env python3
"""Log-spectral distance (dB) between input and decoded output LPC envelopes,
per voicing class (from the firmware encoder's decisions). Usage: lsd.py tag..."""
import glob, os, subprocess, sys
import numpy as np
from scipy.linalg import solve_toeplitz
from scipy.signal import freqz
sys.path.insert(0, 'tools')
from ambe_eval import estimate_delay, read_pcm
SET = os.environ.get("SET", "testdata/matrix")
win = np.hamming(240)

def env(x, order=14):
    x = x * win
    r = np.correlate(x, x, 'full')[len(x) - 1:len(x) + order]
    if r[0] <= 1e-6:
        return None
    r[0] *= 1.0001
    a = solve_toeplitz(r[:order], -r[1:order + 1])
    err = r[0] + np.dot(a, r[1:order + 1])
    w, h = freqz([np.sqrt(max(err, 1e-9))], np.r_[1, a], worN=64)
    return 20 * np.log10(np.abs(h) + 1e-9)

for tag in sys.argv[1:]:
    acc = {}
    for d in sorted(glob.glob(SET + "/*/")):
        ref = read_pcm(d + "ref.raw"); out = read_pcm(d + (f"{tag}.raw" if "." in tag else f"{tag}.fw.raw"))
        lag = estimate_delay(ref, out); out = out[lag:]
        lines = subprocess.run(["bin/ambe-params", d + "fw.amb"], capture_output=True, text=True).stdout.splitlines()
        for k, line in enumerate(lines[2:]):
            t = line.split()
            if t[1] == "S":
                continue
            vf = np.mean([c == "1" for c in t[14]])
            cls = "voiced" if vf >= 0.75 else "unvoiced" if vf == 0 else "mixed"
            s = 160 * k - 40
            if s < 0 or s + 240 > min(len(ref), len(out)):
                continue
            a, b = env(ref[s:s + 240]), env(out[s:s + 240])
            if a is None or b is None:
                continue
            dd = (a - a.mean()) - (b - b.mean())   # shape only
            acc.setdefault(cls, []).append(np.sqrt(np.mean(dd ** 2)))
    print(f"{tag:6s} " + "  ".join(f"{c}: LSD {np.mean(v):.2f} dB (n={len(v)})" for c, v in sorted(acc.items())))
