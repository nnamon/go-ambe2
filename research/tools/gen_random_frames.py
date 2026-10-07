#!/usr/bin/env python3
"""Write a file of random frames for the frame-coding cross-checks.
Usage: gen_random_frames.py imb|dmb out [frames] [seed]
  imb: DSD .imb of 88-bit IMBE frames
  dmb: dsd-fme .dmb of 49-bit D-STAR frames (bit 24, which carries no
       parameter, is 0)"""
import random, sys
kind, out = sys.argv[1], sys.argv[2]
n = int(sys.argv[3]) if len(sys.argv) > 3 else 2000
random.seed(int(sys.argv[4]) if len(sys.argv) > 4 else (7 if kind == "imb" else 11))
with open(out, "wb") as f:
    if kind == "imb":
        f.write(b".imb")
        for _ in range(n):
            f.write(bytes([0]) + bytes(random.getrandbits(8) for _ in range(11)))
    else:
        f.write(b".dmb")
        for _ in range(n):
            bits = [random.getrandbits(1) for _ in range(49)]
            bits[24] = 0
            p = bytearray(8)
            for i in range(48):
                p[1 + i // 8] |= bits[i] << (7 - i % 8)
            p[7] = bits[48]
            f.write(bytes(p))
