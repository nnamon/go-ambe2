#!/usr/bin/env python3
"""Average power envelope of one loud unvoiced frame between quiet ones:
the effective unvoiced synthesis window of the MD-380 decoder vs the Go one."""
import subprocess, sys, tempfile
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
SP = tempfile.mkdtemp(prefix='ambe-')
def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b
quiet = [60, 16, 0, 87, 78, 8, 14, 13, 1]
loud = [60, 16, 31, 87, 78, 8, 14, 13, 1]
pw = {"MD-380": np.zeros(160 * 6), "Go": np.zeros(160 * 6)}
runs = 40
for r in range(runs):
    lead = 12 + r                      # shifts the noise generators' state
    seq = [quiet] * lead + [loud] + [quiet] * 6
    v = Md380Vocoder()
    fw = np.concatenate([np.array(v.decode_frame(bits(f)), float) for f in seq])
    open(SP + '/u.bits', 'w').write('\n'.join(''.join(map(str, bits(f))) for f in seq) + '\n')
    subprocess.run(['bin/ambe-dec', SP + '/u.bits', SP + '/u.raw'], check=True, capture_output=True)
    go = np.fromfile(SP + '/u.raw', '<i2').astype(float)
    s = 160 * (lead - 1)               # from the start of the frame before the loud one
    pw["MD-380"] += fw[s:s + 960] ** 2 / runs
    pw["Go"] += go[s:s + 960] ** 2 / runs
for k, p in pw.items():
    e = np.sqrt(np.convolve(p, np.ones(20) / 20, "same"))
    e /= e.max()
    above = np.where(e > 0.5)[0]; above10 = np.where(e > 0.1)[0]
    print(f"{k:7s} power envelope (peak-normalised amplitude, 40-sample steps): " + " ".join(f"{x:.2f}" for x in e[::40]))
    print(f"        width above 50%: {above[-1]-above[0]} samples, above 10%: {above10[-1]-above10[0]} samples, peak at {int(np.argmax(e))}")
