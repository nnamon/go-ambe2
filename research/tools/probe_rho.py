#!/usr/bin/env python3
"""Step response of the MD-380 decoder's spectral shape: switch from a neutral
steady state to a shaped one and measure convergence per frame (spec: rho=0.65)."""
import subprocess, sys
import tempfile
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
import mbelib_tables
T = mbelib_tables.load()
SP = tempfile.mkdtemp(prefix='ambe-')
def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b
nz = lambda k: int(np.argmin((T[k]**2).sum(1)))
hoc = [nz('AmbeHOCb5'), nz('AmbeHOCb6'), nz('AmbeHOCb7'), nz('AmbeHOCb8')]
def amps(pcm, f0, L, start, n):
    t = np.arange(n)+start; seg = pcm[start:start+n]
    A = np.column_stack([np.cos(2*np.pi*f0*l*t) for l in range(1, L+1)]+[np.sin(2*np.pi*f0*l*t) for l in range(1, L+1)])
    c, *_ = np.linalg.lstsq(A, seg, rcond=None); return np.hypot(c[:L], c[L:])
b0 = 50; f0 = T['AmbeW0table'][b0]; L = int(T['AmbeLtable'][b0])
for b3 in (300, 37, 450):
    base = [b0, 0, 14, nz('AmbePRBA24'), nz('AmbePRBA58')] + hoc
    step = [b0, 0, 14, b3, nz('AmbePRBA58')] + hoc
    seq = [base]*20 + [step]*14
    for dec in ("fw", "go"):
        if dec == "fw":
            v = Md380Vocoder(); pcm = np.concatenate([np.array(v.decode_frame(bits(f)), float) for f in seq])
        else:
            open(SP+'/r.bits', 'w').write('\n'.join(''.join(map(str, bits(f))) for f in seq)+'\n')
            subprocess.run(['bin/ambe-dec', SP+'/r.bits', SP+'/r.raw'], capture_output=True)
            pcm = np.fromfile(SP+'/r.raw', '<i2').astype(float)
        n = int(round(3/f0))
        shapes = []
        for k in range(18, 34):
            a = np.log2(amps(pcm, f0, L, 160*k + 160 - n, n) + 1e-6)
            shapes.append(a - a.mean())
        shapes = np.array(shapes)
        final = shapes[-1]; first = shapes[0]
        delta = final - first
        strong = np.argsort(-np.abs(delta))[:6]
        # progress fraction of the step for each frame after the switch
        prog = [(np.dot(s - first, delta) / np.dot(delta, delta)) for s in shapes]
        rem = [1 - p for p in prog[2:12]]   # frames 20..29
        ratios = [rem[i+1]/rem[i] for i in range(len(rem)-1) if abs(rem[i]) > 0.03]
        print(f"b3={b3} {dec}: step progress per frame: " + " ".join(f"{p:.2f}" for p in prog[1:13]) +
              f"  | remaining-ratio median {np.median(ratios) if ratios else float('nan'):.3f}  | final shape range {np.ptp(final)*6.02:.1f} dB")
