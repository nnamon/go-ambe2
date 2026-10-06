#!/usr/bin/env python3
"""Fit a phase-regeneration model to testdata/phase_probe.json (MD-380 decoder phases)."""
import json, itertools
import numpy as np
cases = json.load(open('testdata/phase_probe.json'))

def model_theta(B, L, h, edge):
    D = len(h)
    if edge == 'zero':
        ext = lambda l: B[l-1] if 1 <= l <= L else 0.0
    elif edge == 'clamp':
        ext = lambda l: B[min(max(l, 1), L) - 1]
    th = np.zeros(L)
    for l in range(1, L+1):
        th[l-1] = sum(h[m-1] * (ext(l+m) - ext(l-m)) for m in range(1, D+1))
    return th

def err(h, edge, btype, subset=None):
    tot = 0.0; wsum = 0.0
    for c in (subset or cases):
        L = c['L']; lm = np.array(c['log2M'])
        B = lm if btype == 'log2' else 2**(btype*lm)
        th = model_theta(B, L, h, edge)
        mrel = th - np.arange(1, L+1) * th[0]
        rel = np.array(c['rel']); a = np.array(c['amp'])
        e = np.abs(np.angle(np.exp(1j*(rel - mrel))))
        tot += np.sum(a * e); wsum += np.sum(a)
    return tot / wsum

sub = cases[:80]
print("baseline zero phase:", round(err([0.0], 'zero', 'log2', sub), 3))
best = None
for D, edge, s in itertools.product([1, 2, 3, 5, 8, 12, 19], ['zero', 'clamp'], [-2, -1.5, -1, -0.7, -0.5, -0.3, 0.3, 0.5, 0.7, 1, 1.5, 2]):
    h = [s / m for m in range(1, D+1)]
    e = err(h, edge, 'log2', sub)
    if best is None or e < best[0]:
        best = (e, D, edge, s)
print("best 1/m kernel on log2 M: err %.3f  D=%d edge=%s scale=%.2f" % best)
