#!/usr/bin/env python3
"""Extract the AMBE+2 codebook annexes (A-G) from the TIA-102.BABA-1
draft PDF by word position, and compare them entry by entry with mbelib's
tables (tools/mbelib_tables.py).  Usage: spec_tables.py [pdf]"""
import re
import sys
from collections import defaultdict

import pymupdf
import numpy as np

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mbelib_tables

PDF = sys.argv[1] if len(sys.argv) > 1 else "papers/TIA-102.BABA-1_halfrate_draft.pdf"
NUM = re.compile(r"^[−-]?\d+(\.\d+)?$")


def rows(page, tol=2.5):
    """Numeric tokens of a page grouped into rows (by y), each sorted by x."""
    words = [w for w in page.get_text("words") if NUM.match(w[4])]
    words.sort(key=lambda w: (w[1], w[0]))
    out, cur, y = [], [], None
    for w in words:
        if y is not None and abs(w[1] - y) > tol:
            out.append(sorted(cur, key=lambda w: w[0]))
            cur = []
        cur.append(w)
        y = w[1] if y is None or not cur[:-1] else y
        if len(cur) == 1:
            y = w[1]
    if cur:
        out.append(sorted(cur, key=lambda w: w[0]))
    return [[float(w[4].replace("−", "-")) for w in r] for r in out]


def table(doc, pages, width):
    """Rows of (index, v1..v_width) pairs, possibly two per printed row."""
    t = {}
    for pno in pages:
        for r in rows(doc[pno - 1]):
            for k in range(0, len(r) - width, width + 1):
                idx = r[k]
                if idx == int(idx) and all(abs(v) < 20 for v in r[k + 1:k + 1 + width]):
                    t.setdefault(int(idx), r[k + 1:k + 1 + width])
    return t


def compare(name, spec, ref, tol=5e-7):
    bad = []
    missing = [i for i in range(len(ref)) if i not in spec]
    for i in range(len(ref)):
        if i in spec and np.max(np.abs(np.array(spec[i]) - np.array(ref[i]))) > tol:
            bad.append((i, spec[i], list(ref[i])))
    print(f"{name}: spec entries found {len(spec)}/{len(ref)}; differing from mbelib: {len(bad)}; not extracted: {missing[:20]}{'...' if len(missing) > 20 else ''}")
    for i, s, r in bad[:40]:
        print(f"    [{i}] spec {s}  mbelib {[round(x, 6) for x in r]}")
    return bad, missing



def compare_all(doc, T):
    """Compare every half-rate codebook annex with mbelib."""
    out = {}
    # Annex A: rows of (b0, L, f0) triples.
    L, W0 = {}, {}
    for p in range(33, 34):
        for r in rows(doc[p - 1]):
            for k in range(0, len(r) - 2, 3):
                b0 = int(r[k])
                L[b0], W0[b0] = [r[k + 1]], [r[k + 2]]
    out["A L"] = compare("Annex A, L(b0)", L, T["AmbeLtable"].reshape(-1, 1))
    out["A W0"] = compare("Annex A, f0(b0)", W0, T["AmbeW0table"].reshape(-1, 1))
    # Annex B: b1 then 8 voicing flags.
    vuv = {int(r[0]): r[1:9] for r in rows(doc[33]) if len(r) == 9}
    out["B"] = compare("Annex B, V/UV vectors", vuv, T["AmbeVuv"])
    # Annex C: L then J1..J4.
    blk = {int(r[0]): r[1:5] for r in rows(doc[34]) if len(r) == 5}
    ref = T["AmbeLmprbl"]
    out["C"] = compare("Annex C, block lengths (L = 9..56)", {k - 9: v for k, v in blk.items()}, ref[9:57])
    # Annex D: b2 then gain level.
    dg = {int(r[0]): [r[1]] for r in rows(doc[35]) if len(r) == 2}
    out["D"] = compare("Annex D, gain levels", dg, T["AmbeDg"].reshape(-1, 1))
    # Annex G: HOC tables; page 47 carries b6 then b7.
    def tab4(page_rows):
        return [r for r in page_rows if len(r) == 5 and r[0] == int(r[0])]
    p46, p47, p48 = tab4(rows(doc[45])), tab4(rows(doc[46])), tab4(rows(doc[47]))
    b5 = {int(r[0]): r[1:] for r in p46}
    b6 = {int(r[0]): r[1:] for r in p47[:16]}
    b7 = {int(r[0]): r[1:] for r in p47[16:32]}
    b8 = {int(r[0]): r[1:] for r in p48}
    for name, spec, key in (("Annex G, HOC b5", b5, "AmbeHOCb5"), ("Annex G, HOC b6", b6, "AmbeHOCb6"),
                            ("Annex G, HOC b7", b7, "AmbeHOCb7"), ("Annex G, HOC b8", b8, "AmbeHOCb8")):
        out[name] = compare(name, spec, T[key])
    return out


if __name__ == "__main__":
    doc = pymupdf.open(PDF)
    T = mbelib_tables.load()
    compare("PRBA24 (Annex E, b3)", table(doc, range(37, 43), 3), T["AmbePRBA24"])
    compare("PRBA58 (Annex F, b4)", table(doc, range(43, 46), 4), T["AmbePRBA58"])
    compare_all(doc, T)
