#!/usr/bin/env python3
"""Long-term average spectra (dB) of input vs decoder outputs, in 250 Hz bands."""
import glob, os, sys
import numpy as np
sys.path.insert(0, 'tools')
from ambe_eval import read_pcm
SET = os.environ.get("SET", "testdata/matrix")
def ltas(x):
    n = 256; w = np.hanning(n); acc = np.zeros(n//2+1); k = 0
    for s in range(0, len(x)-n, n//2):
        seg = x[s:s+n]
        if np.mean(seg**2) < 1e4: continue   # speech-active only (RMS > 100)
        acc += np.abs(np.fft.rfft(seg*w))**2; k += 1
    return acc/max(k, 1)
bands = [(i*250, (i+1)*250) for i in range(16)]
f = np.fft.rfftfreq(256, 1/8000)
def band_db(P): return np.array([10*np.log10(P[(f >= a) & (f < b)].mean()+1e-9) for a, b in bands])
for enc in sys.argv[1:]:
    tot = {}
    for d in sorted(glob.glob(SET + "/*/")):
        for name in ("ref", f"{enc}.fw", f"{enc}.dsil"):
            p = d + ("ref.raw" if name == "ref" else name + ".raw")
            tot.setdefault(name, []).append(band_db(ltas(read_pcm(p))))
    ref = np.mean(tot["ref"], 0)
    print(f"== bitstream {enc}: LTAS relative to input (dB), 250 Hz bands 0..4 kHz")
    for name in (f"{enc}.fw", f"{enc}.dsil"):
        print(f"  {name:10s} " + " ".join(f"{x:+5.1f}" for x in np.mean(tot[name], 0) - ref))
