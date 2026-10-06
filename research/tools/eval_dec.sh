#!/bin/sh
# Score the Go decoder (run from the ambe/ workspace root):
#   tools/eval_dec.sh [dectag] [encoder tags...]
# Decodes <dir>/<enc>.amb with bin/ambe-dec (extra flags via $DECFLAGS) into <enc>.<dectag>.raw for each
# corpus dir, then compares decoders fw (MD-380 firmware, external reference),
# mbelib and the Go decoder via tools/score_dec.py.
set -eu
dectag=${1:-godec}; [ $# -gt 0 ] && shift
encs=${*:-fw go op25}
for d in ${SET:-testdata/matrix}/*/; do
  d=${d%/}
  for e in $encs; do
    [ -f "$d/$e.amb" ] && bin/ambe-dec ${DECFLAGS:-} "$d/$e.amb" "$d/$e.$dectag.raw" 2>/dev/null
  done
done
.venv/bin/python tools/score_dec.py "$dectag" $encs
