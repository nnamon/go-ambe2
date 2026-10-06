#!/usr/bin/env python3
"""Black-box map of the MD-380 decoder's 49-bit frame layout.  For each frame
bit position p, flip p in a steady (repeated) base frame decoded by the
MD-380, and find the position q whose flip changes the Go decoder's output
spectrum (Welch log-PSD) most similarly.  With identical layouts q = p.
DRAFT=1 gives the Go decoder frames in the TIA-102.BABA-1 draft's Table 8
layout (mbelib, OP25) instead, i.e. it maps the MD-380's layout onto that one.
Usage: probe_bit_map.py   (BASES env: ';'-separated b0..b8)"""
import os, sys
import numpy as np
from scipy.signal import welch
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT, go_decode_runs
NF = 30
def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b
def psd(x):
    fq, p = welch(x[160*(NF-12):], 8000, nperseg=512)
    sel = (fq > 100) & (fq < 3800)
    return 10*np.log10(p[sel] + 1e-3)
def flip(b, p):
    c = list(b); c[p] ^= 1; return c
bases = [[int(x) for x in s.split(",")] for s in os.environ.get("BASES", "60,0,16,87,78,8,14,13,1;30,5,20,300,40,20,3,9,6;90,0,12,150,100,3,10,2,4").split(";")]
votes = {}
for base in bases:
    b = bits(base)
    md = []
    variants = [b] + [flip(b, p) for p in range(49)]
    for v in variants:
        dec = Md380Vocoder()
        md.append(psd(np.concatenate([np.array(dec.decode_frame(v), float) for _ in range(NF)])))
    if os.environ.get("DRAFT"):
        # a draft-layout decoder: the Go decoder after FromDraftLayout
        variants = [v[:40] + [v[41], v[42], v[43], v[40]] + v[44:] for v in variants]
    go = [psd(x) for x in go_decode_runs([[v] * NF for v in variants])]
    dmd = [m - md[0] for m in md[1:]]; dgo = [g - go[0] for g in go[1:]]
    for p in range(49):
        dist = [np.std(dmd[p] - dgo[q]) for q in range(49)]
        q = int(np.argmin(dist)); eff = np.std(dmd[p])
        votes.setdefault(p, []).append((q, round(dist[q], 2), round(dist[p], 2), round(eff, 2)))
print(f"MD-380 position p -> best-matching position q in the {'draft Table 8' if os.environ.get('DRAFT') else 'Go'} layout  [per base: q (dist to q, dist to p, size of effect) dB]")
for p in range(49):
    qs = [v[0] for v in votes[p]]
    flag = "" if all(q == p for q in qs) else "   <-- differs" if all(q == qs[0] for q in qs) else "   <-- inconsistent"
    print(f"  {p:2d} {str(LAYOUT[p]):8s} -> {votes[p]}{flag}")
# Summary: a position matches if, in a majority of bases, q = p or flipping p
# itself is within 0.5 dB of the best match (near-ties occur for small effects).
ok = [p for p in range(49) if sum(v[0] == p or v[2] - v[1] <= 0.5 for v in votes[p]) * 2 > len(bases)]
moved, unclear = {}, []
for p in range(49):
    if p in ok: continue
    qs = [v[0] for v in votes[p]]; q = max(set(qs), key=qs.count)
    if qs.count(q) * 2 > len(bases): moved[p] = q
    else: unclear.append(p)
print(f"{len(ok)}/49 positions match the same position; moved (MD-380 position -> match): {moved}; "
      f"no majority (small, level-only effects): {unclear}")
