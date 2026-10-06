#!/usr/bin/env python3
"""Extract DSD's DMR AMBE deinterleave tables (rW, rX, rY, rZ) from a dsd-fme
checkout into rW.inc .. rZ.inc for main.c.  (Generated at build time so the
GPL-licensed tables are not copied into this repository.)"""
import os
import re
import sys

src = open(sys.argv[1]).read()
out = sys.argv[2] if len(sys.argv) > 2 else os.path.dirname(os.path.abspath(__file__))
for n in ("rW", "rX", "rY", "rZ"):
    m = re.search(r"const int " + n + r"\[36\]\s*=\s*\{(.*?)\};", src, re.S)
    if not m:
        raise SystemExit(f"{n} not found")
    open(os.path.join(out, n + ".inc"), "w").write(m.group(1).strip() + "\n")
