#!/usr/bin/env python3
"""Steady voiced frames (one frame repeated) with various spectral fields
b3..b8: per-harmonic levels of the MD-380 decoder output vs the Go decoder
output, and vs the Go decoder's dequantized log2 M_l (no enhancement)."""
import os, subprocess, sys, tempfile
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
import mbelib_tables
W0 = mbelib_tables.load()["AmbeW0table"]
SP = tempfile.mkdtemp(prefix='ambe-')
def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b
def fit(x, f0, L):
    t = np.arange(len(x))
    A = np.column_stack([np.cos(2*np.pi*f0*l*t) for l in range(1, L+1)] + [np.sin(2*np.pi*f0*l*t) for l in range(1, L+1)])
    c, *_ = np.linalg.lstsq(A, x, rcond=None)
    return np.hypot(c[:L], c[L:])
rng = np.random.default_rng(int(os.environ.get("SEED", "1")))
b0 = int(os.environ.get("B0", "60")); b2 = int(os.environ.get("B2", "16"))
combos = [[87, 78, 8, 14, 13, 1]] + [[int(rng.integers(512)), int(rng.integers(128)), int(rng.integers(32)), int(rng.integers(16)), int(rng.integers(16)), int(rng.integers(8))] for _ in range(int(os.environ.get("N", "12")))]
allv = []
for c in combos:
    seq = [[b0, 0, b2] + c] * 40
    v = Md380Vocoder()
    fw = np.concatenate([np.array(v.decode_frame(bits(f)), float) for f in seq])
    open(SP + '/h.bits', 'w').write('\n'.join(''.join(map(str, bits(f))) for f in seq) + '\n')
    subprocess.run(['bin/ambe-dec', SP + '/h.bits', SP + '/h.raw'], check=True, capture_output=True)
    go = np.fromfile(SP + '/h.raw', '<i2').astype(float)
    line = subprocess.run(['bin/ambe-params', SP + '/h.bits'], capture_output=True, text=True).stdout.splitlines()[-1].split()
    L = int(line[12]); lm = np.array(list(map(float, line[15:15+L])))
    f0 = W0[b0]
    seg = slice(160*30, 160*38)
    hf, hg = fit(fw[seg], f0, L), fit(go[seg], f0, L)
    d = 20*np.log10(hg/hf)
    e = 20*np.log10(hg) - 20*np.log10(2**lm)
    e -= np.mean(e)
    allv.append(d - np.mean(d))
    x = 20*np.log10(2)*(lm - lm.mean()); ym = 20*np.log10(hf); ym -= ym.mean(); yg = 20*np.log10(hg); yg -= yg.mean()
    for nm, y in (("MD", ym), ("Go", yg)):
        a = np.dot(x, y)/np.dot(x, x); r = y - a*x
        print(f"   {nm}: slope vs dequantized shape {a:4.2f}, residual sd {np.std(r):4.2f} dB, sd of shape {np.std(x):4.2f}")
    print(f"b3..b8={c}: L={L} Go-MD380 mean {np.mean(d):+5.2f} dB, sd {np.std(d):4.2f} dB | Go out vs log2M sd {np.std(e):4.2f}")
    if os.environ.get("T"):
        print("    l    x(spec)   Go-out   MD-out   (dB, mean removed)")
        for l in range(L): print(f"   {l+1:2d} {x[l]:+8.2f} {yg[l]:+8.2f} {ym[l]:+8.2f}")
    if os.environ.get("V"):
        print("   per-harmonic Go-MD380:", " ".join(f"{x:+.1f}" for x in d))
        print("   per-harmonic Go-lm:   ", " ".join(f"{x:+.1f}" for x in e))

A = np.array(allv)
print("mean Go-MD per harmonic (dB, per-frame mean removed), over", len(A), "frames:")
print("   " + " ".join(f"{x:+.1f}" for x in A.mean(0)))
print("   sd across frames: " + " ".join(f"{x:.1f}" for x in A.std(0)))
