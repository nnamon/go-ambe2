#!/bin/sh
# Score the Go encoder on the corpus (run from the ambe/ workspace root):
#   tools/eval_go.sh [tag] [ambe-enc flags...]
# For each testdata/matrix/<name>/ref.raw: encode with bin/ambe-enc -> <tag>.amb,
# decode with the MD380 firmware decoder (external black-box reference, via
# oracle/unicorn) and with mbelib, then print PESQ/STOI and field agreement
# with the firmware encoder's own bitstream (fw.amb).
set -eu
tag=${1:-go}; [ $# -gt 0 ] && shift
PY=.venv/bin/python
for d in ${SET:-testdata/matrix}/*/; do
  d=${d%/}
  bin/ambe-enc "$@" "$d/ref.raw" "$d/$tag.amb" 2>/dev/null
  $PY oracle/unicorn/md380_uc.py dec "$d/$tag.amb" "$d/$tag.fw.raw" 2>/dev/null
  bin/mbelib-dec "$d/$tag.amb" "$d/$tag.mbelib.raw" 2>/dev/null
done
$PY tools/score.py "$tag"
