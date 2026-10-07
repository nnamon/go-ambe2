#!/usr/bin/env python3
"""PESQ-NB / STOI / level of every <enc>.<dec>.raw against ref.raw, averaged
over the corpus set, as an encoder x decoder table.
Usage: score_matrix.py "enc1 enc2 ..." "dec1 dec2 ..."   (SET env: corpus set)"""
import glob, os, sys
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_pcm
from pesq import pesq
from pystoi import stoi

SET = os.environ.get("SET", "testdata/heldout")
encs, decs = sys.argv[1].split(), sys.argv[2].split()

def score(ref, deg):
    lag = estimate_delay(ref, deg)
    d = deg[lag:]; n = min(len(ref), len(d)); r, d = ref[:n], d[:n]
    try:
        pq = pesq(8000, r.astype(np.int16), np.clip(d, -32768, 32767).astype(np.int16), "nb")
    except Exception:
        pq = float("nan")
    return pq, stoi(r, d, 8000), np.sqrt(np.mean(d**2)) / max(1e-9, np.sqrt(np.mean(r**2)))

dirs = sorted(glob.glob(SET + "/*/"))
print(f"{SET} ({len(dirs)} files): PESQ-NB / STOI (level)")
print(f"{'encoder':12s} " + " ".join(f"{d:>22s}" for d in decs))
for e in encs:
    row = []
    for d in decs:
        v = [score(read_pcm(x + "ref.raw"), read_pcm(f"{x}{e}.{d}.raw")) for x in dirs if os.path.exists(f"{x}{e}.{d}.raw")]
        if len(v) != len(dirs):
            row.append(f"{'(missing)':>22s}"); continue
        v = np.array(v)
        row.append(f"{v[:,0].mean():.3f} / {v[:,1].mean():.3f} ({v[:,2].mean():.2f})".rjust(22))
    print(f"{e:12s} " + " ".join(row))
