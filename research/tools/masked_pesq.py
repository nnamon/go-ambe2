#!/usr/bin/env python3
"""Attribute a PESQ gap to frame classes by masking: for each class, keep only
that class's stretches (10 ms ramps) of the input and of each decoder's own
time-aligned output, silence the rest, and score.  No mixing of decoders.
Usage: masked_pesq.py enc dec1 dec2 ..."""
import glob, math, os, sys
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_frames, read_pcm, to_fields
from pesq import pesq
import mbelib_tables
W0 = mbelib_tables.load()["AmbeW0table"]
SET = os.environ.get("SET", "testdata/heldout")
enc, decs = sys.argv[1], sys.argv[2:]
encdelay = int(os.environ["ENCDELAY"]) if "ENCDELAY" in os.environ else {"fw": 2, "go": 3, "la1": 2, "op25": 3, "op25d": 3}[enc]  # frames of encoder delay
MIXED = {2, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 1, 3}
def classify(F, k):
    f = F[k]
    if f[0] >= 120: return "silence"
    g = F[k - 1] if k else f
    steady = g[0] < 120 and abs(math.log2(W0[f[0]] / W0[g[0]])) < 0.03
    if f[1] == 0: return "all-voiced, steady" if steady else "all-voiced, changing"
    if f[1] in MIXED: return "mixed voicing"
    if os.environ.get("SPLITUV"): return "unvoiced b1=16" if f[1] == 16 else "unvoiced b1>=18"
    return "unvoiced"
classes = ["all-voiced, steady", "all-voiced, changing", "mixed voicing", "unvoiced", "silence"]
if os.environ.get("SPLITUV"): classes[3:4] = ["unvoiced b1=16", "unvoiced b1>=18"]
res = {c: {d: [] for d in decs} for c in classes + ["(all)"]}
cover = {c: [] for c in classes}
ramp = np.hanning(160)[:80]
for dd in sorted(glob.glob(SET + "/*/")):
    ref = read_pcm(dd + "ref.raw")
    outs = {}
    for d in decs:
        o = read_pcm(f"{dd}{enc}.{d}.raw"); o = o[estimate_delay(ref, o):]
        outs[d] = np.concatenate([o, np.zeros(max(0, len(ref) - len(o)))])[:len(ref)]
    F = [to_fields(b) for b in read_frames(f"{dd}{enc}.amb")]
    n = len(ref) // 160
    lab = [classify(F, min(k + encdelay, len(F) - 1)) for k in range(n)]
    for c in classes + ["(all)"]:
        m = np.zeros(len(ref))
        for k in range(n):
            if c == "(all)" or lab[k] == c:
                m[160 * k:160 * k + 160] = 1
        if m.sum() < 8000 * 1.0:   # need at least ~1 s for a meaningful PESQ
            continue
        # soften edges
        m = np.convolve(m, np.ones(80) / 80, "same")
        if c != "(all)": cover[c].append(m.mean())
        r = ref * m
        try:
            v = [pesq(8000, r.astype(np.int16), np.clip(outs[d] * m, -32768, 32767).astype(np.int16), "nb") for d in decs]
        except Exception:
            continue
        for d, x in zip(decs, v):
            res[c][d].append(x)
print(f"{enc} bitstreams ({SET}): PESQ-NB on each frame class alone")
print("   class                   share   " + "  ".join(f"{d:>8s}" for d in decs) + "   gap")
for c in ["(all)"] + classes:
    if not res[c][decs[0]]: continue
    v = [np.mean(res[c][d]) for d in decs]
    share = "" if c == "(all)" else f"{100*np.mean(cover[c]):4.0f}%"
    print(f"   {c:22s} {share:>6s}   " + "  ".join(f"{x:8.3f}" for x in v) + f"   {v[1]-v[0]:+.3f}")
