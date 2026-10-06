#!/usr/bin/env python3
"""Recursive-descent Thumb-2 call-graph walk over the MD380 firmware, starting
from the AMBE entry points, to scope how much code the vocoder actually is.

  fw_callgraph.py [firmware.img] [base]

Heuristic: follows direct branches/BLs, stops blocks at returns (bx lr,
pop {..pc}, ldr pc / ldm..pc), treats B to an address outside the current
function's explored region as a tail call only if it's a known function entry.
Indirect branches (blx rN, bx rN != lr, tbb/tbh) are reported, not followed
(tbb/tbh tables are decoded).  Literal-pool references are classified as
flash (likely constant tables) or RAM (state).
"""
import struct
import sys
from collections import defaultdict

from capstone import CS_ARCH_ARM, CS_MODE_THUMB, Cs
from capstone.arm import ARM_OP_IMM, ARM_OP_MEM, ARM_REG_PC

IMG = sys.argv[1] if len(sys.argv) > 1 else "refs/md380tools/firmware/unwrapped/D002.032.img"
BASE = int(sys.argv[2], 0) if len(sys.argv) > 2 else 0x0800C000
ENTRIES = {"ambe_encode_thing": 0x08050D90, "ambe_decode_wav": 0x08051248, "ambe_unpack": 0x08048C9C}

data = open(IMG, "rb").read()
END = BASE + len(data)
md = Cs(CS_ARCH_ARM, CS_MODE_THUMB)
md.detail = True


def insn_at(addr):
    off = addr - BASE
    if not (0 <= off < len(data) - 1):
        return None
    for i in md.disasm(data[off:off + 4], addr, 1):
        return i
    return None


def u32(addr):
    return struct.unpack_from("<I", data, addr - BASE)[0]


funcs = {}          # entry -> dict(blocks=set(addr), size=int, calls=set, indirect=list, lits=set)
queue = list(ENTRIES.values())
names = {v: k for k, v in ENTRIES.items()}


def explore(entry):
    seen, work = set(), [entry]
    calls, indirect, lits, size = set(), [], set(), 0
    while work:
        pc = work.pop()
        while pc not in seen:
            i = insn_at(pc)
            if i is None:
                indirect.append((pc, "undecodable"))
                break
            seen.add(pc)
            size += i.size
            m, ops = i.mnemonic, i.operands
            nxt = pc + i.size
            # literal-pool loads: ldr rX, [pc, #imm]
            for op in ops:
                if op.type == ARM_OP_MEM and op.mem.base == ARM_REG_PC:
                    la = ((pc + 4) & ~3) + op.mem.disp
                    if BASE <= la < END - 3:
                        lits.add(u32(la))
            if m in ("bl", "blx") and ops and ops[0].type == ARM_OP_IMM:
                calls.add(ops[0].imm & ~1)
                pc = nxt
                continue
            if m in ("blx",) or (m == "bx" and i.op_str != "lr"):
                indirect.append((pc, f"{m} {i.op_str}"))
                if m == "blx":
                    pc = nxt
                    continue
                break
            if m == "bx" and i.op_str == "lr":
                break
            if m.startswith("pop") and "pc" in i.op_str:
                break
            if m.startswith("ldm") and "pc" in i.op_str:
                break
            if m.startswith("ldr") and i.op_str.startswith("pc"):
                indirect.append((pc, f"{m} {i.op_str}"))
                break
            if m in ("tbb", "tbh"):
                # table follows the instruction; read until an entry points back into the table
                tab = nxt
                width = 1 if m == "tbb" else 2
                k, tgts, tab_end = 0, [], None
                while k < 256:
                    ea = tab + k * width
                    if tab_end is not None and ea >= tab_end:
                        break
                    v = data[ea - BASE] if width == 1 else struct.unpack_from("<H", data, ea - BASE)[0]
                    t = tab + 2 * v
                    tgts.append(t)
                    tab_end = t if tab_end is None else min(tab_end, t)
                    k += 1
                work.extend(tgts)
                break
            if m in ("b", "b.w") or (m.startswith("b") and len(m) in (3, 5) and m not in ("bic", "bfi", "bfc", "bkpt")
                                      and ops and ops[0].type == ARM_OP_IMM):
                tgt = ops[0].imm
                if m in ("b", "b.w"):
                    if tgt in ENTRIES.values() or tgt in funcs:
                        calls.add(tgt)  # tail call
                    else:
                        work.append(tgt)
                    break
                work.append(tgt)  # conditional branch
                pc = nxt
                continue
            if m in ("cbz", "cbnz"):
                work.append(ops[1].imm)
                pc = nxt
                continue
            if i.cc not in (0, 15) and m.startswith("it"):
                pc = nxt
                continue
            pc = nxt
    return dict(blocks=seen, size=size, calls=calls, indirect=indirect, lits=lits)


while queue:
    f = queue.pop()
    if f in funcs or not (BASE <= f < END):
        continue
    funcs[f] = explore(f)
    queue.extend(funcs[f]["calls"])


def reach(root):
    out, st = set(), [root]
    while st:
        f = st.pop()
        if f in out or f not in funcs:
            continue
        out.add(f)
        st.extend(funcs[f]["calls"])
    return out


for name, e in ENTRIES.items():
    r = reach(e)
    print(f"{name} @ {e:#x}: {len(r)} functions, {sum(funcs[f]['size'] for f in r)} bytes of code reachable")
both = reach(ENTRIES["ambe_encode_thing"]) & reach(ENTRIES["ambe_decode_wav"])
print(f"shared between encoder and decoder: {len(both)} functions, {sum(funcs[f]['size'] for f in both)} bytes")

allf = reach(ENTRIES["ambe_encode_thing"]) | reach(ENTRIES["ambe_decode_wav"]) | reach(ENTRIES["ambe_unpack"])
print(f"union: {len(allf)} functions, {sum(funcs[f]['size'] for f in allf)} bytes, address span "
      f"{min(allf):#x}..{max(allf):#x}")
ind = [(hex(f), x) for f in sorted(allf) for x in funcs[f]["indirect"]]
print(f"indirect/unfollowed control flow sites: {len(ind)}")
for f, (pc, what) in ind[:20]:
    print(f"   in {f} at {pc:#x}: {what}")
flash_lits = sorted({v for f in allf for v in funcs[f]["lits"] if BASE <= v < END and not (v & 1 and v in funcs)})
ram_lits = sorted({v for f in allf for v in funcs[f]["lits"] if 0x10000000 <= v < 0x10020000 or 0x20000000 <= v < 0x20020000})
print(f"literal refs into flash (candidate constant tables): {len(flash_lits)}")
print("   " + " ".join(hex(v) for v in flash_lits[:80]))
print(f"literal refs into RAM/TCRAM (state/buffers): {len(ram_lits)}")
print("   " + " ".join(hex(v) for v in ram_lits[:80]))
print("\nper-function (entry, size, #calls) for union, sorted by address:")
for f in sorted(allf):
    tag = names.get(f, "")
    print(f"   {f:#010x} {funcs[f]['size']:6d}B calls={len(funcs[f]['calls']):2d} {tag}")
