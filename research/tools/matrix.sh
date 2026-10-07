#!/bin/sh
# Encoder x decoder cross-matrix for one 8 kHz s16le input.
#   tools/matrix.sh testdata/foo/bar.raw      (run from the ambe/ workspace root)
# Writes into testdata/matrix/<name>/:
#   fw.amb / op25.amb                     firmware (oracle) vs OP25 encodings
#   {fw,op25}.{fw,mbelib}.raw              each bitstream decoded by each decoder
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
PATH="$ROOT/bin:$ROOT/oracle/md380-emu-docker:$PATH"
in=$1
name=$(basename "$in" .raw)
out="testdata/matrix/$name"
mkdir -p "$out"
cp "$in" "$out/ref.raw"
cd "$out"
ambe-oracle enc ref.raw fw.amb
op25-enc ref.raw op25.amb
for e in fw op25; do
  ambe-oracle dec $e.amb $e.fw.raw
  mbelib-dec -g 1 $e.amb $e.mbelib.raw
done
