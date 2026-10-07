#!/usr/bin/env python3
"""Noisy-speech evaluation of the encoders and the noise suppressor.  Run from
research/ after `make -C tools go` (needs bin/mbevoc-enc, bin/mbevoc-dec,
bin/denoise and the MD-380 oracle).

  eval_noise.py [--set dev|heldout] [--types white,pink,babble] [--snrs 30,20,10]
                [--variants fw,go,ns,nsgo] [--ns "-floor -15"] [--tag name]

Each recording of the set gets white, pink or babble noise at the given
speech-to-noise ratios (speech level: the mean power of its 20 ms frames
within 30 dB of the loudest).  Babble is six talkers from the other set, so
tuning on dev never hears held-out speech.  Variants:

  fw    the MD-380 encoder (black box), decoded by the MD-380 decoder and mbevoc-dec
  go    mbevoc-enc, decoded by both
  ns    the noise suppressor alone (bin/denoise), not coded
  nsgo  the suppressor, then mbevoc-enc, decoded by both
  dn    mbevoc-enc -denoise (the suppressor inside the encoder), decoded by both

Only recordings whose own background is at least --min-snr (30) dB below the
speech are used, so that the clean recording is a fair reference: several
corpus recordings have background noise 13-24 dB down, which the suppressor
would rightly remove and PESQ would count against it.  Scores are PESQ-NB
and STOI against the clean recording.  Noisy inputs and
MD-380 results are cached under testdata/noise/."""
import argparse
import glob
import os
import shlex
import subprocess
import sys
import zlib
from concurrent.futures import ProcessPoolExecutor

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_pcm  # noqa: E402
from pesq import pesq  # noqa: E402
from pystoi import stoi  # noqa: E402

ROOT = 'testdata/noise'
PY = '.venv/bin/python'


def quality(ref, deg):
    """PESQ-NB and STOI of deg against ref, after aligning them (as tools/score.py)."""
    d = deg[estimate_delay(ref, deg):]
    n = min(len(ref), len(d))
    r, d = ref[:n], d[:n]
    try:
        pq = pesq(8000, r.astype(np.int16), np.clip(d, -32768, 32767).astype(np.int16), 'nb')
    except Exception:  # noqa: BLE001
        pq = float('nan')
    return pq, stoi(r, d, 8000, extended=False)


def background_snr(path):
    """Speech level over the recording's own background (its quietest 10% of frames), dB."""
    x = read_pcm(path).astype(float)
    e = np.mean(x[:len(x) // 160 * 160].reshape(-1, 160) ** 2, axis=1) + 1e-9
    return 10 * np.log10(np.mean(e[e > e.max() * 1e-3]) / np.percentile(e, 10))


def active_power(x):
    f = x[:len(x) // 160 * 160].reshape(-1, 160)
    e = np.mean(f ** 2, axis=1)
    return np.mean(e[e > e.max() * 1e-3])


def make_noise(kind, n, seed, other):
    rng = np.random.default_rng(seed)
    if kind == 'white':
        return rng.normal(0, 1, n)
    if kind == 'pink':
        w = np.fft.rfft(rng.normal(0, 1, n))
        f = np.fft.rfftfreq(n, 1 / 8000)
        w[1:] /= np.sqrt(f[1:] / 100)
        w[0] = 0
        return np.fft.irfft(w, n)
    if kind == 'babble':
        b = np.zeros(n)
        for path in rng.choice(other, 6, replace=False):
            s = read_pcm(path).astype(float)
            s /= np.sqrt(active_power(s))
            s = np.roll(np.resize(s, n), rng.integers(len(s)))
            b += s
        return b
    raise ValueError(kind)


def prepare(args):
    """Noisy inputs for every condition; returns [(cond, name, dir)]."""
    sets = {'dev': 'testdata/matrix', 'heldout': 'testdata/heldout'}
    files = [f for f in sorted(glob.glob(sets[args.set] + '/*/ref.raw')) if background_snr(f) >= args.min_snr]
    other = sorted(glob.glob(sets['heldout' if args.set == 'dev' else 'dev'] + '/*/ref.raw'))
    jobs = []
    for kind in args.types:
        for snr in args.snrs:
            cond = 'clean' if kind == 'clean' else f'{kind}{snr}'
            for f in files:
                name = os.path.basename(os.path.dirname(f))
                d = os.path.join(ROOT, args.set, cond, name)
                os.makedirs(d, exist_ok=True)
                if not os.path.exists(d + '/in.raw'):
                    ref = read_pcm(f).astype(float)
                    if kind == 'clean':
                        x = ref
                    else:
                        nz = make_noise(kind, len(ref), zlib.crc32(f'{kind}{snr}{name}'.encode()), other)
                        nz *= np.sqrt(active_power(ref) / 10 ** (snr / 10) / np.mean(nz ** 2))
                        x = ref + nz
                    np.clip(np.round(x), -32768, 32767).astype('<i2').tofile(d + '/in.raw')
                jobs.append((cond, name, d, f))
            if kind == 'clean':
                break
    return jobs


def run(cmd):
    subprocess.run(cmd, check=True, capture_output=True)


def decode_both(amb, stem):
    if not os.path.exists(stem + '.fw.raw'):
        run([PY, 'oracle/unicorn/md380_uc.py', 'dec', amb, stem + '.fw.raw'])
    run(['bin/mbevoc-dec', amb, stem + '.go.raw'])


def evaluate(job):
    cond, name, d, reffile, variants, nsargs, tag = job
    ref = read_pcm(reffile)
    out = {}
    if 'fw' in variants:
        if not os.path.exists(d + '/fw.amb'):
            run([PY, 'oracle/unicorn/md380_uc.py', 'enc', d + '/in.raw', d + '/fw.amb'])
        decode_both(d + '/fw.amb', d + '/fw')
        for dec in ('fw', 'go'):
            out[('fw', dec)] = quality(ref, read_pcm(f'{d}/fw.{dec}.raw'))[:2]
    if 'go' in variants:
        run(['bin/mbevoc-enc', d + '/in.raw', d + '/go.amb'])
        for f in glob.glob(d + '/go.fw.raw'):
            os.remove(f)
        decode_both(d + '/go.amb', d + '/go')
        for dec in ('fw', 'go'):
            out[('go', dec)] = quality(ref, read_pcm(f'{d}/go.{dec}.raw'))[:2]
    if 'ns' in variants or 'nsgo' in variants:
        run(['bin/denoise'] + shlex.split(nsargs) + [d + '/in.raw', f'{d}/{tag}.ns.raw'])
        if 'ns' in variants:
            out[('ns', '-')] = quality(ref, read_pcm(f'{d}/{tag}.ns.raw'))[:2]
        if 'nsgo' in variants:
            run(['bin/mbevoc-enc', f'{d}/{tag}.ns.raw', f'{d}/{tag}.nsgo.amb'])
            for f in glob.glob(f'{d}/{tag}.nsgo.fw.raw'):
                os.remove(f)
            decode_both(f'{d}/{tag}.nsgo.amb', f'{d}/{tag}.nsgo')
            for dec in ('fw', 'go'):
                out[('nsgo', dec)] = quality(ref, read_pcm(f'{d}/{tag}.nsgo.{dec}.raw'))[:2]
    if 'dn' in variants:
        run(['bin/mbevoc-enc', '-denoise', d + '/in.raw', d + '/dn.amb'])
        for f in glob.glob(d + '/dn.fw.raw'):
            os.remove(f)
        decode_both(d + '/dn.amb', d + '/dn')
        for dec in ('fw', 'go'):
            out[('dn', dec)] = quality(ref, read_pcm(f'{d}/dn.{dec}.raw'))[:2]
    out[('input', '-')] = quality(ref, read_pcm(d + '/in.raw'))[:2]
    return cond, name, out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--set', default='dev')
    ap.add_argument('--types', default='clean,white,pink,babble')
    ap.add_argument('--snrs', default='30,20,10')
    ap.add_argument('--variants', default='fw,go,ns,nsgo')
    ap.add_argument('--ns', default='')
    ap.add_argument('--tag', default='t')
    ap.add_argument('--jobs', type=int, default=os.cpu_count())
    ap.add_argument('--min-snr', type=float, default=30,
                    help='use only recordings whose own background is this far below the speech (dB)')
    a = ap.parse_args()
    a.types = a.types.split(',')
    a.snrs = [int(s) for s in a.snrs.split(',')]
    variants = a.variants.split(',')
    jobs = [j + (variants, a.ns, a.tag) for j in prepare(a)]
    res = {}
    with ProcessPoolExecutor(a.jobs) as ex:
        for cond, name, out in ex.map(evaluate, jobs):
            for k, v in out.items():
                res.setdefault(cond, {}).setdefault(k, []).append(v)
    cols = [('input', '-')] + [(v, dec) for v in ('fw', 'go', 'nsgo', 'dn') if v in variants for dec in ('fw', 'go')]
    if 'ns' in variants:
        cols.insert(1, ('ns', '-'))
    label = {('input', '-'): 'noisy in', ('ns', '-'): 'NS only', ('fw', 'fw'): 'fw>fw', ('fw', 'go'): 'fw>go',
             ('go', 'fw'): 'go>fw', ('go', 'go'): 'go>go', ('nsgo', 'fw'): 'NS+go>fw', ('nsgo', 'go'): 'NS+go>go',
             ('dn', 'fw'): 'go -dn>fw', ('dn', 'go'): 'go -dn>go'}
    print(f"set {a.set} ({len(jobs) // max(1, len(res))} recordings), PESQ-NB / STOI (encoder>decoder; fw = MD-380), NS args '{a.ns}'")
    print(f"{'condition':10s}" + ''.join(f"{label[c]:>16s}" for c in cols))
    tot = {}
    for cond, r in res.items():
        line = f"{cond:10s}"
        for c in cols:
            v = np.array(r[c])
            line += f"{v[:, 0].mean():9.3f} /{v[:, 1].mean():5.3f}"
            if cond != 'clean':
                tot.setdefault(c, []).append(v.mean(axis=0))
        print(line)
    if tot:
        print(f"{'noisy mean':10s}" + ''.join(f"{np.mean(tot[c], axis=0)[0]:9.3f} /{np.mean(tot[c], axis=0)[1]:5.3f}" for c in cols))


if __name__ == '__main__':
    main()
