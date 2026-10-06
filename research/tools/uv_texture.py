#!/usr/bin/env python3
"""Texture of unvoiced stretches: within runs of >=3 unvoiced frames,
short-time (8 ms, 4 ms hop) log energy in 4 bands; reports the temporal
fluctuation (sd of the first difference) and the sd around a 20 ms moving
average, for the input and each decoder.  Usage: uv_texture.py enc dec..."""
import glob, os, sys
import numpy as np
from scipy.signal import stft
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_frames, read_pcm, to_fields
SET = os.environ.get("SET", "testdata/heldout")
enc, decs = sys.argv[1], sys.argv[2:]
encdelay = int(os.environ["ENCDELAY"]) if "ENCDELAY" in os.environ else {"fw": 2, "go": 3, "la1": 2, "op25": 3, "op25d": 3}[enc]  # frames of encoder delay
bands = [(500, 1500), (1500, 2500), (2500, 3200), (3200, 4000)]
acc = {k: [] for k in ["input"] + decs}
for dd in sorted(glob.glob(SET + "/*/")):
    ref = read_pcm(dd + "ref.raw"); sig = {"input": ref}
    for d in decs:
        o = read_pcm(f"{dd}{enc}.{d}.raw"); sig[d] = o[estimate_delay(ref, o):]
    F = [to_fields(b) for b in read_frames(f"{dd}{enc}.amb")]
    uv = [F[k + encdelay][0] < 120 and (F[k + encdelay][1] == 16 or F[k + encdelay][1] >= 18) for k in range(len(F) - encdelay)]
    runs, k = [], 0
    while k < len(uv):
        if uv[k]:
            j = k
            while j < len(uv) and uv[j]: j += 1
            if j - k >= 3: runs.append((k, j))
            k = j
        else: k += 1
    for name, x in sig.items():
        f, t, Z = stft(x, 8000, nperseg=64, noverlap=32)
        P = np.abs(Z) ** 2
        E = np.array([10*np.log10(P[(f >= a) & (f < b)].sum(0) + 1) for a, b in bands])  # bands x time (4 ms hop)
        for a, b in runs:
            i0, i1 = int(160 * a / 32) + 1, int(160 * b / 32) - 1
            if i1 - i0 < 10: continue
            seg = E[:, i0:i1]
            d1 = np.diff(seg, axis=1).std(1)
            ma = np.array([np.convolve(r, np.ones(5) / 5, "same") for r in seg])
            acc[name].append(np.r_[d1, (seg - ma)[:, 2:-2].std(1)])
print(f"{enc} streams, unvoiced runs: band log-energy fluctuation (dB), bands {bands}")
print("                 4-ms step sd per band              |  sd around 20-ms average per band")
for k, v in acc.items():
    v = np.array(v).mean(0)
    print(f"   {k:8s} " + " ".join(f"{x:5.2f}" for x in v[:4]) + "   |  " + " ".join(f"{x:5.2f}" for x in v[4:]))
