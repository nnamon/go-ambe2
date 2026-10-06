// op25-enc: encode 8 kHz s16le PCM to AMBE+2 3600x2450 49-bit frames (.amb or .bits)
// using OP25's open-source encoder (IMBE analysis + AMBE+2 requantization).
#include <stdint.h>
#include <stdlib.h>
#include "imbe_vocoder/imbe_vocoder.h"
#include "mbelib.h"
#include "p25p2_vf.h"
#include "ambe_encoder.h"
extern "C" {
#include "../common_amb.h"
}

int main(int argc, char **argv) {
  float gain = 0;
  int a = 1;
  for (; a < argc && argv[a][0] == '-'; a++) {
    if (!strcmp(argv[a], "-g") && a + 1 < argc) gain = atof(argv[++a]);
    else { fprintf(stderr, "unknown flag %s\n", argv[a]); return 2; }
  }
  if (argc - a != 2) {
    fprintf(stderr, "usage: %s [-g gain_adjust] <in.raw> <out.amb|out.bits>\n", argv[0]);
    return 2;
  }
  const char *inpath = argv[a], *outpath = argv[a + 1];
  FILE *in = fopen(inpath, "rb"), *out = fopen(outpath, "wb");
  if (!in || !out) { perror("open"); return 1; }
  int asbits = has_suffix(outpath, ".bits");
  if (!asbits) fwrite(".amb", 1, 4, out);

  ambe_encoder enc;
  enc.set_49bit_mode();
  enc.set_gain_adjust(gain);
  int16_t pcm[160];
  uint8_t cw[72];
  int frames = 0;
  while (fread(pcm, 2, 160, in) == 160) {
    enc.encode(pcm, cw);
    amb_write_frame(out, asbits, cw);
    frames++;
  }
  fclose(in); fclose(out);
  fprintf(stderr, "encoded %d frames\n", frames);
  return 0;
}
