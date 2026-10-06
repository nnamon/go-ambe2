#!/usr/bin/env python3
"""Per-frame envelope error (LPC log-spectral distance to the input, shape
only) of two decoders on the same bitstreams, grouped by the value of one
field (b3 or b4 by default): is the second decoder's excess error spread
evenly over codebook entries or concentrated in some?
Usage: excess_by_index.py enc decA decB"""
import collections, glob, os, subprocess, sys
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_pcm, read_frames, to_fields
from scipy.linalg import solve_toeplitz
from scipy.signal import freqz
win = np.hamming(240)
def env(x, order=14):
    """LPC envelope in dB at 64 frequencies (as tools/lsd.py)."""
    x = x * win
    r = np.correlate(x, x, 'full')[len(x) - 1:len(x) + order]
    if r[0] <= 1e-6:
        return None
    r[0] *= 1.0001
    a = solve_toeplitz(r[:order], -r[1:order + 1])
    err = r[0] + np.dot(a, r[1:order + 1])
    w, h = freqz([np.sqrt(max(err, 1e-9))], np.r_[1, a], worN=64)
    return 20 * np.log10(np.abs(h) + 1e-9)
SET = os.environ.get("SET", "testdata/heldout")
enc, decA, decB = sys.argv[1:4]
encdelay = int(os.environ["ENCDELAY"]) if "ENCDELAY" in os.environ else {"fw": 2, "go": 3, "la1": 2, "op25": 3, "op25d": 3}[enc]  # frames of encoder delay
rows = []
for d in sorted(glob.glob(SET + "/*/")):
    ref = read_pcm(d + "ref.raw")
    outs = []
    for dec in (decA, decB):
        o = read_pcm(f"{d}{enc}.{dec}.raw"); outs.append(o[estimate_delay(ref, o):])
    F = [to_fields(b) for b in read_frames(d + enc + ".amb")]
    for k in range(len(F) - encdelay):
        f = F[k + encdelay]
        if f[0] >= 120: continue
        s = 160 * k - 40
        if s < 0 or s + 240 > min(len(ref), *map(len, outs)): continue
        a = env(ref[s:s + 240])
        if a is None or np.max(np.abs(ref[s:s+240])) < 200: continue
        ds = []
        for o in outs:
            b = env(o[s:s + 240])
            if b is None: break
            dd = (a - a.mean()) - (b - b.mean()); ds.append(np.sqrt(np.mean(dd ** 2)))
        if len(ds) == 2:
            rows.append((f, ds[0], ds[1]))
print(f"{enc} streams, {len(rows)} voice frames: LSD {decA} {np.mean([r[1] for r in rows]):.2f} dB, {decB} {np.mean([r[2] for r in rows]):.2f} dB")
rng = np.random.default_rng(0)
for fi in map(int, os.environ.get("FIELDS", "3,4,5,6,7,8,1").split(",")):
    g = collections.defaultdict(list)
    for f, a, b in rows: g[f[fi]].append(b - a)
    keys = [k for k, v in g.items() if len(v) >= 8]
    means = np.array([np.mean(g[k]) for k in keys]); ns = np.array([len(g[k]) for k in keys])
    allx = np.array([b - a for _, a, b in rows])
    # spread of per-index means vs the spread expected by chance (same group sizes, shuffled)
    null = []
    for _ in range(200):
        p = rng.permutation(allx); i = 0; m = []
        for n in ns: m.append(p[i:i+n].mean()); i += n
        null.append(np.std(m))
    worst = sorted(zip(means, keys, ns), reverse=True)[:6]
    print(f"  b{fi}: {len(keys)} values with >=8 frames; sd of per-value mean excess {np.std(means):.3f} dB vs chance {np.mean(null):.3f}±{np.std(null):.3f}; "
          f"worst: " + ", ".join(f"{k}:{m:+.2f}(n={n})" for m, k, n in worst))
