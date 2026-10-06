#!/usr/bin/env python3
import json
import numpy as np
cases = json.load(open('testdata/phase_probe.json'))
def enhance(w0, M):
    L = len(M); l = np.arange(1, L+1)
    r0 = np.sum(M**2); r1 = np.sum(M**2*np.cos(w0*l)); k = 0.96*np.pi/(w0*r0*(r0**2-r1**2))
    W = np.sqrt(M)*np.power(np.maximum(k*(r0**2+r1**2-2*r0*r1*np.cos(w0*l)), 0), 0.25)
    E = M.copy(); m = 8*l > L
    E[m] = np.where(W[m] > 1.2, 1.2*M[m], np.where(W[m] < 0.5, 0.5*M[m], W[m]*M[m]))
    return E*np.sqrt(r0/np.sum(E**2))
X, Y, XE = [], [], []
for c in cases:
    lm = np.array(c['log2M']); fw = np.log2(np.array(c['amp']))
    e = np.log2(enhance(2*np.pi*c['f0'], 2**lm))
    X.append(lm - lm.mean()); XE.append(e - e.mean()); Y.append(fw - fw.mean())
X, Y, XE = map(np.concatenate, (X, Y, XE))
for name, x in (("unenhanced", X), ("enhanced", XE)):
    s = np.dot(x, Y) / np.dot(x, x)
    r = Y - s*x
    print(f"{name:10s}: fw_shape ≈ {s:.3f} × model_shape; residual RMS {6.02*np.sqrt(np.mean(r**2)):.2f} dB (vs {6.02*np.sqrt(np.mean((Y-x)**2)):.2f} dB at slope 1)")
    # implied rho if fw uses a different prediction coefficient: (1-0.65)/(1-rho) = s
    print(f"            implied rho if only rho differs: {1 - 0.35/s:.3f}")

print("--- restricted to harmonics within 20 dB of the frame maximum (measurable) ---")
for name in ("unenhanced", "enhanced"):
    xs, ys = [], []
    for c in cases:
        lm = np.array(c['log2M']); amp = np.array(c['amp']); fw = np.log2(amp)
        model = lm if name == "unenhanced" else np.log2(enhance(2*np.pi*c['f0'], 2**lm))
        keep = 20*np.log10(amp/amp.max()) > -20
        if keep.sum() < 3: continue
        d = fw[keep] - model[keep]
        xs.append(model[keep] - np.mean(model[keep])); ys.append(fw[keep] - np.mean(fw[keep]))
    x, y = np.concatenate(xs), np.concatenate(ys)
    s = np.dot(x, y)/np.dot(x, x)
    print(f"{name:10s}: slope {s:.3f}; residual RMS at slope 1: {6.02*np.sqrt(np.mean((y-x)**2)):.2f} dB, at best slope: {6.02*np.sqrt(np.mean((y-s*x)**2)):.2f} dB  (n={len(x)})")
