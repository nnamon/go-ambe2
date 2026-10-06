#!/usr/bin/env python3
"""Which harmonic->voicing-band mapping explains the MD-380 probe data?
Predict per-500Hz-band harmonic fractions for canonical codewords under
j_l = floor(16 l f0 + off) for several offsets and compare with measurements."""
import json, sys
import numpy as np
sys.path.insert(0, 'tools')
import mbelib_tables
T = mbelib_tables.load()
probe = json.load(open('testdata/vuv_probe.json'))
canon = [0, 2, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16]
for off in (0.0, 0.125, 0.25, 0.375, 0.5):
    errs = []
    for key, d in probe.items():
        b0 = d['b0']; f0 = T['AmbeW0table'][b0]; L = int(T['AmbeLtable'][b0])
        h0 = np.median(np.array(d['harmfrac'][16]))  # noise baseline
        for b1 in canon:
            vec = T['AmbeVuv'][b1]
            pred = np.zeros(8); cnt = np.zeros(8)
            for l in range(1, L+1):
                fz = l*f0*8000
                band_meas = min(int(fz // 500), 7)
                j = min(int(l*16*f0 + off), 7)
                pred[band_meas] += vec[j]; cnt[band_meas] += 1
            m = cnt > 0
            p = pred[m]/cnt[m]
            meas = (np.array(d['harmfrac'][b1])[m] - h0)/(1 - h0)
            errs.append(np.abs(p - np.clip(meas, 0, 1)))
    e = np.concatenate(errs)
    print(f"offset {off:5.3f}: mean |predicted - measured voiced fraction| = {e.mean():.3f}  (fraction of bands off by >0.2: {np.mean(e > 0.2):.3f})")
