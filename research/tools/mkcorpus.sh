#!/bin/sh
# Build a corpus set: tools/mkcorpus.sh <setdir> file.wav|file.raw ...   (run from research/)
# Creates <setdir>/<name>/ with ref.raw plus the MD-380 firmware (external
# reference, via oracle/unicorn) and OP25 baselines, each decoded by the
# firmware decoder and mbelib.  Inputs must be 8 kHz mono 16-bit.
set -eu
set_dir=$1; shift
PY=.venv/bin/python
for src in "$@"; do
  name=$(basename "$src"); name=${name%.*}
  d="$set_dir/$name"; mkdir -p "$d"
  case "$src" in
    *.wav) $PY - "$src" "$d/ref.raw" <<'PYEOF'
import sys, wave
w = wave.open(sys.argv[1])
if (w.getnchannels(), w.getsampwidth(), w.getframerate()) != (1, 2, 8000):
    raise SystemExit(f"{sys.argv[1]}: need 8 kHz mono 16-bit, got {w.getparams()}")
open(sys.argv[2], "wb").write(w.readframes(w.getnframes()))
PYEOF
    ;;
    *) cp "$src" "$d/ref.raw" ;;
  esac
  [ -f "$d/fw.amb" ] || $PY oracle/unicorn/md380_uc.py enc "$d/ref.raw" "$d/fw.amb" 2>/dev/null
  [ -f "$d/op25.amb" ] || bin/op25-enc "$d/ref.raw" "$d/op25.amb" 2>/dev/null
  for e in fw op25; do
    [ -f "$d/$e.fw.raw" ] || $PY oracle/unicorn/md380_uc.py dec "$d/$e.amb" "$d/$e.fw.raw" 2>/dev/null
    [ -f "$d/$e.mbelib.raw" ] || bin/mbelib-dec "$d/$e.amb" "$d/$e.mbelib.raw" 2>/dev/null
    [ -f "$d/$e.mbelib.params" ] || bin/mbelib-dec -p "$d/$e.amb" /dev/null 2> "$d/$e.mbelib.params"
  done
  echo "$d: $(( $(wc -c < "$d/ref.raw") / 320 )) frames"
done
