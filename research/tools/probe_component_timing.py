#!/usr/bin/env python3
"""Timing of the voiced and unvoiced components in each decoder: one loud
frame (b2=31) between quiet frames (b2=0), all-voiced or all-unvoiced,
averaged over several noise phases.  Prints the centroid and peak of the
power envelope, relative to the start of the loud frame's output frame."""
import os, subprocess, sys, tempfile
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
SP = tempfile.mkdtemp(prefix='ambe-')
def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b
for label, b1 in (("all voiced", 0), ("all unvoiced", 16), ("mixed b1=8", 8)):
    quiet = [60, b1, 0, 87, 78, 8, 14, 13, 1]; loud = [60, b1, 31, 87, 78, 8, 14, 13, 1]
    pw = {"MD-380": np.zeros(960), "Go": np.zeros(960)}
    runs = 30
    for r in range(runs):
        lead = 12 + r
        seq = [quiet] * lead + [loud] + [quiet] * 6
        v = Md380Vocoder()
        fw = np.concatenate([np.array(v.decode_frame(bits(f)), float) for f in seq])
        open(SP + '/u.bits', 'w').write('\n'.join(''.join(map(str, bits(f))) for f in seq) + '\n')
        subprocess.run(['bin/ambe-dec', SP + '/u.bits', SP + '/u.raw'], check=True, capture_output=True)
        go = np.fromfile(SP + '/u.raw', '<i2').astype(float)
        s = 160 * (lead - 1)
        pw["MD-380"] += fw[s:s + 960] ** 2 / runs; pw["Go"] += go[s:s + 960] ** 2 / runs
    for k, p in pw.items():
        p = p - np.median(p)   # remove the quiet floor
        p = np.clip(p, 0, None)
        e = np.convolve(p, np.ones(20) / 20, "same")
        t = np.arange(960) - 160
        c = np.sum(t * e) / np.sum(e)
        print(f"{label:13s} {k:7s} centroid {c:6.1f}  peak {t[np.argmax(e)]:4d}  (samples from the start of the loud frame's output frame)")
