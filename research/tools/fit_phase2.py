#!/usr/bin/env python3
"""Gauss-Newton fit of phase models to the MD-380 decoder phase probe data."""
import json, sys
import numpy as np
cases = json.load(open('testdata/phase_probe.json'))
train, test = cases[:200], cases[200:]
D = 20; NL = 56

def design(c, edge, use_h, use_c):
    """Rows: harmonics 1..L. Columns: h_1..h_D, c_1..c_NL.  mrel = theta_l - l*theta_1."""
    L = c['L']; B = np.array(c['log2M'])
    def ext(l):
        if 1 <= l <= L: return B[l-1]
        return 0.0 if edge == 'zero' else B[min(max(l, 1), L) - 1]
    cols = []
    if use_h:
        Hm = np.array([[ext(l+m) - ext(l-m) for m in range(1, D+1)] for l in range(1, L+1)])
        cols.append(Hm - np.arange(1, L+1)[:, None] * Hm[0][None, :])
    if use_c:
        C = np.zeros((L, NL)); 
        for l in range(1, L+1): C[l-1, l-1] = 1
        C[:, 0] -= np.arange(1, L+1)   # theta_1 term
        cols.append(C)
    return np.hstack(cols)

def fit(edge, use_h, use_c, iters=30):
    X = [design(c, edge, use_h, use_c) for c in train]
    npar = X[0].shape[1]; p = np.zeros(npar)
    for it in range(iters):
        JTJ = np.eye(npar) * 1e-3; JTr = np.zeros(npar)
        for c, A in zip(train, X):
            w = np.array(c['amp']); w = w / w.sum()
            r = np.angle(np.exp(1j*(np.array(c['rel']) - A @ p)))
            JTJ += A.T @ (A * w[:, None]); JTr += A.T @ (w * r)
        p = p + np.linalg.solve(JTJ, JTr)
    return p

def evaluate(p, edge, use_h, use_c, data):
    tot = ws = 0
    for c in data:
        A = design(c, edge, use_h, use_c); a = np.array(c['amp'])
        e = np.abs(np.angle(np.exp(1j*(np.array(c['rel']) - A @ p))))
        tot += np.sum(a*e); ws += np.sum(a)
    return tot / ws

for name, edge, uh, uc in (("kernel(zero edge)", 'zero', True, False), ("kernel(clamp edge)", 'clamp', True, False),
                           ("fixed table", 'zero', False, True), ("kernel+table", 'zero', True, True)):
    p = fit(edge, uh, uc)
    print(f"{name:20s} train err {evaluate(p, edge, uh, uc, train):.3f}  test err {evaluate(p, edge, uh, uc, test):.3f}")
    if uh:
        print("   h:", " ".join(f"{x:+.3f}" for x in p[:D]))
