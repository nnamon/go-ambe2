#!/usr/bin/env python3
"""Phase-model scoring with an optimal time alignment (linear phase) and optional
constant offset per case, which avoids amplifying the fundamental's phase error."""
import json, itertools
import numpy as np
cases = json.load(open('testdata/phase_probe.json'))
pre = [(c['L'], np.array(c['log2M']), np.array(c['rel']), np.array(c['amp'])) for c in cases]
TAU = np.linspace(-np.pi, np.pi, 256, endpoint=False)
C0 = np.linspace(-np.pi, np.pi, 32, endpoint=False)

def case_err(rel, th, a, free_c0):
    L = len(rel); l = np.arange(1, L+1)
    best = np.inf
    for c0 in (C0 if free_c0 else [0.0]):
        d = rel[None, :] - th[None, :] - c0 - TAU[:, None] * l[None, :]
        e = (np.abs(np.angle(np.exp(1j*d))) * a[None, :]).sum(1) / a.sum()
        best = min(best, e.min())
    return best

def theta(L, lm, D, s, comp, kind='all'):
    B = lm if comp == 'log2' else 2**(0.5*lm)
    Bp = np.zeros(L + 2*D + 2); Bp[D+1:D+1+L] = B
    th = np.zeros(L)
    for m in range(1, D+1):
        if kind == 'odd' and m % 2 == 0: continue
        th += s/m * (Bp[D+1+m:D+1+m+L] - Bp[D+1-m:D+1-m+L])
    return th

sub = pre[:100]
for free in (False, True):
    z = np.mean([case_err(rel, np.zeros(L), a, free) for L, lm, rel, a in sub])
    print(f"zero phase (free c0={free}): {z:.3f}")
    # random-phase reference: score random thetas
    rng = np.random.default_rng(0)
    r = np.mean([case_err(rel, rng.uniform(-np.pi, np.pi, L), a, free) for L, lm, rel, a in sub])
    print(f"random phase (free c0={free}): {r:.3f}")
best = []
for D, s, comp, kind in itertools.product((5, 10, 19), np.arange(-4, 4.01, 0.25), ('log2',), ('all', 'odd')):
    if s == 0: continue
    e = np.mean([case_err(rel, theta(L, lm, D, s, comp, kind), a, False) for L, lm, rel, a in sub[:40]])
    best.append((e, D, s, comp, kind))
best.sort(); print("best kernels:", best[:5])
