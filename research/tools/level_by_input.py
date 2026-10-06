#!/usr/bin/env python3
"""Per-frame output level relative to the input, by input level, for one
frame class, for several decoders on the same bitstreams.
Usage: level_by_input.py enc dec1 dec2 ...   (CLASS=unvoiced|mixed|voiced|silence)"""
import glob, os, sys
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_frames, read_pcm, to_fields
SET = os.environ.get("SET", "testdata/heldout"); CLASS = os.environ.get("CLASS", "unvoiced")
enc, decs = sys.argv[1], sys.argv[2:]
encdelay = int(os.environ["ENCDELAY"]) if "ENCDELAY" in os.environ else {"fw": 2, "go": 3, "la1": 2, "op25": 3, "op25d": 3}[enc]  # frames of encoder delay
def cls(f):
    if f[0] >= 120: return "silence"
    if f[1] == 0: return "voiced"
    if f[1] == 16 or f[1] >= 18: return "unvoiced"
    return "mixed"
rows = []
for dd in sorted(glob.glob(SET + "/*/")):
    ref = read_pcm(dd + "ref.raw"); outs = []
    for d in decs:
        o = read_pcm(f"{dd}{enc}.{d}.raw"); outs.append(o[estimate_delay(ref, o):])
    F = [to_fields(b) for b in read_frames(f"{dd}{enc}.amb")]
    for k in range(len(F) - encdelay):
        if cls(F[k + encdelay]) != CLASS: continue
        s = 160 * k
        if s + 160 > min(len(ref), *map(len, outs)): continue
        pin = np.mean(ref[s:s+160]**2) + 1
        rows.append([10*np.log10(pin)] + [10*np.log10(np.mean(o[s:s+160]**2) + 1) for o in outs])
R = np.array(rows)
edges = np.percentile(R[:, 0], [0, 25, 50, 75, 100])
print(f"{enc} streams, {CLASS} frames ({len(R)}): output level (dB) by input-level quartile")
print("   input level range (dB)   " + "  ".join(f"{d:>9s}" for d in decs) + "   (input)")
for a, b in zip(edges[:-1], edges[1:]):
    m = (R[:, 0] >= a) & (R[:, 0] <= b)
    print(f"   {a:5.1f} .. {b:5.1f}         " + "  ".join(f"{R[m, i+1].mean():9.1f}" for i in range(len(decs))) + f"   {R[m, 0].mean():6.1f}")
