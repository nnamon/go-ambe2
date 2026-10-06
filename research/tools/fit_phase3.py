#!/usr/bin/env python3
import json, itertools
import numpy as np
cases = json.load(open('testdata/phase_probe.json'))
sub = cases[:100]
pre = []
for c in sub:
    L = c['L']; lm = np.array(c['log2M'])
    pre.append((L, lm, np.array(c['rel']), np.array(c['amp'])))

def err(kind, D, s, comp, edge):
    tot = ws = 0
    for L, lm, rel, a in pre:
        if comp == 'log2': B = lm
        elif comp == 'sqrt': B = 2**(0.5*lm)
        elif comp == 'lin': B = 2**lm
        elif comp == 'p25': B = 2**(0.25*lm)
        Bp = np.zeros(L + 2*D + 2)
        Bp[D+1:D+1+L] = B
        if edge == 'clamp':
            Bp[:D+1] = B[0]; Bp[D+1+L:] = B[-1]
        th = np.zeros(L)
        for m in range(1, D+1):
            if kind == 'odd' and m % 2 == 0: continue
            hm = s / m
            th += hm * (Bp[D+1+m:D+1+m+L] - Bp[D+1-m:D+1-m+L])
        mrel = th - np.arange(1, L+1) * th[0]
        e = np.abs(np.angle(np.exp(1j*(rel - mrel))))
        tot += np.sum(a*e); ws += np.sum(a)
    return tot/ws

res = []
for kind, comp, edge in itertools.product(['all', 'odd'], ['log2', 'sqrt', 'p25'], ['zero', 'clamp']):
    scales = np.arange(-8, 8.01, 0.25) if comp == 'log2' else np.concatenate([np.arange(-0.2, 0.2, 0.005), np.arange(-2, 2, 0.05)])
    for D in (3, 5, 8, 12, 19, 25):
        for s in scales:
            if s == 0: continue
            res.append((err(kind, D, s, comp, edge), kind, comp, edge, D, s))
res.sort()
for r in res[:10]:
    print("err %.3f kind=%s comp=%s edge=%s D=%d s=%.3f" % r)
