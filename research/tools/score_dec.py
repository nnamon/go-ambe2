#!/usr/bin/env python3
"""Compare decoders on the corpus.  score_dec.py dectag enc...
For each encoder bitstream: PESQ-NB/STOI/level of the firmware decoder, mbelib and
the Go decoder (dectag) against the input, plus PESQ of the Go decoder against the
firmware decoder's output (how closely it reproduces the MD-380 decoder)."""
import glob, os, sys
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_pcm
from pesq import pesq
from pystoi import stoi

def q(ref, deg):
    lag = estimate_delay(ref, deg)
    d = deg[lag:]; n = min(len(ref), len(d)); r, d = ref[:n], d[:n]
    try:
        pq = pesq(8000, r.astype(np.int16), np.clip(d, -32768, 32767).astype(np.int16), "nb")
    except Exception:
        pq = float("nan")
    return pq, stoi(r, d, 8000), np.sqrt(np.mean(d**2)) / max(1e-9, np.sqrt(np.mean(r**2))), lag

def align_pesq(ref, deg):
    # both are decoder outputs: find lag in [-400, 400] by envelope xcorr either way
    l1 = estimate_delay(ref, deg); l2 = estimate_delay(deg, ref)
    if l1 >= l2:
        d, r = deg[l1:], ref
    else:
        d, r = deg, ref[l2:]
    n = min(len(r), len(d))
    try:
        return pesq(8000, r[:n].astype(np.int16), np.clip(d[:n], -32768, 32767).astype(np.int16), "nb")
    except Exception:
        return float("nan")

dectag = sys.argv[1]
SET = os.environ.get("SET", "testdata/matrix")
dirs = sorted(glob.glob(SET + "/*/"))
for enc in sys.argv[2:]:
    acc = {k: [] for k in ("fw", "mbelib", dectag)}; sim = []
    for d in dirs:
        ref = read_pcm(d + "ref.raw")
        for dec in acc:
            p = f"{d}{enc}.{dec}.raw"
            if os.path.exists(p):
                acc[dec].append(q(ref, read_pcm(p))[:3])
        a, b = f"{d}{enc}.fw.raw", f"{d}{enc}.{dectag}.raw"
        if os.path.exists(a) and os.path.exists(b):
            sim.append(align_pesq(read_pcm(a), read_pcm(b)))
    print(f"== bitstream from encoder '{enc}' ({len(dirs)} files)")
    for dec, v in acc.items():
        if v:
            v = np.array(v)
            print(f"   decoder {dec:8s} PESQ {v[:,0].mean():.3f}  STOI {v[:,1].mean():.3f}  level {v[:,2].mean():.2f}")
    if sim:
        print(f"   PESQ of {dectag} output vs MD-380 decoder output: {np.nanmean(sim):.3f}")
