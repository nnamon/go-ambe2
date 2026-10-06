"""Parse the AMBE+2 3600x2450 codebooks out of mbelib's ambe3600x2450_const.h (ISC)."""
import os
import re

import numpy as np

HDR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "refs/mbelib/ambe3600x2450_const.h")


def load(path=HDR):
    s = re.sub(r"/\*.*?\*/", "", open(path).read(), flags=re.S)
    out = {}
    for m in re.finditer(r"const\s+(float|int)\s+(\w+)\s*((?:\[\d+\])+)\s*=\s*\{(.*?)\};", s, re.S):
        typ, name, dims, body = m.groups()
        shape = [int(d) for d in re.findall(r"\[(\d+)\]", dims)]
        nums = [float(x) for x in re.findall(r"-?\d+\.?\d*(?:[eE][-+]?\d+)?", body)]
        arr = np.array(nums, dtype=float if typ == "float" else int)
        assert arr.size == int(np.prod(shape)), (name, arr.size, shape)
        out[name] = arr.reshape(shape)
    return out


if __name__ == "__main__":
    for k, v in load().items():
        print(k, v.shape, v.dtype)
