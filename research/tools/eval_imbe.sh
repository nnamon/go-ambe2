#!/bin/sh
# IMBE 7200x4400 (P25 Phase 1) matrix: the Go and OP25 encoders, each decoded
# by the Go decoder, mbelib and OP25 (run from research/ after make -C tools).
#   tools/eval_imbe.sh [go-tag]     (extra mbevoc-dec flags in $DECFLAGS)
set -eu
tag=${1:-goimbe}
for d in ${SET:-testdata/heldout}/*/; do
  d=${d%/}
  [ -f "$d/$tag.imb" ] || bin/mbevoc-enc -codec imbe "$d/ref.raw" "$d/$tag.imb" 2>/dev/null
  [ -f "$d/op25imbe.imb" ] || bin/op25-imbe enc "$d/ref.raw" "$d/op25imbe.imb" 2>/dev/null
  for e in $tag op25imbe; do
    bin/mbevoc-dec -codec imbe ${DECFLAGS:-} "$d/$e.imb" "$d/$e.godec.raw" 2>/dev/null
    [ -f "$d/$e.mbelib.raw" ] || bin/mbelib-dec -g 1 -imbe "$d/$e.imb" "$d/$e.mbelib.raw" 2>/dev/null
    [ -f "$d/$e.op25.raw" ] || bin/op25-imbe dec "$d/$e.imb" "$d/$e.op25.raw" 2>/dev/null
  done
done
.venv/bin/python tools/score_matrix.py "$tag op25imbe" "godec mbelib op25"
