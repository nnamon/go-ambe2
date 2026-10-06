#!/usr/bin/env python3
"""Summarise PESQ-NB / STOI / level for encoder tags across the corpus.
  score.py tag [tag...]      (tags: fw, op25, go, ...; decoders: fw and mbelib)"""
import glob
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_frames, read_pcm, to_fields  # noqa: E402
from pesq import pesq  # noqa: E402
from pystoi import stoi  # noqa: E402


def quality(ref, deg):
    lag = estimate_delay(ref, deg)
    d = deg[lag:]
    n = min(len(ref), len(d))
    r, d = ref[:n], d[:n]
    try:
        pq = pesq(8000, r.astype(np.int16), np.clip(d, -32768, 32767).astype(np.int16), "nb")
    except Exception:  # noqa: BLE001
        pq = float("nan")
    st = stoi(r, d, 8000, extended=False)
    lvl = np.sqrt(np.mean(d ** 2)) / max(1e-9, np.sqrt(np.mean(r ** 2)))
    return pq, st, lvl, lag


dirs = sorted(glob.glob(os.environ.get("SET", "testdata/matrix") + "/*/"))
for tag in sys.argv[1:]:
    print(f"== encoder: {tag}")
    rows = {}
    for d in dirs:
        name = os.path.basename(d.rstrip("/"))
        ref = read_pcm(d + "ref.raw")
        line = []
        for dec in ("fw", "mbelib"):
            p = f"{d}{tag}.{dec}.raw"
            if not os.path.exists(p):
                line.append(None)
                continue
            line.append(quality(ref, read_pcm(p)))
        fa = np.array([to_fields(b) for b in read_frames(d + "fw.amb")])
        fb = np.array([to_fields(b) for b in read_frames(d + f"{tag}.amb")])
        n = min(len(fa), len(fb))
        sil_a, sil_b = fa[:n, 0] >= 124, fb[:n, 0] >= 124
        rows[name] = (line, (sil_a.mean(), sil_b.mean()))
        fwq, mbq = line
        print(f"  {name:22s} fw-dec PESQ {fwq[0]:.3f} STOI {fwq[1]:.3f} lvl {fwq[2]:.2f} delay {fwq[3]/8:.1f}ms"
              f" | mbelib-dec PESQ {mbq[0]:.3f} STOI {mbq[1]:.3f} lvl {mbq[2]:.2f}"
              f" | silence frac fw {sil_a.mean():.2f} {tag} {sil_b.mean():.2f}")
    fw = np.array([r[0][0][:3] for r in rows.values()])
    mb = np.array([r[0][1][:3] for r in rows.values()])
    print(f"  MEAN                   fw-dec PESQ {fw[:,0].mean():.3f} STOI {fw[:,1].mean():.3f} lvl {fw[:,2].mean():.2f}"
          f"     | mbelib-dec PESQ {mb[:,0].mean():.3f} STOI {mb[:,1].mean():.3f} lvl {mb[:,2].mean():.2f}")
