/* fec_xcheck: reference 72->49 decoding by mbelib (szechyjs) using DSD's DMR
   deinterleave tables, for cross-checking the Go fec package.  Prints "hex72 bits49" lines. */
#include <stdio.h>
#include <stdlib.h>
#include "mbelib.h"

/* rW/rX/rY/rZ: DSD's DMR AMBE+2 deinterleave tables, extracted from dsd-fme at
   build time by gen_inc.py (see ../Makefile). */
#define DMR_CONST_TABLES_ONLY
static const int rW[36] = {
#include "rW.inc"
};
static const int rX[36] = {
#include "rX.inc"
};
static const int rY[36] = {
#include "rY.inc"
};
static const int rZ[36] = {
#include "rZ.inc"
};

int main(int argc, char **argv) {
  int n = argc > 1 ? atoi(argv[1]) : 20000;
  srand(12345);
  for (int f = 0; f < n; f++) {
    unsigned char b[72];
    /* Mix of clean-ish and random frames: random 72 bits stresses decoding. */
    for (int i = 0; i < 72; i++) b[i] = rand() & 1;
    char fr[4][24] = {{0}};
    for (int s = 0; s < 36; s++) {
      fr[rW[s]][rX[s]] = b[2 * s];
      fr[rY[s]][rZ[s]] = b[2 * s + 1];
    }
    char d[49];
    mbe_eccAmbe3600x2450C0(fr);
    mbe_demodulateAmbe3600x2450Data(fr);
    mbe_eccAmbe3600x2450Data(fr, d);
    for (int i = 0; i < 9; i++) {
      int v = 0;
      for (int k = 0; k < 8; k++) v = v << 1 | b[8 * i + k];
      printf("%02X", v);
    }
    putchar(' ');
    for (int i = 0; i < 49; i++) putchar('0' + d[i]);
    putchar('\n');
  }
  return 0;
}
