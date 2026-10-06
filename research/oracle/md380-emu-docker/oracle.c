/*
  ambe-oracle: a thin, deterministic wrapper around the Tytera MD380
  firmware (D002.032) AMBE+2 3600x2450 vocoder, for generating reference
  test vectors.  Derived from md380tools/emulator/ambe.c, but:

    - never drops frames (md380-emu's encoder skips the first 26),
    - fixes the inverted .amb magic check,
    - can emit/consume the 49 voice-parameter bits as ASCII lines.

  Formats
    raw  : 8 kHz mono signed 16-bit little-endian PCM, 160 samples/frame
    amb  : DSD ".amb": 4-byte magic ".amb", then 8-byte frames:
           byte0 = 0 (status), bytes1..6 = bits 0..47 MSB-first,
           byte7 = bit 48 in the LSB.
    bits : one line per frame, 49 chars of '0'/'1' (bit 0 first).

  Usage
    ambe-oracle enc  <in.raw> <out.amb|out.bits>
    ambe-oracle dec  <in.amb|in.bits> <out.raw>
  The output/input flavour is picked from the file extension.
*/
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <fcntl.h>
#include <unistd.h>

/* Firmware entry points and buffers, resolved by --just-symbols. */
int ambe_decode_wav(signed short *wavbuffer, signed int eighty,
                    short *bitbuffer, int a4, short a5, short a6, int a7);
int ambe_encode_thing(short *bitbuffer, int a2, signed short *wavbuffer,
                      signed int eighty, int a5, short a6, short a7, int a8);
extern short ambe_inbuffer[], ambe_outbuffer0[], ambe_outbuffer1[];
extern short ambe_outbuffer[], wav_inbuffer0[], wav_inbuffer1[];
extern char ambe_mystery[], ambe_en_mystery[];

static int has_suffix(const char *s, const char *suf) {
  size_t n = strlen(s), m = strlen(suf);
  return n >= m && !strcmp(s + n - m, suf);
}

static void map_tcram(void) {
  /* 64 kB CCM/TCRAM at 0x10000000, zero-filled (not in the core dump). */
  int fd = open("/dev/zero", O_RDONLY);
  void *p = mmap((void *)0x10000000, 0x20000, PROT_READ | PROT_WRITE,
                 MAP_PRIVATE | MAP_FIXED, fd, 0);
  if (p == MAP_FAILED) { perror("mmap tcram"); exit(1); }
  if (mprotect((void *)0x0800c000, 0xf2c00, PROT_READ | PROT_EXEC))
    fprintf(stderr, "warning: mprotect(firmware, RX) failed\n");
}

static void pack49(const short *bits, unsigned char out[8]) {
  memset(out, 0, 8);
  for (int i = 0; i < 48; i++)
    out[1 + i / 8] |= (bits[i] & 1) << (7 - (i % 8));
  out[7] = bits[48] & 1;
}

static void unpack49(const unsigned char in[8], short *bits) {
  for (int i = 0; i < 48; i++)
    bits[i] = (in[1 + i / 8] >> (7 - (i % 8))) & 1;
  bits[48] = in[7] & 1;
}

static int encode(const char *inpath, const char *outpath) {
  FILE *in = fopen(inpath, "rb"), *out = fopen(outpath, "wb");
  if (!in || !out) { perror("open"); return 1; }
  int asbits = has_suffix(outpath, ".bits");
  if (!asbits) fwrite(".amb", 1, 4, out);

  short *inbuf0 = wav_inbuffer0;
  short *inbuf1 = wav_inbuffer1;
  short *ambe = ambe_outbuffer;
  short pcm[160];
  memset(ambe, 0, 49 * sizeof(short));

  int frames = 0;
  while (fread(pcm, 2, 160, in) == 160) {
    memcpy(inbuf0, pcm, 80 * 2);
    memcpy(inbuf1, pcm + 80, 80 * 2);
    /* Same arguments the radio uses (observed via hooks on hardware). */
    ambe_encode_thing(ambe, 0, inbuf0, 0x50, 0x1840, 0, 0x2000, (int)ambe_en_mystery);
    ambe_encode_thing(ambe, 0, inbuf1, 0x50, 0x1840, 1, 0x2000, (int)ambe_en_mystery);
    if (asbits) {
      for (int i = 0; i < 49; i++) fputc('0' + (ambe[i] & 1), out);
      fputc('\n', out);
    } else {
      unsigned char packed[8];
      pack49(ambe, packed);
      fwrite(packed, 1, 8, out);
    }
    frames++;
  }
  fclose(in); fclose(out);
  fprintf(stderr, "encoded %d frames\n", frames);
  return 0;
}

static int read_frame(FILE *in, int asbits, short *bits) {
  if (asbits) {
    char line[128];
    if (!fgets(line, sizeof line, in)) return 0;
    if (strlen(line) < 49) return 0;
    for (int i = 0; i < 49; i++) bits[i] = line[i] == '1';
    return 1;
  }
  unsigned char packed[8];
  if (fread(packed, 1, 8, in) != 8) return 0;
  unpack49(packed, bits);
  return 1;
}

static int decode(const char *inpath, const char *outpath) {
  FILE *in = fopen(inpath, "rb"), *out = fopen(outpath, "wb");
  if (!in || !out) { perror("open"); return 1; }
  int asbits = has_suffix(inpath, ".bits");
  if (!asbits) {
    char magic[4];
    if (fread(magic, 1, 4, in) != 4 || memcmp(magic, ".amb", 4)) {
      fprintf(stderr, "bad .amb magic\n");
      return 1;
    }
  }
  short *ambe = ambe_inbuffer;
  short *outbuf0 = ambe_outbuffer0;
  short *outbuf1 = ambe_outbuffer1;

  int frames = 0;
  while (read_frame(in, asbits, ambe)) {
    ambe_decode_wav(outbuf0, 80, ambe, 0, 0, 0, (int)ambe_mystery);
    ambe_decode_wav(outbuf1, 80, ambe, 0, 0, 1, (int)ambe_mystery);
    fwrite(outbuf0, 2, 80, out);
    fwrite(outbuf1, 2, 80, out);
    frames++;
  }
  fclose(in); fclose(out);
  fprintf(stderr, "decoded %d frames\n", frames);
  return 0;
}

int main(int argc, char **argv) {
  if (argc != 4) {
    fprintf(stderr, "usage: %s enc|dec <in> <out>\n", argv[0]);
    return 2;
  }
  map_tcram();
  if (!strcmp(argv[1], "enc")) return encode(argv[2], argv[3]);
  if (!strcmp(argv[1], "dec")) return decode(argv[2], argv[3]);
  fprintf(stderr, "unknown mode %s\n", argv[1]);
  return 2;
}
