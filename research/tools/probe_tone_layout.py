#!/usr/bin/env python3
"""Is the MD-380 vocoder's 49-bit buffer in the TIA-102.BABA-1 bit-vector
order (u0..u3 concatenated)?  Tone frames say so: TIA-102.BABA-1 Table 10
repeats the 8-bit tone index four times, the fourth copy in u3 bits 12..5
(frame positions 36..43).  The MD-380 encoder's tone frames are checked for
four agreeing copies at exactly those positions; a buffer-wide reordering of
positions 40..43 (the b3/b4 placement difference, tools/probe_bit_map.py)
would break the fourth copy for most tone indices."""
import sys
import numpy as np
sys.path.insert(0, 'oracle/unicorn')
from md380_uc import Md380Vocoder
fs = 8000
rows, cols = [697, 770, 852, 941], [1209, 1336, 1477, 1633]
tests = [(f"DTMF {r}/{c} Hz", (r, c)) for r in rows for c in cols] + [(f"{f} Hz", (f, None)) for f in (300, 450, 700, 1500, 2100, 2800)]
agree = broken = 0
for name, (f1, f2) in tests:
    t = np.arange(fs // 2) / fs
    x = 6000 * np.sin(2*np.pi*f1*t) + (6000 * np.sin(2*np.pi*f2*t) if f2 else 0)
    pcm = np.round(x).astype(int).tolist(); v = Md380Vocoder(); b = None
    for k in range(len(pcm) // 160):
        b = v.encode_frame(pcm[160*k:160*k+160])
        if b[:6] == [1]*6: break
    else:
        print(f"{name:20s} no tone frame"); continue
    u1, u2, u3 = b[12:24], b[24:35], b[35:49]
    num = lambda bits: int(''.join(map(str, bits)), 2)
    ids = [num(u1[:8]), num(u1[8:] + u2[:4]), num(u2[4:] + [u3[0]]), num(u3[1:9])]
    rot = num(u3[1:5] + [u3[8], u3[5], u3[6], u3[7]])   # the 4th copy if 40..43 were reordered
    agree += len(set(ids)) == 1; broken += rot != ids[0]
    print(f"{name:20s} tone index copies {ids}; 4th copy under a 40..43 reordering: {rot}")
print(f"{agree}/{len(tests)} tone frames have four agreeing copies at the Table 10 positions; "
      f"a reordering of 40..43 would have broken {broken} of them")
