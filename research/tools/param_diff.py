#!/usr/bin/env python3
"""Compare decoded parameters of two encoders' bitstreams (default fw vs go) per frame.
  param_diff.py [tagA] [tagB]   (SET env selects corpus dir)"""
import glob, os, subprocess, sys
import numpy as np

A = sys.argv[1] if len(sys.argv) > 1 else "fw"
B = sys.argv[2] if len(sys.argv) > 2 else "go"
SET = os.environ.get("SET", "testdata/matrix")


def params(path):
    out = []
    for line in subprocess.run(["bin/mbevoc-params", path], capture_output=True, text=True, check=True).stdout.splitlines():
        t = line.split()
        if t[1] != "V":
            out.append(None if t[1] != "S" else ("S", float(t[11]) if len(t) > 11 else 0, 0, 0, "", []))
            continue
        f0, L, g, vl = float(t[11]), int(t[12]), float(t[13]), t[14]
        out.append(("V", f0, L, g, vl, np.array(list(map(float, t[15:15 + L])))))
    return out


def band_voicing(p):
    f0, L, vl = p[1], p[2], p[4]
    v = np.full(8, np.nan)
    for j in range(8):
        hs = [l for l in range(1, L + 1) if min(7, int(l * f0 / 500)) == j]
        if hs:
            v[j] = np.mean([vl[l - 1] == "1" for l in hs])
    return v


def env(p, grid):
    f0, L, M = p[1], p[2], p[5]
    return np.interp(grid, f0 * np.arange(1, L + 1), M)


grid = np.linspace(200, 3400, 33)
tot = dict(n=0, pitch_ok=0, oct_up=0, oct_dn=0, other=0, vagree=[], gdiff=[], envd=[], both_voice=0, sil_mismatch=0, frames=0)
for d in sorted(glob.glob(SET + "/*/")):
    pa, pb = params(d + A + ".amb"), params(d + B + ".amb")
    # Choose frame offset maximising f0 agreement.
    best = None
    for off in range(-4, 5):
        hits = 0
        for k in range(len(pa)):
            j = k + off
            if 0 <= j < len(pb) and pa[k] and pb[j] and pa[k][0] == pb[j][0] == "V":
                hits += abs(np.log2(pb[j][1] / pa[k][1])) < 0.05
        if best is None or hits > best[0]:
            best = (hits, off)
    off = best[1]
    for k in range(len(pa)):
        j = k + off
        if not (0 <= j < len(pb)):
            continue
        a, b = pa[k], pb[j]
        tot["frames"] += 1
        if (a is not None and a[0] == "S") != (b is not None and b[0] == "S"):
            tot["sil_mismatch"] += 1
        if not (a and b and a[0] == b[0] == "V"):
            continue
        tot["both_voice"] += 1
        va0 = band_voicing(a)
        strong = np.nanmean(va0) >= 0.5
        r = np.log2(b[1] / a[1])
        if strong:
            tot.setdefault("strong", []).append(r)
            tot.setdefault("strong_g", []).append(b[3] - a[3])
        else:
            tot.setdefault("weak_g", []).append(b[3] - a[3])
        if abs(r) < 0.05:
            tot["pitch_ok"] += 1
        elif abs(r - 1) < 0.1:
            tot["oct_up"] += 1
        elif abs(r + 1) < 0.1:
            tot["oct_dn"] += 1
        else:
            tot["other"] += 1
        va, vb = band_voicing(a), band_voicing(b)
        m = ~np.isnan(va) & ~np.isnan(vb)
        tot["vagree"].append(np.mean((va[m] > 0.5) == (vb[m] > 0.5)))
        tot["gdiff"].append(b[3] - a[3])
        tot["envd"].append(np.sqrt(np.mean((env(b, grid) - env(a, grid) - np.mean(env(b, grid) - env(a, grid))) ** 2)))
    print(f"{os.path.basename(d.rstrip('/')):22s} offset {off:+d} frames")
n = tot["both_voice"]
print(f"\n{A} vs {B}: {tot['frames']} aligned frames, silence-decision mismatch {tot['sil_mismatch']/tot['frames']:.3f}")
print(f"both voice: {n}  pitch within 3.5%: {tot['pitch_ok']/n:.3f}  {B} octave up: {tot['oct_up']/n:.3f}  octave down: {tot['oct_dn']/n:.3f}  other: {tot['other']/n:.3f}")
print(f"band voicing agreement: {np.mean(tot['vagree']):.3f}")
st = np.array(tot["strong"])
print(f"strongly voiced ({A} >=50% bands voiced): {len(st)} frames; pitch within 3.5%: {np.mean(np.abs(st) < 0.05):.3f}, "
      f"octave up {np.mean(np.abs(st - 1) < 0.1):.3f}, octave down {np.mean(np.abs(st + 1) < 0.1):.3f}, "
      f"x1.5/x0.67 {np.mean((np.abs(st - 0.585) < 0.07) | (np.abs(st + 0.585) < 0.07)):.3f}, median |log2 ratio| {np.median(np.abs(st)):.4f}")
print(f"gamma diff strongly voiced: {np.mean(tot['strong_g']):+.3f}  weakly/unvoiced: {np.mean(tot['weak_g']):+.3f}")
g = np.array(tot["gdiff"])
print(f"gamma({B}) - gamma({A}): mean {g.mean():+.3f} std {g.std():.3f}  (log2 units)")
print(f"spectral envelope shape RMS diff (log2, mean removed): {np.mean(tot['envd']):.3f}")
