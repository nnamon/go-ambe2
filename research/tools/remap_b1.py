#!/usr/bin/env python3
"""Rewrite fw.amb with non-canonical V/UV codewords mapped to canonical ones
(1,3->0; 13,15->10; 17..31->16), writing fwcanon.amb in each corpus dir."""
import glob, os, sys
sys.path.insert(0, 'tools')
from ambe_eval import read_frames, to_fields, LAYOUT
remap = {1: 0, 3: 0, 13: 10, 15: 10, **{k: 16 for k in range(17, 32)}}
SET = os.environ.get("SET", "testdata/matrix")
n = c = 0
for d in sorted(glob.glob(SET + "/*/")):
    out = bytearray(b".amb")
    for b in read_frames(d + "fw.amb"):
        f = to_fields(b)
        if f[0] < 120 and f[1] in remap:
            f[1] = remap[f[1]]; c += 1
        n += 1
        bits = [0]*49
        for pos, (fi, sig) in LAYOUT.items(): bits[pos] = (f[fi] >> sig) & 1
        p = bytearray(8)
        for i in range(48): p[1 + i//8] |= bits[i] << (7 - i % 8)
        p[7] = bits[48]; out += p
    open(d + "fwcanon.amb", "wb").write(out)
print(f"remapped {c} of {n} frames")
