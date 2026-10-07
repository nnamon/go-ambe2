#!/bin/sh
# D-STAR AMBE 3600x2400 matrix: this module's and mbelib-neo's encoders, each
# decoded by this decoder, mbelib and mbelib-neo (run from research/ after
# make -C tools).   tools/eval_dstar.sh [go-tag]   (extra ambe-dec flags in $DECFLAGS)
set -eu
tag=${1:-godstar}
for d in ${SET:-testdata/heldout}/*/; do
  d=${d%/}
  [ -f "$d/$tag.dmb" ] || bin/ambe-enc -codec dstar "$d/ref.raw" "$d/$tag.dmb" 2>/dev/null
  [ -f "$d/neodstar.dmb" ] || bin/neo-codec dstar-enc "$d/ref.raw" "$d/neodstar.dmb" 2>/dev/null
  for e in $tag neodstar; do
    bin/ambe-dec -codec dstar ${DECFLAGS:-} "$d/$e.dmb" "$d/$e.godec.raw" 2>/dev/null
    [ -f "$d/$e.mbelib.raw" ] || bin/mbelib-dec -dstar "$d/$e.dmb" "$d/$e.mbelib.raw" 2>/dev/null
    [ -f "$d/$e.neo.raw" ] || bin/neo-codec dstar-dec "$d/$e.dmb" "$d/$e.neo.raw" 2>/dev/null
  done
done
.venv/bin/python tools/score_matrix.py "$tag neodstar" "godec mbelib neo"
