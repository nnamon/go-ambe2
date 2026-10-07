/* neo-codec: mbelib-neo (GPL-2.0-or-later, research use only) as a reference
   for this module's codecs:
     neo-codec dstar-enc in.raw out.dmb   D-STAR AMBE encoder (8 kHz s16le in)
     neo-codec dstar-dec in.dmb out.raw   D-STAR AMBE decoder
     neo-codec ambe2-dec in.amb out.raw   AMBE+2 3600x2450 decoder
     neo-codec imbe-dec  in.imb out.raw   IMBE 7200x4400 decoder
     neo-codec dstar-dv  in.dmb out.dv    D-STAR FEC + interleave: 9-byte voice data
     neo-codec pv-params in.pv out.imb    ProVoice (IMBE 7100x4400) frames -> error
                                          correction, demodulation and reordering
                                          to the 88-bit P25 order (.imb)
     neo-codec p25-params in.imbe144 out.imb  P25 IMBE 144-bit frames -> error correction
                                          and demodulation to the 88 bits (.imb)
   .pv: 18 bytes per frame, the 142 frame bits MSB-first in DSD's order (pW, pX).
   .imbe144: 18 bytes per frame, the 144 frame bits MSB-first (TIA-102.BABA Annex
   H order; DSD's iW..iZ).
   .dmb/.amb: 4-byte header, then per frame 1 status byte, bits 0..47 MSB-first
   in 6 bytes, bit 48 in the LSB of a 7th.  .imb: header, then 1 status byte and
   the 88 bits MSB-first in 11 bytes. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <mbelib-neo/mbelib.h>
#define _MAIN
#include "provoice_const.h"
#include "p25p1_const.h"

static int read49(FILE *in, char d[49]) {
  unsigned char p[8];
  if (fread(p, 1, 8, in) != 8) return 0;
  for (int i = 0; i < 48; i++) d[i] = (p[1 + i / 8] >> (7 - i % 8)) & 1;
  d[48] = p[7] & 1;
  return 1;
}

static void write49(FILE *out, const char d[49]) {
  unsigned char p[8] = {0};
  for (int i = 0; i < 48; i++) p[1 + i / 8] |= (d[i] & 1) << (7 - i % 8);
  p[7] = d[48] & 1;
  fwrite(p, 1, 8, out);
}

int main(int argc, char **argv) {
  if (argc != 4) {
    fprintf(stderr, "usage: %s dstar-enc|dstar-dec|ambe2-dec|imbe-dec <in> <out>\n", argv[0]);
    return 2;
  }
  const char *mode = argv[1];
  FILE *in = fopen(argv[2], "rb"), *out = fopen(argv[3], "wb");
  if (!in || !out) { perror("open"); return 1; }
  mbe_parms cur, prev, enh;
  mbe_initMbeParms(&cur, &prev, &enh);
  mbe_setThreadRngSeed(1);
  short pcm[160];
  int frames = 0;
  if (!strcmp(mode, "dstar-enc")) {
    mbe_ambe2400_encoder *enc = mbe_ambe2400EncoderAlloc();
    fwrite(".dmb", 1, 4, out);
    char d[49];
    while (fread(pcm, 2, 160, in) == 160) {
      if (mbe_encodeAmbe2400ParmsShort(enc, pcm, d, &cur, &prev) < 0) { fprintf(stderr, "encode failed\n"); return 1; }
      mbe_moveMbeParms(&cur, &prev);
      write49(out, d);
      frames++;
    }
    mbe_ambe2400EncoderFree(enc);
  } else if (!strcmp(mode, "pv-params")) {
    unsigned char rec[18], out12[12];
    fwrite(".imb", 1, 4, out);
    while (fread(rec, 1, 18, in) == 18) {
      char fr[7][24], d[88];
      mbe_process_result res;
      memset(fr, 0, sizeof fr);
      for (int n = 0; n < 142; n++) fr[pW[n]][pX[n]] = (rec[n / 8] >> (7 - n % 8)) & 1;
      mbe_initProcessResult(&res);
      if (mbe_decodeImbe7100x4400Frame(fr, d, &res) < 0) { fprintf(stderr, "decode failed\n"); return 1; }
      memset(out12, 0, 12);
      for (int i = 0; i < 88; i++) out12[1 + i / 8] |= (d[i] & 1) << (7 - i % 8);
      fwrite(out12, 1, 12, out);
      frames++;
    }
  } else if (!strcmp(mode, "p25-params")) {
    unsigned char rec[18], out12[12];
    fwrite(".imb", 1, 4, out);
    while (fread(rec, 1, 18, in) == 18) {
      char fr[8][23], d[88];
      mbe_process_result res;
      memset(fr, 0, sizeof fr);
      for (int s2 = 0; s2 < 72; s2++) {
        fr[iW[s2]][iX[s2]] = (rec[(2 * s2) / 8] >> (7 - (2 * s2) % 8)) & 1;
        fr[iY[s2]][iZ[s2]] = (rec[(2 * s2 + 1) / 8] >> (7 - (2 * s2 + 1) % 8)) & 1;
      }
      mbe_initProcessResult(&res);
      if (mbe_decodeImbe7200x4400Frame(fr, d, &res) < 0) { fprintf(stderr, "decode failed\n"); return 1; }
      memset(out12, 0, 12);
      for (int i = 0; i < 88; i++) out12[1 + i / 8] |= (d[i] & 1) << (7 - i % 8);
      fwrite(out12, 1, 12, out);
      frames++;
    }
  } else if (!strcmp(mode, "dstar-dv")) {
    char magic[4], d[49], fr[4][24];
    unsigned char dv[9];
    if (fread(magic, 1, 4, in) != 4) { fprintf(stderr, "short file\n"); return 1; }
    while (read49(in, d)) {
      if (mbe_encodeAmbe3600x2400Frame(d, fr) < 0 || mbe_encodeDStarDVData(fr, dv) < 0) { fprintf(stderr, "frame encode failed\n"); return 1; }
      fwrite(dv, 1, 9, out);
      frames++;
    }
  } else {
    char magic[4];
    if (fread(magic, 1, 4, in) != 4) { fprintf(stderr, "short file\n"); return 1; }
    mbe_process_result res;
    for (;;) {
      mbe_initProcessResult(&res);
      if (!strcmp(mode, "imbe-dec")) {
        unsigned char rec[12];
        char d[88];
        if (fread(rec, 1, 12, in) != 12) break;
        for (int i = 0; i < 88; i++) d[i] = (rec[1 + i / 8] >> (7 - i % 8)) & 1;
        mbe_processImbe4400Data(pcm, &res, d, &cur, &prev, &enh);
      } else {
        char d[49];
        if (!read49(in, d)) break;
        if (!strcmp(mode, "dstar-dec")) mbe_processAmbe2400Data(pcm, &res, d, &cur, &prev, &enh);
        else mbe_processAmbe2450Data(pcm, &res, d, &cur, &prev, &enh);
      }
      fwrite(pcm, 2, 160, out);
      frames++;
    }
  }
  fclose(in); fclose(out);
  fprintf(stderr, "%d frames\n", frames);
  return 0;
}
