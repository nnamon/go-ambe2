/* hamming-check: encode every 11-bit word with the P25 [15,11] Hamming code of
   TIA-102.BABA (generator g_H), flip each bit in turn, and count the words whose
   data mbe_hamming1511 fails to recover.  Built against mbelib (hamming-check)
   and mbelib-neo (hamming-check-neo). */
#include <stdio.h>
#include <string.h>
#ifdef NEO
#include <mbelib-neo/mbelib.h>
#else
#include "mbelib.h"
#endif
static const int rows[11] = {0xF,0xE,0xD,0xC,0xB,0xA,0x9,0x7,0x6,0x5,0x3};
int main(void) {
  int bad[15] = {0}, trials = 0;
  for (int d = 0; d < 2048; d++) {
    int par = 0;
    for (int i = 0; i < 11; i++) if (d & (1 << (10 - i))) par ^= rows[i];
    int cw = d << 4 | par;
    for (int p = 0; p < 15; p++) {
      char in[15], out[15];
      int e = cw ^ (1 << p);
      for (int j = 0; j < 15; j++) in[j] = (e >> j) & 1;   /* in[14] = MSB, as mbelib's imbe_fr */
      mbe_hamming1511(in, out);
      int got = 0;
      for (int j = 14; j >= 4; j--) got = got << 1 | out[j];
      if (got != d) bad[p]++;
    }
    trials++;
  }
  printf("single-error positions (bit 14 = data MSB) left uncorrected by mbe_hamming1511, out of %d words each:\n", trials);
  for (int p = 14; p >= 0; p--) printf("  bit %2d: %d\n", p, bad[p]);
  return 0;
}
