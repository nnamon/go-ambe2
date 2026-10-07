#!/usr/bin/env python3
"""Native (no Docker/qemu-user) MD380 D002.032 AMBE+2 oracle on Unicorn.

Loads the same unwrapped firmware + SRAM core dump that md380-emu links in,
and calls the same firmware entry points with the same arguments.  Like
md380-emu, it only writes the input buffer, calls the encode or decode entry
point and reads the output buffer.

  md380_uc.py enc in.raw out.amb|out.bits
  md380_uc.py dec in.amb|in.bits out.raw
"""
import os
import struct
import sys

from unicorn import (UC_ARCH_ARM, UC_HOOK_MEM_FETCH_UNMAPPED, UC_HOOK_MEM_READ_UNMAPPED,
                     UC_HOOK_MEM_WRITE_UNMAPPED, UC_MODE_THUMB, Uc, UcError)
from unicorn.arm_const import (UC_ARM_REG_C1_C0_2, UC_ARM_REG_FPEXC, UC_ARM_REG_LR,
                               UC_ARM_REG_PC, UC_ARM_REG_R0, UC_ARM_REG_R1, UC_ARM_REG_R2,
                               UC_ARM_REG_R3, UC_ARM_REG_SP)

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", ".."))
FW = os.path.join(ROOT, "refs/md380tools/firmware/unwrapped/D002.032.img")
CORE = os.path.join(ROOT, "refs/md380tools/cores/d02032-core.img")

# From md380tools applet/src/symbols_d02.032
SYM = dict(
    ambe_encode_thing=0x08050D91, ambe_decode_wav=0x08051249, ambe_unpack=0x08048C9D,
    ambe_inbuffer=0x20011C8E, ambe_outbuffer0=0x20011AA8, ambe_outbuffer1=0x20011B48,
    ambe_mystery=0x20011224, ambe_en_mystery=0x2000C730,
    wav_inbuffer0=0x2000DE82, wav_inbuffer1=0x2000DF22, ambe_outbuffer=0x2000DFC6,
)
FLASH, FLASH_SZ = 0x08000000, 0x00100000
FW_LOAD = 0x0800C000
SRAM, SRAM_SZ = 0x20000000, 0x00020000
TCRAM, TCRAM_SZ = 0x10000000, 0x00020000
STACK, STACK_SZ = 0x30000000, 0x00010000
RET = 0x31000000  # sentinel return address page


class Md380Vocoder:
    def __init__(self, fw=FW, core=CORE):
        uc = Uc(UC_ARCH_ARM, UC_MODE_THUMB)
        uc.mem_map(FLASH, FLASH_SZ)
        uc.mem_write(FW_LOAD, open(fw, "rb").read())
        uc.mem_map(SRAM, SRAM_SZ)
        uc.mem_write(SRAM, open(core, "rb").read())
        uc.mem_map(TCRAM, TCRAM_SZ)
        uc.mem_map(STACK, STACK_SZ)
        uc.mem_map(RET, 0x1000)
        uc.mem_write(RET, b"\x00\xbf" * 8)  # nops; we stop on reaching RET anyway
        # Enable VFP (CPACR cp10/cp11 full access + FPEXC.EN), as the M4F would.
        uc.reg_write(UC_ARM_REG_C1_C0_2, uc.reg_read(UC_ARM_REG_C1_C0_2) | (0xF << 20))
        uc.reg_write(UC_ARM_REG_FPEXC, 0x40000000)
        uc.hook_add(UC_HOOK_MEM_READ_UNMAPPED | UC_HOOK_MEM_WRITE_UNMAPPED | UC_HOOK_MEM_FETCH_UNMAPPED,
                    self._unmapped)
        self.uc = uc
        self.insn_count = 0

    def _unmapped(self, uc, access, addr, size, value, user):
        pc = uc.reg_read(UC_ARM_REG_PC)
        print(f"unmapped access type={access} addr={addr:#x} size={size} pc={pc:#x}", file=sys.stderr)
        return False

    def call(self, fn, *args):
        uc = self.uc
        regs = [UC_ARM_REG_R0, UC_ARM_REG_R1, UC_ARM_REG_R2, UC_ARM_REG_R3]
        sp = STACK + STACK_SZ - 0x100
        stack_args = [a & 0xFFFFFFFF for a in args[4:]]
        sp -= 4 * len(stack_args)
        sp &= ~7
        for k, a in enumerate(stack_args):
            uc.mem_write(sp + 4 * k, struct.pack("<I", a))
        for r, a in zip(regs, args[:4]):
            uc.reg_write(r, a & 0xFFFFFFFF)
        uc.reg_write(UC_ARM_REG_SP, sp)
        uc.reg_write(UC_ARM_REG_LR, RET | 1)
        try:
            uc.emu_start(fn | 1, RET)
        except UcError as e:
            pc = uc.reg_read(UC_ARM_REG_PC)
            raise RuntimeError(f"emulation error {e} at pc={pc:#x}") from e
        return uc.reg_read(UC_ARM_REG_R0)

    def _w16(self, addr, vals):
        self.uc.mem_write(addr, struct.pack(f"<{len(vals)}h", *vals))

    def _r16(self, addr, n):
        return list(struct.unpack(f"<{n}h", self.uc.mem_read(addr, 2 * n)))

    def encode_frame(self, pcm160):
        """160 s16 samples -> list of 49 bits (same call sequence as md380-emu)."""
        self._w16(SYM["wav_inbuffer0"], pcm160[:80])
        self._w16(SYM["wav_inbuffer1"], pcm160[80:])
        for half, buf in ((0, "wav_inbuffer0"), (1, "wav_inbuffer1")):
            self.call(SYM["ambe_encode_thing"], SYM["ambe_outbuffer"], 0, SYM[buf], 0x50,
                      0x1840, half, 0x2000, SYM["ambe_en_mystery"])
        return [b & 1 for b in self._r16(SYM["ambe_outbuffer"], 49)]

    def decode_frame(self, bits49):
        self._w16(SYM["ambe_inbuffer"], bits49)
        out = []
        for half, buf in ((0, "ambe_outbuffer0"), (1, "ambe_outbuffer1")):
            self.call(SYM["ambe_decode_wav"], SYM[buf], 80, SYM["ambe_inbuffer"], 0, 0, half,
                      SYM["ambe_mystery"])
            out += self._r16(SYM[buf], 80)
        return out


def _read_frames(path):
    if path.endswith(".bits"):
        return [[int(c) for c in l.strip()] for l in open(path) if len(l.strip()) >= 49]
    d = open(path, "rb").read()
    assert d[:4] == b".amb", "bad .amb magic"
    return [[(d[k + 1 + i // 8] >> (7 - i % 8)) & 1 for i in range(48)] + [d[k + 7] & 1]
            for k in range(4, len(d) - 7, 8)]


def _write_frames(path, frames):
    with open(path, "wb" if not path.endswith(".bits") else "w") as f:
        if path.endswith(".bits"):
            for b in frames:
                f.write("".join(map(str, b)) + "\n")
            return
        f.write(b".amb")
        for b in frames:
            p = bytearray(8)
            for i in range(48):
                p[1 + i // 8] |= b[i] << (7 - i % 8)
            p[7] = b[48]
            f.write(bytes(p))


def main():
    if len(sys.argv) != 4 or sys.argv[1] not in ("enc", "dec"):
        raise SystemExit(__doc__)
    mode, src, dst = sys.argv[1:]
    v = Md380Vocoder()
    if mode == "enc":
        raw = open(src, "rb").read()
        n = len(raw) // 320
        frames = [v.encode_frame(list(struct.unpack_from("<160h", raw, 320 * k))) for k in range(n)]
        _write_frames(dst, frames)
        print(f"encoded {n} frames", file=sys.stderr)
    else:
        frames = _read_frames(src)
        with open(dst, "wb") as f:
            for b in frames:
                f.write(struct.pack("<160h", *v.decode_frame(b)))
        print(f"decoded {len(frames)} frames", file=sys.stderr)


if __name__ == "__main__":
    main()
