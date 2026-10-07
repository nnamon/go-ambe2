// op25-imbe: OP25's open-source IMBE 7200x4400 vocoder (imbe_vocoder, by
// Pavel Yazev) and P25 frame coder (op25_imbe_frame.h), as a reference:
//   op25-imbe enc in.raw out.imb    encode 8 kHz s16le PCM
//   op25-imbe dec in.imb out.raw    decode
//   op25-imbe cw  in.imb out.txt    per frame: the 144-bit code vectors c0..c7
//                                   (before interleaving) as 36 hex digits
// .imb is DSD's container: ".imb", then per frame 1 status byte and 11 bytes
// (u0..u7 concatenated, MSB first).
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <vector>
#include "imbe_vocoder/imbe_vocoder.h"
#include "op25_imbe_frame.h"

static const int vlen[8] = {12, 12, 12, 12, 11, 11, 11, 7};

static void to_bits(const uint32_t u[8], uint8_t rec[12]) {
  memset(rec, 0, 12);
  int p = 0;
  for (int i = 0; i < 8; i++)
    for (int j = vlen[i] - 1; j >= 0; j--, p++)
      rec[1 + p / 8] |= ((u[i] >> j) & 1) << (7 - p % 8);
}

static void from_bits(const uint8_t rec[12], uint32_t u[8]) {
  int p = 0;
  for (int i = 0; i < 8; i++) {
    u[i] = 0;
    for (int j = 0; j < vlen[i]; j++, p++) u[i] = u[i] << 1 | ((rec[1 + p / 8] >> (7 - p % 8)) & 1);
  }
}

int main(int argc, char **argv) {
  if (argc != 4) {
    fprintf(stderr, "usage: %s enc|dec|cw <in> <out>\n", argv[0]);
    return 2;
  }
  FILE *in = fopen(argv[2], "rb"), *out = fopen(argv[3], "wb");
  if (!in || !out) { perror("open"); return 1; }
  imbe_vocoder voc;
  int16_t fv[8], pcm[160];
  int frames = 0;
  if (!strcmp(argv[1], "enc")) {
    fwrite(".imb", 1, 4, out);
    while (fread(pcm, 2, 160, in) == 160) {
      voc.imbe_encode(fv, pcm);
      uint32_t u[8];
      for (int i = 0; i < 8; i++) u[i] = (uint16_t)fv[i];
      u[7] >>= 1; /* OP25 keeps u7 shifted left by one */
      uint8_t rec[12];
      to_bits(u, rec);
      fwrite(rec, 1, 12, out);
      frames++;
    }
  } else {
    char magic[4];
    if (fread(magic, 1, 4, in) != 4 || memcmp(magic, ".imb", 4)) { fprintf(stderr, "bad .imb magic\n"); return 1; }
    uint8_t rec[12];
    while (fread(rec, 1, 12, in) == 12) {
      uint32_t u[8];
      from_bits(rec, u);
      if (!strcmp(argv[1], "dec")) {
        for (int i = 0; i < 7; i++) fv[i] = u[i];
        fv[7] = u[7] << 1;
        voc.imbe_decode(fv, pcm);
        fwrite(pcm, 2, 160, out);
      } else {
        voice_codeword cw(voice_codeword_sz);
        imbe_header_encode(cw, u[0], u[1], u[2], u[3], u[4], u[5], u[6], u[7] << 1);
        for (int i = 0; i < 144; i += 4)
          fprintf(out, "%x", cw[i] << 3 | cw[i + 1] << 2 | cw[i + 2] << 1 | cw[i + 3]);
        fputc('\n', out);
      }
      frames++;
    }
  }
  fclose(in); fclose(out);
  fprintf(stderr, "%d frames\n", frames);
  return 0;
}
