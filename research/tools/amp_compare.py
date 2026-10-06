#!/usr/bin/env python3
"""Compare MD-380 decoder harmonic amplitudes (steady all-voiced frames) with the
spec reconstruction, with and without TIA-102.BABA ch.8 enhancement."""
import json
import numpy as np
cases = json.load(open('testdata/phase_probe.json'))
def enhance(w0, M):
    L = len(M); l = np.arange(1, L+1)
    r0 = np.sum(M**2); r1 = np.sum(M**2 * np.cos(w0*l))
    k = 0.96*np.pi/(w0*r0*(r0**2 - r1**2))
    x = k*(r0**2 + r1**2 - 2*r0*r1*np.cos(w0*l))
    W = np.sqrt(M)*np.power(np.maximum(x, 0), 0.25)
    E = M.copy()
    m = 8*l > L
    E[m] = np.where(W[m] > 1.2, 1.2*M[m], np.where(W[m] < 0.5, 0.5*M[m], W[m]*M[m]))
    return E*np.sqrt(r0/np.sum(E**2))
rows = []
for c in cases:
    L = c['L']; w0 = 2*np.pi*c['f0']; M = 2**np.array(c['log2M']); fw = np.array(c['amp'])
    for name, model in (("plain", 2*M), ("enhanced", 2*enhance(w0, M))):
        d = 20*np.log10(fw/model)
        rows.append((name, L, d))
for name in ("plain", "enhanced"):
    ds = [d for n, L, d in rows if n == name]
    allv = np.concatenate(ds)
    # position-binned mean (fraction of L)
    pos = np.concatenate([np.arange(1, len(d)+1)/len(d) for d in ds])
    bins = np.linspace(0, 1, 9)
    prof = [np.mean(allv[(pos > a) & (pos <= b)]) for a, b in zip(bins[:-1], bins[1:])]
    shape = np.concatenate([d - d.mean() for d in ds])
    print(f"{name:9s}: mean fw/model {allv.mean():+.2f} dB, shape RMS (per-frame mean removed) {np.sqrt(np.mean(shape**2)):.2f} dB")
    print(f"           profile over l/L octiles: " + " ".join(f"{x:+.1f}" for x in prof))
