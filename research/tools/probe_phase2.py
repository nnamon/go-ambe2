#!/usr/bin/env python3
"""Harmonic relative phases over time for steady voiced frames: firmware vs Go decoder."""
import subprocess, sys, os
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

def relphase(pcm, f0, L, start, nper=6):
    n = int(round(nper / f0)); t = np.arange(n) + start; seg = pcm[start:start+n]
    A = np.column_stack([np.cos(2*np.pi*f0*l*t) for l in range(1, L+1)] + [np.sin(2*np.pi*f0*l*t) for l in range(1, L+1)])
    c, *_ = np.linalg.lstsq(A, seg, rcond=None)
    ph = np.arctan2(-c[L:], c[:L]); amp = np.hypot(c[:L], c[L:])
    resid = np.sqrt(np.mean((seg - A @ c)**2)) / np.sqrt(np.mean(seg**2))
    return np.angle(np.exp(1j*(ph - np.arange(1, L+1)*ph[0]))), amp, resid

b0 = 60; f0 = T['AmbeW0table'][b0]; L = int(T['AmbeLtable'][b0])
fr = bits([b0, 0, 16, 300, 70, 3, 3, 3, 3])
v = Md380Vocoder()
fw = np.concatenate([np.array(v.decode_frame(fr), float) for _ in range(60)])
open(SP + '/p.bits', 'w').write('\n'.join(''.join(map(str, fr)) for _ in range(60)) + '\n')
subprocess.run(['bin/mbevoc-dec', SP + '/p.bits', SP + '/p.raw'], capture_output=True)
go = np.fromfile(SP + '/p.raw', '<i2').astype(float)
for name, pcm in (("firmware", fw), ("go", go)):
    print(f"== {name} decoder, f0={f0*8000:.1f} Hz, L={L}")
    for frame in (30, 31, 40, 50):
        rel, amp, res = relphase(pcm, f0, L, 160 * frame)
        print(f"   frame {frame}: fit residual {res:.3f}  rel phase l=2..9: " + " ".join(f"{x:+.2f}" for x in rel[1:9]))
