#!/usr/bin/env python3
"""Which quantizer field does the MD-380 decoder interpret differently from the
spec reconstruction?  Vary one field at a time from a neutral baseline and compare
harmonic amplitudes (steady all-voiced frames) with the spec model."""
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
def enhance(w0, M):
    L = len(M); l = np.arange(1, L+1)
    r0 = np.sum(M**2); r1 = np.sum(M**2*np.cos(w0*l)); k = 0.96*np.pi/(w0*r0*(r0**2-r1**2))
    W = np.sqrt(M)*np.power(np.maximum(k*(r0**2+r1**2-2*r0*r1*np.cos(w0*l)), 0), 0.25)
    E = M.copy(); m = 8*l > L
    E[m] = np.where(W[m] > 1.2, 1.2*M[m], np.where(W[m] < 0.5, 0.5*M[m], W[m]*M[m]))
    return E*np.sqrt(r0/np.sum(E**2))
def run(p, nfr=40):
    fr = bits(p); v = Md380Vocoder()
    pcm = np.concatenate([np.array(v.decode_frame(fr), float) for _ in range(nfr)])
    open(SP+'/f.bits', 'w').write('\n'.join(''.join(map(str, fr)) for _ in range(nfr))+'\n')
    last = subprocess.run(['bin/ambe-params', SP+'/f.bits'], capture_output=True, text=True).stdout.splitlines()
    f0 = T['AmbeW0table'][p[0]]; L = int(T['AmbeLtable'][p[0]])
    start = 160*(nfr-6); n = int(round(6/f0)); t = np.arange(n)+start; seg = pcm[start:start+n]
    A = np.column_stack([np.cos(2*np.pi*f0*l*t) for l in range(1, L+1)]+[np.sin(2*np.pi*f0*l*t) for l in range(1, L+1)])
    c, *_ = np.linalg.lstsq(A, seg, rcond=None); amp = np.hypot(c[:L], c[L:])
    lm = np.array(list(map(float, last[-1].split()[15:15+L])))
    model = 2*enhance(2*np.pi*f0, 2**lm)
    d = 20*np.log10(amp/model)
    return d, lm
nz = lambda k: int(np.argmin((T[k]**2).sum(1)))
base = [60, 0, 14, nz('AmbePRBA24'), nz('AmbePRBA58'), nz('AmbeHOCb5'), nz('AmbeHOCb6'), nz('AmbeHOCb7'), nz('AmbeHOCb8')]
d, lm = run(base)
print(f"baseline {base}: shape RMS {np.std(d):.2f} dB, mean {d.mean():+.2f} dB")
rng = np.random.default_rng(1)
for field, n in ((3, 512), (4, 128), (5, 32), (6, 16), (7, 16), (8, 8), (2, 32)):
    rs = []
    for v in rng.choice(n, 6, replace=False):
        p = list(base); p[field] = int(v)
        d, _ = run(p)
        rs.append(np.std(d))
    print(f"vary b{field}: shape RMS over 6 random values: " + " ".join(f"{x:.2f}" for x in rs))
