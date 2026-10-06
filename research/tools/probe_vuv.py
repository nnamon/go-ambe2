#!/usr/bin/env python3
"""Black-box probe of the MD-380 decoder's handling of each V/UV codeword b1.
For steady synthetic frames (fixed pitch, flat spectrum) it measures, per 500 Hz
band, the fraction of energy at harmonic frequencies and the band energy
relative to the all-voiced codeword.  Output: JSON with per-pitch tables."""
import json, sys
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
import mbelib_tables
T = mbelib_tables.load()

def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b

b3 = int(np.argmin((T['AmbePRBA24']**2).sum(1))); b4 = int(np.argmin((T['AmbePRBA58']**2).sum(1)))
hoc = [int(np.argmin((T[k]**2).sum(1))) for k in ('AmbeHOCb5', 'AmbeHOCb6', 'AmbeHOCb7', 'AmbeHOCb8')]
res = {}
for f0t in (95.0, 125.0, 170.0, 230.0):
    b0 = int(np.argmin(np.abs(T['AmbeW0table'] * 8000 - f0t))); f0 = T['AmbeW0table'][b0] * 8000
    rows = []
    for b1 in range(32):
        v = Md380Vocoder()
        fr = bits([b0, b1, 14, b3, b4] + hoc)
        pcm = np.concatenate([np.array(v.decode_frame(fr), float) for _ in range(80)])[160*20:]
        n = len(pcm); X = np.abs(np.fft.rfft(pcm * np.hanning(n)))**2; fz = np.fft.rfftfreq(n, 1/8000)
        harm = np.zeros_like(X, bool)
        for h in np.arange(f0, 4000, f0): harm |= np.abs(fz - h) < 5
        hf, en = [], []
        for j in range(8):
            band = (fz >= 500*j) & (fz < 500*(j+1))
            tot = X[band].sum(); en.append(tot); hf.append(X[band & harm].sum() / max(tot, 1e-12))
        rows.append((hf, en))
    base = rows[0][1]
    res[f"{f0:.1f}"] = {"b0": b0, "harmfrac": [r[0] for r in rows], "relenergy": [[e / max(b, 1e-12) for e, b in zip(r[1], base)] for r in rows]}
    print(f"f0={f0:.1f} Hz (b0={b0})")
    for b1 in range(32):
        hf = res[f"{f0:.1f}"]["harmfrac"][b1]; re = res[f"{f0:.1f}"]["relenergy"][b1]
        print(f"  b1={b1:2d} spec={''.join(map(str, T['AmbeVuv'][b1]))} harm " + " ".join(f"{x:4.2f}" for x in hf) + "  energy " + " ".join(f"{x:5.2f}" for x in re))
json.dump(res, open('testdata/vuv_probe.json', 'w'), indent=1)
