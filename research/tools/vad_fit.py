#!/usr/bin/env python3
"""Fit a simple adaptive VAD to the firmware encoder's silence decisions.
Frame k of the firmware bitstream is aligned with input frame k-LAG."""
import glob, itertools, sys
import numpy as np
sys.path.insert(0, 'tools')
from ambe_eval import read_frames, to_fields
LAG = 2

def load(setdir):
    out = []
    for d in sorted(glob.glob(setdir + '/*/')):
        ref = np.fromfile(d + 'ref.raw', '<i2').astype(float)
        y = np.zeros_like(ref); px = py = 0.0
        for i, x in enumerate(ref):
            py = x - px + 0.99 * py; px = x; y[i] = py
        n = len(ref) // 160
        e = np.array([np.mean(y[160*k:160*k+160]**2) for k in range(n)])
        sil = np.array([to_fields(b)[0] >= 124 for b in read_frames(d + 'fw.amb')])[:n]
        sil = sil[LAG:]; e = e[:len(e) - LAG]
        out.append((e, sil))
    return out

def vad(e, ratio, floor, hang, pre, rise):
    n = len(e); noise = None; speech = np.zeros(n, bool); hcount = 0
    for k in range(n):
        v = e[k]
        noise = v if noise is None else (v if v < noise else noise * rise)
        act = lambda x: x > max(noise * ratio, floor)
        a = any(act(e[j]) for j in range(k, min(n, k + pre + 1)))
        if a:
            hcount = hang
        elif hcount > 0:
            hcount -= 1
        speech[k] = a or hcount > 0
    return ~speech

def score(data, p):
    agree = np.concatenate([vad(e, *p) == s for e, s in data])
    return agree.mean()

dev = load('testdata/matrix'); test = load('testdata/heldout')
grid = itertools.product([4, 8, 16, 32], [500, 2000, 5000], [0, 2, 4, 8], [0, 1, 2], [1.001, 1.003, 1.01])
best = max(((score(dev, p), p) for p in grid))
print('best on dev: agreement %.3f params ratio,floor,hang,pre,rise=%s' % best)
print('held-out agreement with same params: %.3f' % score(test, best[1]))
