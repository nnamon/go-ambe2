#!/usr/bin/env python3
"""Wire-compatibility check of ambe-server against DVSwitch's md380-emu -S.

Both servers are driven by the same small UDP client (md380-emu's protocol:
320-byte PCM -> 7-byte frame, 7-byte frame -> 320-byte PCM).  For each
recording this checks reply sizes and the byte-6 convention, compares each
server's frames with the offline tools' output (go.amb from mbevoc-enc,
fw.amb from the firmware oracle), cross-decodes every stream on both servers
and scores the results, and times request round trips.

  server_compat.py [SET]            (run from research/, after setup.sh)

md380-emu (oracle/dvswitch-md380-emu, image "dvswitch-md380-emu") has one
shared vocoder state, so its container is restarted before every stream to
start each from a fresh state, like the offline tools.  ambe-server runs in
per-client mode, so each stream simply uses a new client socket.
"""
import argparse
import glob
import os
import socket
import subprocess
import sys
import time

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ambe_eval import estimate_delay, read_frames, read_pcm  # noqa: E402
from pesq import pesq  # noqa: E402
from pystoi import stoi  # noqa: E402


class Client:
    def __init__(self, addr):
        host, port = addr.rsplit(":", 1)
        self.addr = (host, int(port))
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sock.settimeout(2.0)
        self.rtt = []

    def ask(self, payload, want):
        t0 = time.perf_counter()
        self.sock.sendto(payload, self.addr)
        data, _ = self.sock.recvfrom(2048)
        self.rtt.append(time.perf_counter() - t0)
        if len(data) != want:
            raise SystemExit(f"{self.addr}: {len(data)}-byte reply, expected {want}")
        return data

    def encode(self, pcm):
        return [self.ask(pcm[i:i + 320], 7) for i in range(0, len(pcm) - 319, 320)]

    def decode(self, frames):
        return b"".join(self.ask(f, 320) for f in frames)


def amb_to_7(path):
    """The offline .amb frames in the 7-byte wire form."""
    out = []
    for bits in read_frames(path):
        p = bytearray(7)
        for i in range(48):
            p[i // 8] |= bits[i] << (7 - i % 8)
        p[6] = 0x80 if bits[48] else 0
        out.append(bytes(p))
    return out


def score(ref, pcm):
    deg = np.frombuffer(pcm, "<i2").astype(float)
    lag = estimate_delay(ref, deg)
    d = deg[lag:]
    n = min(len(ref), len(d))
    return pesq(8000, ref[:n].astype(np.int16), d[:n].astype(np.int16), "nb"), stoi(ref[:n], d[:n], 8000)


MD380 = ("127.0.0.1", 2471)
GO = ("127.0.0.1", 2472)


def fresh_md380():
    """(Re)start md380-emu's container and wait until it answers."""
    subprocess.run(["docker", "rm", "-f", "dvs-emu"], capture_output=True)
    subprocess.run(["docker", "run", "-d", "--name", "dvs-emu", "-p", f"{MD380[1]}:2470/udp",
                    "dvswitch-md380-emu"], check=True, capture_output=True)
    time.sleep(1.5)  # any probe request would change its state, so just wait
    return Client(f"{MD380[0]}:{MD380[1]}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("set", nargs="?", default="testdata/heldout")
    a = ap.parse_args()
    server = subprocess.Popen(["bin/ambe-server", "-S", str(GO[1]), "-state", "client"],
                              stderr=subprocess.DEVNULL)
    time.sleep(0.5)
    go = lambda: Client(f"{GO[0]}:{GO[1]}")  # noqa: E731  new socket = new state
    results = {k: [] for k in ("md380>md380", "md380>go", "go>md380", "go>go")}
    rtt_md, rtt_go = [], []
    try:
        for d in sorted(glob.glob(a.set + "/*/")):
            pcm = open(d + "ref.raw", "rb").read()
            ref = read_pcm(d + "ref.raw")
            md = fresh_md380()
            fm = md.encode(pcm)
            g = go()
            fg = g.encode(pcm)
            rtt_md += md.rtt
            rtt_go += g.rtt
            for f in fm + fg:
                if f[6] not in (0x00, 0x80):
                    raise SystemExit(f"byte 6 = {f[6]:#x}")
            same_m = sum(x == y for x, y in zip(fm, amb_to_7(d + "fw.amb")))
            same_g = sum(x == y for x, y in zip(fg, amb_to_7(d + "go.amb")))
            results["md380>md380"].append(score(ref, md.decode(fm)))
            results["md380>go"].append(score(ref, go().decode(fm)))
            results["go>md380"].append(score(ref, fresh_md380().decode(fg)))
            results["go>go"].append(score(ref, go().decode(fg)))
            print(f"{os.path.basename(d.rstrip('/'))}: {len(fm)} frames; md380-emu frames identical to offline firmware "
                  f"{same_m}/{len(fm)}, ambe-server frames identical to offline mbevoc-enc {same_g}/{len(fg)}", flush=True)
    finally:
        server.terminate()
        subprocess.run(["docker", "rm", "-f", "dvs-emu"], capture_output=True)
    print("\nencoded by  > decoded by     PESQ-NB  STOI   (mean over recordings)")
    name = {"md380": "md380-emu", "go": "ambe-server"}
    for k in ("md380>md380", "md380>go", "go>md380", "go>go"):
        v = np.array(results[k])
        enc, dec = k.split(">")
        print(f"  {name[enc]:11s} > {name[dec]:11s}  {v[:, 0].mean():.3f}   {v[:, 1].mean():.3f}")
    for r, label in ((rtt_md, "md380-emu (qemu-user, Docker)"), (rtt_go, "ambe-server")):
        r = np.array(r) * 1e3
        print(f"encode round trip, {label:30s} median {np.median(r):.2f} ms, p99 {np.percentile(r, 99):.2f} ms, "
              f"max {r.max():.2f} ms ({len(r)} requests)")


if __name__ == "__main__":
    main()
