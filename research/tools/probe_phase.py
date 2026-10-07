#!/usr/bin/env python3
"""Black-box probe: what harmonic phases does the MD-380 decoder synthesize for
steady voiced frames?  Compares measured relative phases with zero phase and
with minimum phase derived from the decoded spectral amplitudes."""
import subprocess, sys
import tempfile
import numpy as np
sys.path.insert(0, 'oracle/unicorn'); sys.path.insert(0, 'tools')
from md380_uc import Md380Vocoder
from ambe_eval import LAYOUT
import mbelib_tables
T = mbelib_tables.load()

def bits(f):
    b = [0]*49
    for pos, (fi, sig) in LAYOUT.items(): b[pos] = (f[fi] >> sig) & 1
    return b

def minphase(logA, L, n_fft=4096):
    """Minimum phase at harmonics 1..L for log-amplitudes (natural log) logA[1..L]."""
    f = np.arange(n_fft // 2 + 1) / n_fft          # cycles/sample 0..0.5, harmonics at l*f0
    return f

W0 = T['AmbeW0table']
for b0 in (60, 90):
    f0 = W0[b0]; L = int(T['AmbeLtable'][b0])
    for b3, b4 in ((5, 9), (300, 70), (120, 30)):
        fr = bits([b0, 0, 16, b3, b4, 3, 3, 3, 3])
        v = Md380Vocoder()
        pcm = np.concatenate([np.array(v.decode_frame(fr), float) for _ in range(60)])
        # decoded amplitudes of the steady frame
        amb = SP + '/p.bits'
        open(amb, 'w').write(''.join(map(str, fr)) * 0 + ('\n'.join(''.join(map(str, fr)) for _ in range(60)) + '\n'))
        out = subprocess.run(['bin/mbevoc-params', amb], capture_output=True, text=True).stdout.splitlines()[-1].split()
        logM = np.array(list(map(float, out[15:15 + L]))) * np.log(2)
        # Measure harmonic phases over an analysis span centred in the steady region.
        P = 1 / f0
        start = 160 * 40
        n = int(round(8 * P))
        seg = pcm[start:start + n]
        t = np.arange(n) + start
        # least-squares fit of harmonics at exact frequencies
        A = np.column_stack([np.cos(2*np.pi*f0*l*t) for l in range(1, L+1)] + [np.sin(2*np.pi*f0*l*t) for l in range(1, L+1)])
        c, *_ = np.linalg.lstsq(A, seg, rcond=None)
        amp = np.hypot(c[:L], c[L:]); ph = np.arctan2(-c[L:], c[:L])
        # relative phase: remove linear phase term (time shift) fitted on harmonics 1..L weighted by amplitude
        # choose time reference that maximises alignment: rel_l = ph_l - l*ph_1
        rel = np.angle(np.exp(1j * (ph - np.arange(1, L+1) * ph[0])))
        # minimum phase prediction from decoded log amplitudes (cepstral method on harmonic grid)
        Nf = 1024
        grid = np.arange(Nf // 2 + 1) / Nf
        logEnv = np.interp(grid, f0 * np.arange(1, L+1), logM)
        cep = np.fft.irfft(logEnv, Nf)
        fold = np.zeros(Nf); fold[0] = cep[0]; fold[1:Nf//2] = 2 * cep[1:Nf//2]; fold[Nf//2] = cep[Nf//2]
        mp = np.angle(np.exp(np.fft.rfft(fold)))
        mph = np.interp(f0 * np.arange(1, L+1), grid, np.unwrap(mp))
        mrel = np.angle(np.exp(1j * (mph - np.arange(1, L+1) * mph[0])))
        w = amp / amp.sum()
        err_zero = np.sum(w * np.abs(np.angle(np.exp(1j * rel)))) 
        err_mp = np.sum(w * np.abs(np.angle(np.exp(1j * (rel - mrel)))))
        err_mpn = np.sum(w * np.abs(np.angle(np.exp(1j * (rel + mrel)))))
        crest = np.max(np.abs(seg)) / np.sqrt(np.mean(seg**2))
        print(f"b0={b0} f0={f0*8000:.0f}Hz L={L} b3={b3} b4={b4}: amp-weighted |phase err| vs zero-phase {err_zero:.2f} rad, vs min-phase {err_mp:.2f}, vs max-phase {err_mpn:.2f}; crest {crest:.2f}")
        print("    measured rel phase l=1..10:", " ".join(f"{x:+.2f}" for x in rel[:10]))
        print("    min-phase  rel phase l=1..10:", " ".join(f"{x:+.2f}" for x in mrel[:10]))
