#!/usr/bin/env python3
"""Write a .imb file of random 88-bit frames (for the FEC cross-check).
Usage: gen_random_imb.py out.imb [frames] [seed]"""
import random, sys
out = sys.argv[1]
n = int(sys.argv[2]) if len(sys.argv) > 2 else 2000
random.seed(int(sys.argv[3]) if len(sys.argv) > 3 else 7)
with open(out, "wb") as f:
    f.write(b".imb")
    for _ in range(n):
        f.write(bytes([0]) + bytes(random.getrandbits(8) for _ in range(11)))
