/* mbelib-dec: decode AMBE+2 3600x2450 49-bit frames (.amb or .bits) to 8 kHz s16le
   PCM with szechyjs/mbelib.  Optional -p prints the decoded model parameters
   per frame to stderr: idx w0 L gamma Vl-string log2M[1..L]. */
#include <stdlib.h>
#include "mbelib.h"
#include "../common_amb.h"

int main(int argc, char **argv) {
  int params = 0, uvq = 3;
  int a = 1;
  for (; a < argc && argv[a][0] == '-'; a++) {
    if (!strcmp(argv[a], "-p")) params = 1;
    else if (!strcmp(argv[a], "-q") && a + 1 < argc) uvq = atoi(argv[++a]);
    else { fprintf(stderr, "unknown flag %s\n", argv[a]); return 2; }
  }
  if (argc - a != 2) {
    fprintf(stderr, "usage: %s [-p] [-q uvquality] <in.amb|in.bits> <out.raw>\n", argv[0]);
    return 2;
  }
  const char *inpath = argv[a], *outpath = argv[a + 1];
  FILE *in = fopen(inpath, "rb"), *out = fopen(outpath, "wb");
  if (!in || !out) { perror("open"); return 1; }
  int asbits = has_suffix(inpath, ".bits");
  if (!amb_open_in(in, asbits)) { fprintf(stderr, "bad .amb magic\n"); return 1; }

  mbe_parms cur, prev, prev_enh;
  mbe_initMbeParms(&cur, &prev, &prev_enh);
  char ambe_d[49], err_str[64];
  short pcm[160];
  int frames = 0;
  while (amb_read_frame(in, asbits, ambe_d)) {
    int errs = 0, errs2 = 0;
    err_str[0] = 0;
    mbe_processAmbe2450Data(pcm, &errs, &errs2, err_str, ambe_d, &cur, &prev, &prev_enh, uvq);
    if (params) {
      /* After processing, prev holds this frame's (unenhanced) parameters. */
      fprintf(stderr, "%d %.9g %d %.9g ", frames, prev.w0, prev.L, prev.gamma);
      for (int l = 1; l <= prev.L; l++) fputc('0' + prev.Vl[l], stderr);
      for (int l = 1; l <= prev.L; l++) fprintf(stderr, " %.9g", prev.log2Ml[l]);
      fputc('\n', stderr);
    }
    fwrite(pcm, 2, 160, out);
    frames++;
  }
  fclose(in); fclose(out);
  fprintf(stderr, "decoded %d frames\n", frames);
  return 0;
}
