/* Shared .amb / .bits I/O for the native reference tools (see oracle.c for format docs). */
#include <stdio.h>
#include <string.h>

static int has_suffix(const char *s, const char *suf) {
  size_t n = strlen(s), m = strlen(suf);
  return n >= m && !strcmp(s + n - m, suf);
}

/* Reads one 49-bit frame into bits[49] (values 0/1). Returns 1 on success. */
static int amb_read_frame(FILE *in, int asbits, char bits[49]) {
  if (asbits) {
    char line[128];
    if (!fgets(line, sizeof line, in) || strlen(line) < 49) return 0;
    for (int i = 0; i < 49; i++) bits[i] = line[i] == '1';
    return 1;
  }
  unsigned char p[8];
  if (fread(p, 1, 8, in) != 8) return 0;
  for (int i = 0; i < 48; i++) bits[i] = (p[1 + i / 8] >> (7 - (i % 8))) & 1;
  bits[48] = p[7] & 1;
  return 1;
}

static void amb_write_frame(FILE *out, int asbits, const unsigned char bits[49]) {
  if (asbits) {
    for (int i = 0; i < 49; i++) fputc('0' + (bits[i] & 1), out);
    fputc('\n', out);
    return;
  }
  unsigned char p[8] = {0};
  for (int i = 0; i < 48; i++) p[1 + i / 8] |= (bits[i] & 1) << (7 - (i % 8));
  p[7] = bits[48] & 1;
  fwrite(p, 1, 8, out);
}

static int amb_open_in(FILE *in, int asbits) {
  if (asbits) return 1;
  char magic[4];
  return fread(magic, 1, 4, in) == 4 && !memcmp(magic, ".amb", 4);
}
