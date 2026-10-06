#!/usr/bin/env python3
"""Differential sweep of one spectral field: for every value v of field b<f>
(others fixed at a base frame), decode the frame repeated to steady state by
the MD-380 and the Go decoder and fit the harmonic levels.  The change from
the base value, D(v) = level(v) - level(base), should agree between the
decoders wherever they interpret the field alike; per-value mismatch
sd(D_MD - D_Go) singles out entries that differ.
DRAFT=1 gives the Go decoder the frames as a decoder following the
TIA-102.BABA-1 draft's Table 8 (mbelib, OP25) reads them.
Usage: probe_field_sweep.py f"""
import os, sys
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
import mbelib_tables
W0 = mbelib_tables.load()["AmbeW0table"]
from ambe_eval import go_decode_runs
field = int(sys.argv[1])
size = {3: 512, 4: 128, 5: 32, 6: 16, 7: 16, 8: 8}[field]
base = [int(x) for x in os.environ.get("BASE", "60,0,16,87,78,8,14,13,1").split(",")]
NF = 24; seg = slice(160*(NF-7), 160*(NF-1))
f0 = W0[base[0]]
L = int(np.floor(0.9254 * np.floor(np.pi / (2*np.pi*f0) + 0.25)))
t = np.arange(seg.stop - seg.start)
A = np.column_stack([np.cos(2*np.pi*f0*l*t) for l in range(1, L+1)] + [np.sin(2*np.pi*f0*l*t) for l in range(1, L+1)])
Ap = np.linalg.pinv(A)
def lev(x):
    c = Ap @ x[seg]; h = 20*np.log10(np.hypot(c[:L], c[L:]) + 1e-9); return h - h.mean()
def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return ''.join(map(str, b))
frames = []
for v in range(size):
    f = list(base); f[field] = v; frames.append(f)
md = []
for f in frames:
    dec = Md380Vocoder(); bb = [int(c) for c in bits(f)]
    md.append(lev(np.concatenate([np.array(dec.decode_frame(bb), float) for _ in range(NF)])))
def godraft(f):
    b = [int(c) for c in bits(f)]
    if os.environ.get("DRAFT"):   # FromDraftLayout: what a draft-layout decoder reads
        b = b[:40] + [b[41], b[42], b[43], b[40]] + b[44:]
    return b
go = [lev(x) for x in go_decode_runs([[godraft(f)] * NF for f in frames])]
md, go = np.array(md), np.array(go)
b = base[field]
mis = np.array([np.std((md[v] - md[b]) - (go[v] - go[b])) for v in range(size)])
direct = np.array([np.std(md[v] - go[v]) for v in range(size)])
print(f"b{field} sweep (base {base}, L={L}): mismatch of change from base: median {np.median(mis):.2f} dB, "
      f"90th pct {np.percentile(mis, 90):.2f}, max {mis.max():.2f};  direct MD-Go sd median {np.median(direct):.2f}")
order = np.argsort(-mis)
print("   largest mismatch: " + ", ".join(f"{v}:{mis[v]:.2f}" for v in order[:12]))
