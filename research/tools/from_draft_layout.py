#!/usr/bin/env python3
"""Convert a .amb/.bits file whose b3/b4 bits follow the TIA-102.BABA-1
draft's Table 8 (as OP25's encoder writes them) to the MD-380's placement
used by the Go library (frame.FromDraftLayout).  Tone frames are left alone.
Usage: from_draft_layout.py in.amb|in.bits out.amb|out.bits"""
import os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "oracle", "unicorn"))
from ambe_eval import read_frames
from md380_uc import _write_frames
out = []
for b in read_frames(sys.argv[1]):
    if b[:6] != [1] * 6:   # not a tone frame
        b = b[:40] + [b[41], b[42], b[43], b[40]] + b[44:]
    out.append(b)
_write_frames(sys.argv[2], out)
