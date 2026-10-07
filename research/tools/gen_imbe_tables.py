#!/usr/bin/env python3
"""Generate ../internal/codebook/imbe.go, the IMBE 7200x4400 (P25 Phase 1
full-rate) quantizer and frame tables, from the TIA-102.BABA text
(papers/BABA.txt, pdftotext -layout):

  Table 3   uniform quantizer step size by number of bits
  Table 4   standard deviation of the higher-order DCT coefficients
  Annex E   gain quantizer levels (G1)
  Annex F   bit allocation and step size for G2..G6
  Annex G   bit allocation for the higher-order DCT coefficients
  Annex H   bit frame format (interleave of the 144-bit frame)
  Annex J   block lengths

Every table is checked against upstream mbelib's imbe7200x4400_const.h
(ISC), Annex F also against OP25's imbe_vocoder (fixed point), and Annex J
against the rule of eq. 59.  The spec values are used.  mbelib (and
mbelib-neo, which inherits the table) has one known erratum: Annex F step
size for G5 at L = 23 is 0.068 there, 0.058 in the spec; OP25 agrees with
the spec, and every other 4-bit G5 step is 0.058.  Run from research/."""
import os, re, sys
import numpy as np

ROOT = os.path.dirname(os.path.abspath(__file__))
txt = open(os.path.join(ROOT, "..", "papers", "BABA.txt")).read()
txt = re.sub(r"[\x00-\x08\x0b-\x1f\x7f]", "", txt)   # pdftotext leaves stray control characters

def section(start, end):
    i = txt.index(start)
    j = txt.index(end, i + len(start))
    return txt[i:j]

def mbelib():
    src = open(os.path.join(ROOT, "..", "refs", "mbelib", "imbe7200x4400_const.h")).read()
    out = {}
    for m in re.finditer(r'const\s+(?:float|int)\s+(\w+)((?:\[\d+\])+)\s*=\s*\{(.*?)\};', src, re.S):
        body = re.sub(r'/\*.*?\*/', '', m.group(3), flags=re.S)
        body = re.sub(r'//[^\n]*', '', body)
        vals = [float(x) for x in re.findall(r'[-+]?\d*\.?\d+(?:[eE][-+]?\d+)?', body)]
        shape = [int(x) for x in re.findall(r'\[(\d+)\]', m.group(2))]
        if int(np.prod(shape)) == len(vals):
            out[m.group(1)] = np.array(vals).reshape(shape)
        else:   # trailing elements omitted: zero-fill
            a = np.zeros(int(np.prod(shape))); a[:len(vals)] = vals
            out[m.group(1)] = a.reshape(shape)
    return out

ref = mbelib()
fail = []
def check(name, ok):
    print(f"{name}: {'matches mbelib' if ok else 'DIFFERS from mbelib'}")
    if not ok: fail.append(name)

# Table 3 and Table 4.
t3 = section("Number of Bits Step Size", "Table 3: Uniform Quantizer")
step = {int(a): float(b.replace(":", ".")) for a, b in re.findall(r"^\s*(\d+)\s+(\d*:\d+)\s*$", t3, re.M)}
assert sorted(step) == list(range(1, 11)), step
t4 = section("Table 3: Uniform Quantizer", "Table 4: Standard Deviation")
sigma = {int(k): float(v) for k, v in re.findall(r"C\s*i;(\d+)\s+(\.\d+)", t4)}
assert sorted(sigma) == list(range(2, 11)), sigma
check("Table 3", np.allclose([step[b] for b in range(1, 11)], ref["quantstep"][:10]))
check("Table 4", np.allclose([sigma[k] for k in range(2, 11)], ref["standdev"]))

# Annex E.
e = section("Annex E Gain Quantizer Levels", "Annex F Bit Allocation")
gain = {int(a): float(b) for a, b in re.findall(r"(\d+)\s+(-?\d+\.\d+)", e)}
assert sorted(gain) == list(range(64)), sorted(gain)
G1 = [gain[i] for i in range(64)]
check("Annex E", np.allclose(G1, ref["B2"]))

# Annex F: L, G_m, b_m, B_m, step.
f = section("Annex F Bit Allocation and Step Size", "Annex G Bit Allocation")
gb = {}
for L, g, b, B, d in re.findall(r"(\d+)\s+G\^?\s*(\d)\s+\^?b\^?\s*(\d+)\s+(\d+)\s+(\d+\.\d+)", f):
    L, g, b = int(L), int(g), int(b)
    assert b == g + 1, (L, g, b)
    gb[(L, g)] = (int(B), float(d))
assert len(gb) == 48 * 5, len(gb)
diffs = [(L, g) for L in range(9, 57) for g in range(2, 7)
         if gb[(L, g)][0] != ref["ba"][L - 9][g - 2][0] or abs(gb[(L, g)][1] - ref["ba"][L - 9][g - 2][1]) > 1e-9]
ERRATA = [(23, 5)]   # mbelib's 0.068 for G5 at L = 23; the spec and OP25 have 0.058
check("Annex F (apart from mbelib's known erratum at L=23, G5)", diffs == ERRATA)
# The step size depends only on the element and its bit count.
steps = {}
for (L, g), (B, d) in gb.items():
    steps.setdefault((g, B), set()).add(d)
print("Annex F step sizes are a function of (element, bits):", all(len(v) == 1 for v in steps.values()))
op25 = open(os.path.join(ROOT, "..", "refs", "op25", "op25", "gr-op25_repeater", "lib", "imbe_vocoder", "tbls.cc")).read()
q = [int(x) for x in re.findall(r"\d+", op25.split("gain_step_size_tbl[] =")[1].split("};")[0])]
assert len(q) == 48 * 5
check_op25 = all(abs(q[(L - 9) * 5 + g - 2] / 65536 - gb[(L, g)][1]) < 1e-4 for L in range(9, 57) for g in range(2, 7))
print(f"Annex F step sizes: {'match' if check_op25 else 'DIFFER from'} OP25's gain_step_size_tbl (Q0.16)")
if not check_op25: fail.append("Annex F vs OP25")

# Annex G: L, C_i,k, b_m, B_m.
g = section("Annex G Bit Allocation for Higher Order", "Annex H Bit Frame Format")
hoc = {}
for L, i, k, m, B in re.findall(r"(\d+)\s+C\^?\s*(\d);(\d+)\s+\^?b\^?\s*(\d+)\s+(\d+)", g):
    hoc[(int(L), int(m))] = (int(i), int(k), int(B))
for L in range(9, 57):
    ms = sorted(m for (LL, m) in hoc if LL == L)
    assert ms == list(range(8, L + 2)), (L, ms[:5], len(ms))

# Annex J (and eq. 59).
j = section("Annex J Log Magnitude Prediction", "Annex K Flow Charts")
blk = {}
for line in j.splitlines():
    nums = [int(x) for x in re.findall(r"\d+", line)]
    for c in range(0, len(nums) - 6, 7):
        if 9 <= nums[c] <= 56 and len(nums[c:c + 7]) == 7:
            blk[nums[c]] = nums[c + 1:c + 7]
assert sorted(blk) == list(range(9, 57)), sorted(blk)
check("Annex J", all(blk[L] == list(ref["ImbeJi"][L - 9]) for L in range(9, 57)))
rule = all(blk[L] == [L // 6] * (6 - L % 6) + [L // 6 + 1] * (L % 6) for L in range(9, 57))
print("Annex J follows eq. 59 (floor(L/6) for the first 6 - L mod 6 blocks):", rule)

# Annex G is consistent with Annex J: the coefficient order is C1,2..C1,J1, ..., C6,2..C6,J6.
for L in range(9, 57):
    exp = [(i, k) for i in range(1, 7) for k in range(2, blk[L][i - 1] + 1)]
    got = [hoc[(L, m)][:2] for m in range(8, L + 2)]
    assert got == exp, (L, got[:4], exp[:4])
check("Annex G", all(hoc[(L, m)][2] == ref["hoba"][L - 9][m - 8] for L in range(9, 57) for m in range(8, L + 2)))

# Every frame carries 88 bits: 8 (b0) + K (b1) + 6 (b2) + gain and HOC bits + 1 (sync).
for L in range(9, 57):
    K = (L + 2) // 3 if L <= 36 else 12
    n = 8 + K + 6 + sum(gb[(L, g)][0] for g in range(2, 7)) + sum(hoc[(L, m)][2] for m in range(8, L + 2)) + 1
    assert n == 88, (L, n)
print("bit allocation totals 88 for every L: ok")

# Annex H: symbol -> (bit 1 source, bit 0 source) as (vector, bit).
h = section("Annex H Bit Frame Format", "Annex I Speech Synthesis Window")
sym = {}
for s, v1, b1, v0, b0 in re.findall(r"(\d+)\s+c(\d)\s*\((\d+)\)\s+c(\d)\s*\((\d+)\)", h):
    sym[int(s)] = ((int(v1), int(b1)), (int(v0), int(b0)))
assert sorted(sym) == list(range(72)), len(sym)
lens = [23, 23, 23, 23, 15, 15, 15, 7]
seen = sorted(x for pair in sym.values() for x in pair)
assert seen == [(v, b) for v in range(8) for b in range(lens[v])], "Annex H is not a permutation"
print("Annex H: a permutation of the 144 code-vector bits")

if fail:
    sys.exit("tables differ from mbelib: " + ", ".join(fail))

def golist(vals, fmt, per=8, indent="\t"):
    out = []
    for i in range(0, len(vals), per):
        out.append(indent + ", ".join(fmt(v) for v in vals[i:i + per]) + ",")
    return "\n".join(out)

lines = ["// Code generated by research/tools/gen_imbe_tables.py from TIA-102.BABA; DO NOT EDIT.", "",
         "package codebook", "",
         "// IMBE 7200x4400 (P25 Phase 1 full-rate) tables, transcribed from TIA-102.BABA",
         "// and checked entry by entry against mbelib's imbe7200x4400_const.h (which has",
         "// one erratum: the G5 step for L = 23) and, for Annex F, against OP25.", "",
         "// IMBEStep[B] is the uniform quantizer step size for a higher-order DCT",
         "// coefficient quantized with B bits, before scaling by IMBESigma (Table 3).",
         "var IMBEStep = [11]float64{0, " + ", ".join(repr(step[b]) for b in range(1, 11)) + "}", "",
         "// IMBESigma[k] is the standard deviation of DCT coefficient C_i,k, k = 2..10 (Table 4).",
         "var IMBESigma = [11]float64{0, 0, " + ", ".join(repr(sigma[k]) for k in range(2, 11)) + "}", "",
         "// IMBEGain[b2] is the quantizer level for G1 (Annex E).",
         "var IMBEGain = [64]float64{", golist(G1, repr), "}", "",
         "// IMBEGainBits[L][m-2] and IMBEGainStep[L][m-2] are the bits and step size",
         "// for transformed gain element G_m, m = 2..6 (Annex F).",
         "var IMBEGainBits = [57][5]int{"]
for L in range(9, 57):
    lines.append(f"\t{L}: {{" + ", ".join(str(gb[(L, g)][0]) for g in range(2, 7)) + "},")
lines += ["}", "", "var IMBEGainStep = [57][5]float64{"]
for L in range(9, 57):
    lines.append(f"\t{L}: {{" + ", ".join(repr(gb[(L, g)][1]) for g in range(2, 7)) + "},")
lines += ["}", "",
          "// IMBEHOCBits[L][m-8] is the number of bits of b_m, m = 8..L+1, which carry the",
          "// higher-order DCT coefficients C1,2..C1,J1, ..., C6,2..C6,J6 in that order (Annex G).",
          "var IMBEHOCBits = [57][]int{"]
for L in range(9, 57):
    lines.append(f"\t{L}: {{" + ", ".join(str(hoc[(L, m)][2]) for m in range(8, L + 2)) + "},")
lines += ["}", "", "// IMBEBlockLen[L] gives the six block lengths J1..J6 for L harmonics (Annex J).",
          "var IMBEBlockLen = [57][6]int{"]
for L in range(9, 57):
    lines.append(f"\t{L}: {{" + ", ".join(str(x) for x in blk[L]) + "},")
lines += ["}", "",
          "// IMBEInterleave[s] gives, for dibit symbol s = 0..71 of the 144-bit frame, the",
          "// code vector and bit (bit N-1 is a vector's MSB) carried by the symbol's bit 1",
          "// and bit 0, in that order (Annex H).",
          "var IMBEInterleave = [72][2][2]int{"]
for s in range(72):
    (v1, b1), (v0, b0) = sym[s]
    lines.append(f"\t{{{{{v1}, {b1}}}, {{{v0}, {b0}}}}},")
lines += ["}", ""]
dst = os.path.join(ROOT, "..", "..", "internal", "codebook", "imbe.go")
open(dst, "w").write("\n".join(lines))
os.system(f"gofmt -w {dst}")
print("wrote", os.path.normpath(dst))
