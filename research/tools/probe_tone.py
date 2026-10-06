#!/usr/bin/env python3
"""Black-box: MD-380 decoder output for tone frames (TIA-102.BABA-1 Table 10)."""
import sys
import numpy as np
sys.path.insert(0, 'oracle/unicorn')
from md380_uc import Md380Vocoder

def tone_bits(ID, AD):
    u0 = (63 << 6) | (AD >> 1)
    u1 = (ID << 4) | (ID >> 4)
    u2 = ((ID & 0xF) << 7) | (ID >> 1)
    u3 = ((ID & 1) << 13) | (ID << 5) | ((AD & 1) << 4)
    bits = []
    for v, n in ((u0, 12), (u1, 12), (u2, 11), (u3, 14)):
        bits += [(v >> (n - 1 - i)) & 1 for i in range(n)]
    return bits

for ID, AD, freqs in ((32, 127, [1000]), (32, 100, [1000]), (32, 80, [1000]), (128, 127, [1336, 941]), (128, 100, [1336, 941]), (255, 100, [])):
    v = Md380Vocoder()
    pcm = np.concatenate([np.array(v.decode_frame(tone_bits(ID, AD)), float) for _ in range(25)])
    seg = pcm[160*10:160*20]; t = np.arange(len(seg))
    out = []
    for f in freqs:
        c = np.exp(-2j*np.pi*f*t/8000)
        out.append(2*abs(np.dot(seg, c))/len(seg))
    rms = np.sqrt(np.mean(seg**2))
    pred = 32767 * 10**(0.711*(AD-127)/20)
    print(f"ID={ID:3d} AD={AD:3d}: rms {rms:8.1f} peak {np.max(np.abs(seg)):7.0f}  component amplitudes {[round(x) for x in out]}  (full-scale formula A={pred:.0f})")
