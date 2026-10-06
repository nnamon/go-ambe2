#!/usr/bin/env python3
"""Black-box: how does the MD-380 decoder's level evolve over a run of silence
frames?  Compares with the Go decoder (spec behaviour)."""
import subprocess, sys
import tempfile
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
SP = tempfile.mkdtemp(prefix='ambe-')
def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b
for vb2, sb2 in ((22, 6), (22, 12), (10, 6), (22, 20)):
    V = [60, 0, vb2, 87, 78, 8, 14, 13, 1]
    S = [124, 16, sb2, 87, 78, 8, 14, 13, 1]
    seq = [V]*12 + [S]*12 + [V]*4
    v = Md380Vocoder()
    fw = np.concatenate([np.array(v.decode_frame(bits(f)), float) for f in seq])
    open(SP + '/s.bits', 'w').write('\n'.join(''.join(map(str, bits(f))) for f in seq) + '\n')
    subprocess.run(['bin/ambe-dec', SP + '/s.bits', SP + '/s.raw'], capture_output=True)
    go = np.fromfile(SP + '/s.raw', '<i2').astype(float)
    r = lambda x, k: np.sqrt(np.mean(x[160*k:160*k+160]**2))
    print(f"voice b2={vb2}, silence b2={sb2}")
    print("  fw: " + " ".join(f"{r(fw,k):6.0f}" for k in range(10, 28)))
    print("  go: " + " ".join(f"{r(go,k):6.0f}" for k in range(10, 28)))
