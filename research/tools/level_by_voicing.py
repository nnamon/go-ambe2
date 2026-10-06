#!/usr/bin/env python3
"""Output/input level ratio of <enc>.<dec>.raw split by the firmware encoder's
voicing of each frame. Usage: level_by_voicing.py tag [tag...]"""
import glob, os, subprocess, sys
import numpy as np
sys.path.insert(0, 'tools')
from ambe_eval import estimate_delay, read_pcm
SET = os.environ.get("SET", "testdata/matrix")
for tag in sys.argv[1:]:
    acc = {"voiced": [], "mixed": [], "unvoiced": [], "silence": []}
    for d in sorted(glob.glob(SET + "/*/")):
        ref = read_pcm(d + "ref.raw"); out = read_pcm(d + f"{tag}.raw")
        lag = estimate_delay(ref, out)
        out = out[lag:]
        # classify input frames by the firmware encoder's decisions (fw.amb lags input by 2 frames)
        lines = subprocess.run(["bin/ambe-params", d + "fw.amb"], capture_output=True, text=True).stdout.splitlines()
        for k, line in enumerate(lines[2:]):
            t = line.split()
            if t[1] == "S":
                cls = "silence"
            else:
                vf = np.mean([c == "1" for c in t[14]])
                cls = "voiced" if vf >= 0.75 else "unvoiced" if vf == 0 else "mixed"
            a, b = ref[160 * k:160 * k + 160], out[160 * k:160 * k + 160]
            if len(b) < 160:
                break
            ea, eb = np.sum(a * a), np.sum(b * b)
            acc[cls].append((ea, eb))
    print(f"== {tag}")
    for cls, v in acc.items():
        v = np.array(v)
        if len(v):
            print(f"   {cls:9s} frames {len(v):6d}  level ratio (energy-weighted) {np.sqrt(v[:,1].sum()/v[:,0].sum()):.3f}")
