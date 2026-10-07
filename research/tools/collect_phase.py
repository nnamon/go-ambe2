#!/usr/bin/env python3
"""Collect steady-state harmonic phases of the MD-380 decoder (black box) for
random all-voiced spectra, with the decoded log2 amplitudes from bin/mbevoc-params.
Writes testdata/phase_probe.json."""
import json, subprocess, sys
import tempfile
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
import mbelib_tables
T = mbelib_tables.load()
SP = tempfile.mkdtemp(prefix='ambe-')
rng = np.random.default_rng(7)

def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b

def measure(pcm, f0, L, start, nper=6):
    n = int(round(nper / f0)); t = np.arange(n) + start; seg = pcm[start:start+n]
    A = np.column_stack([np.cos(2*np.pi*f0*l*t) for l in range(1, L+1)] + [np.sin(2*np.pi*f0*l*t) for l in range(1, L+1)])
    c, *_ = np.linalg.lstsq(A, seg, rcond=None)
    ph = np.arctan2(-c[L:], c[:L]); amp = np.hypot(c[:L], c[L:])
    res = np.sqrt(np.mean((seg - A @ c)**2)) / np.sqrt(np.mean(seg**2))
    return ph, amp, res

cases = []
N = int(sys.argv[1]) if len(sys.argv) > 1 else 300
for k in range(N):
    b0 = int(rng.integers(5, 110))
    f0 = T['AmbeW0table'][b0]; L = int(T['AmbeLtable'][b0])
    p = [b0, 0, int(rng.integers(8, 20)), int(rng.integers(512)), int(rng.integers(128)),
         int(rng.integers(32)), int(rng.integers(16)), int(rng.integers(16)), int(rng.integers(8))]
    fr = bits(p)
    v = Md380Vocoder()
    pcm = np.concatenate([np.array(v.decode_frame(fr), float) for _ in range(40)])
    open(SP + '/c.bits', 'w').write('\n'.join(''.join(map(str, fr)) for _ in range(40)) + '\n')
    last = subprocess.run(['bin/mbevoc-params', SP + '/c.bits'], capture_output=True, text=True).stdout.splitlines()[-1].split()
    log2M = list(map(float, last[15:15 + L]))
    ph1, amp1, r1 = measure(pcm, f0, L, 160 * 30)
    ph2, amp2, r2 = measure(pcm, f0, L, 160 * 34)
    # relative phase (remove linear phase via fundamental) at two times; should agree
    rel1 = np.angle(np.exp(1j*(ph1 - np.arange(1, L+1)*ph1[0])))
    rel2 = np.angle(np.exp(1j*(ph2 - np.arange(1, L+1)*ph2[0])))
    cases.append(dict(params=p, f0=f0, L=L, log2M=log2M, rel=rel1.tolist(), rel2=rel2.tolist(),
                      amp=amp1.tolist(), resid=max(r1, r2)))
json.dump(cases, open('testdata/phase_probe.json', 'w'))
st = [np.mean(np.abs(np.angle(np.exp(1j*(np.array(c['rel']) - np.array(c['rel2'])))) * np.array(c['amp'])) / np.sum(c['amp'])) for c in cases]
print(f"{len(cases)} cases; mean amp-weighted phase change between t1 and t2: {np.mean(st):.3f} rad; median fit residual {np.median([c['resid'] for c in cases]):.3f}")
