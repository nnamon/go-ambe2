#!/usr/bin/env python3
"""Per-frame log-spectral distance between two decoders' outputs of the same
bitstream, grouped by b1 / frame type.  Usage: dec_divergence.py enc decA decB"""
import glob, os, sys
from collections import defaultdict
import numpy as np
sys.path.insert(0, 'tools')
from ambe_eval import read_frames, to_fields, read_pcm, estimate_delay
enc, A, B = sys.argv[1:4]
SET = os.environ.get("SET", "testdata/matrix")
acc = defaultdict(list); lev = defaultdict(list)
win = np.hanning(160)
def spec(x):
    X = np.abs(np.fft.rfft(x * win, 256))**2
    # smooth over 5 bins to compare envelopes, not fine structure
    Xs = np.convolve(X, np.ones(5)/5, 'same')
    return 10*np.log10(Xs + 1e-3)
for d in sorted(glob.glob(SET + "/*/")):
    a = read_pcm(f"{d}{enc}.{A}.raw"); b = read_pcm(f"{d}{enc}.{B}.raw")
    l1 = estimate_delay(a, b); l2 = estimate_delay(b, a)
    if l1 >= l2: b = b[l1:]
    else: a = a[l2:]
    F = [to_fields(x) for x in read_frames(f"{d}{enc}.amb")]
    for k, f in enumerate(F):
        s = 160*k
        if s + 160 > min(len(a), len(b)): break
        xa, xb = a[s:s+160], b[s:s+160]
        ea, eb = np.mean(xa**2), np.mean(xb**2)
        if ea < 100 and eb < 100: continue
        key = "silence" if f[0] >= 124 else f"b1={f[1]}"
        sa, sb = spec(xa), spec(xb)
        acc[key].append(np.sqrt(np.mean((sa - sb)**2)))
        lev[key].append(10*np.log10((eb+1)/(ea+1)))
tot = sum(len(v) for v in acc.values())
print(f"{enc} bitstreams: {A} vs {B}; per-frame smoothed log-spectral distance (dB) and level diff ({B}-{A}, dB)")
for k in sorted(acc, key=lambda k: -len(acc[k]) * np.mean(acc[k])):
    v = acc[k]
    print(f"  {k:8s} frames {len(v):5d} ({100*len(v)/tot:4.1f}%)  LSD {np.mean(v):5.2f}  level {np.mean(lev[k]):+5.2f}  share of total LSD {100*np.sum(v)/sum(np.sum(x) for x in acc.values()):4.1f}%")
