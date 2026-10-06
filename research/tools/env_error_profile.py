#!/usr/bin/env python3
"""Mean signed envelope error (decoded minus input, dB, per-frame mean removed)
by frequency band, over steady-pitch voiced frames, for several decoders.
Usage: env_error_profile.py enc dec1 [dec2 ...]"""
import glob, math, os, sys
import numpy as np
from scipy.linalg import solve_toeplitz
from scipy.signal import freqz
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_frames, read_pcm, to_fields
import mbelib_tables
W0 = mbelib_tables.load()["AmbeW0table"]
SET = os.environ.get("SET", "testdata/heldout")
enc, decs = sys.argv[1], sys.argv[2:]
encdelay = int(os.environ["ENCDELAY"]) if "ENCDELAY" in os.environ else {"fw": 2, "go": 3, "la1": 2, "op25": 3, "op25d": 3}[enc]  # frames of encoder delay
CLASS = os.environ.get("CLASS", "voiced")
win = np.hamming(240)
NB = 64
def env(x, order=14):
    x = x * win
    r = np.correlate(x, x, 'full')[len(x) - 1:len(x) + order]
    if r[0] <= 1e-6: return None
    r[0] *= 1.0001
    a = solve_toeplitz(r[:order], -r[1:order + 1])
    err = r[0] + np.dot(a, r[1:order + 1])
    _, h = freqz([np.sqrt(max(err, 1e-9))], np.r_[1, a], worN=NB)
    return 20 * np.log10(np.abs(h) + 1e-9)
sums = {d: np.zeros(NB) for d in decs}; sq = {d: np.zeros(NB) for d in decs}; n = 0
lev = {d: [] for d in decs}
for dd in sorted(glob.glob(SET + "/*/")):
    ref = read_pcm(dd + "ref.raw")
    outs = {}
    for d in decs:
        o = read_pcm(f"{dd}{enc}.{d}.raw"); outs[d] = o[estimate_delay(ref, o):]
    F = [to_fields(b) for b in read_frames(f"{dd}{enc}.amb")]
    for k in range(1, len(F) - encdelay):
        f, g = F[k + encdelay], F[k + encdelay - 1]
        if f[0] >= 120 or g[0] >= 120: continue
        if CLASS == "voiced":
            if f[1] not in (0, 1, 2, 3, 4, 5, 6, 7, 8, 9): continue
            if abs(math.log2(W0[f[0]] / W0[g[0]])) >= 0.03: continue
        elif CLASS == "unvoiced":
            if f[1] != 16 and f[1] < 18: continue
        elif CLASS == "mixed":
            if f[1] == 0 or f[1] == 16 or f[1] >= 18: continue
        s = 160 * k - 40
        if s < 0 or s + 240 > min(len(ref), *(len(o) for o in outs.values())): continue
        e0 = env(ref[s:s + 240])
        if e0 is None or np.mean(ref[s:s+240]**2) < 1e4: continue
        es = {d: env(outs[d][s:s + 240]) for d in decs}
        if any(e is None for e in es.values()): continue
        for d, e in es.items():
            lev[d].append(10*np.log10(np.mean(outs[d][s:s+240]**2) / np.mean(ref[s:s+240]**2) + 1e-9))
            diff = (e - e.mean()) - (e0 - e0.mean())
            sums[d] += diff; sq[d] += diff ** 2
        n += 1
f = np.linspace(0, 4000, NB, endpoint=False)
print(f"{enc} bitstreams, {n} {'steady voiced' if CLASS == 'voiced' else CLASS} frames: mean signed envelope error (decoded - input, dB) per 500 Hz band")
print("   band (kHz)      " + " ".join(f"{a/1000:.1f}-{(a+500)/1000:.1f}" for a in range(0, 4000, 500)))
for d in decs:
    m = sums[d] / n
    print(f"   {d:14s}  " + " ".join(f"{m[(f >= a) & (f < a + 500)].mean():+6.2f} " for a in range(0, 4000, 500)) +
          f"   | RMS error {np.sqrt((sq[d] / n).mean()):.2f} dB, level {np.mean(lev[d]):+.2f} dB (sd {np.std(lev[d]):.2f})")
