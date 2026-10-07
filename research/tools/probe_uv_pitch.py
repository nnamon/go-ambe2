#!/usr/bin/env python3
"""Steady all-unvoiced frames at several pitch codes (b0), decoded by the
MD-380 decoder and the Go decoder: overall level and long-term spectrum
difference (dB per 250 Hz band).  Same spectral fields b2..b8 throughout."""
import os, subprocess, sys, tempfile
import numpy as np
from scipy.signal import welch
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
spec = [int(x) for x in (sys.argv[1] if len(sys.argv) > 1 else "16,87,78,8,14,13,1").split(",")]
b1 = int(os.environ.get("B1", "16"))
edges = np.arange(0, 4001, 500)
print(f"b1={b1} b2..b8={spec}: Go minus MD-380, dB per band " + " ".join(f"{a}-{b}" for a, b in zip(edges[:-1], edges[1:])))
for b0 in (2, 10, 20, 30, 45, 60, 80, 100, 119):
    seq = [[b0, b1] + spec] * 60
    v = Md380Vocoder()
    fw = np.concatenate([np.array(v.decode_frame(bits(f)), float) for f in seq])[1600:]
    open(SP + '/p.bits', 'w').write('\n'.join(''.join(map(str, bits(f))) for f in seq) + '\n')
    subprocess.run(['bin/mbevoc-dec', SP + '/p.bits', SP + '/p.raw'], check=True, capture_output=True, env=os.environ)
    go = np.fromfile(SP + '/p.raw', '<i2').astype(float)[1600:]
    fq, pf = welch(fw, 8000, nperseg=256); _, pg = welch(go, 8000, nperseg=256)
    band = lambda p: np.array([p[(fq >= a) & (fq < b)].sum() for a, b in zip(edges[:-1], edges[1:])])
    d = 10 * np.log10(band(pg) / band(pf))
    L = int(np.floor(0.9254 * np.floor(np.pi / (2*np.pi*W0[b0]) + 0.25)))
    print(f"b0={b0:3d} f0={W0[b0]*8000:6.1f} Hz L={L:2d}  level Go/MD-380 {20*np.log10(go.std()/fw.std()):+5.2f} dB   " + " ".join(f"{x:+5.1f}" for x in d))
