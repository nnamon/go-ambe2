#!/bin/sh
# Fetch and build everything the research tooling needs.  None of it is
# committed to the repository: third-party sources, the MD-380 firmware,
# specification PDFs and the speech corpus.  Safe to re-run.
#
# Requires: git, curl, make, a C/C++ compiler, Go, python3 (and pdftotext for
# the spec text used by tools/gen_imbe_windows.py, optional).
set -eu
cd "$(dirname "$0")"
mkdir -p refs papers bin testdata/osr testdata/fec testdata/speech

echo "== reference sources (refs/)"
clone() { [ -d "refs/$2/.git" ] || git clone -q --depth 1 "$1" "refs/$2"; }
clone https://github.com/travisgoodspeed/md380tools.git md380tools
clone https://github.com/szechyjs/mbelib.git mbelib
clone https://github.com/boatbod/op25.git op25
clone https://github.com/lwvmobile/dsd-fme.git dsd-fme
clone https://github.com/g4klx/MMDVMHost.git MMDVMHost
clone https://github.com/DVSwitch/md380tools.git dvswitch-md380tools
clone https://github.com/arancormonk/mbelib-neo.git mbelib-neo   # GPL; research comparisons only

echo "== MD-380 firmware D002.032 (black-box reference; md380tools checks its SHA-256)"
make -s -C refs/md380tools/firmware -f Makefile_orig unwrapped/D002.032.img

echo "== specifications (papers/; optional, only needed to regenerate tables)"
fetch() { [ -s "$2" ] || curl -fsSL --retry 5 --retry-delay 3 --retry-all-errors -o "$2" "$1"; }
fetch "https://archive.org/download/TIA-102_Series_Documents/TIA-102.BABA_Project_25_IMBE_Vocoder_Description.pdf" papers/TIA-102.BABA_IMBE.pdf ||
  echo "warning: could not fetch TIA-102.BABA; tools/gen_imbe_windows.py will not run"
fetch "https://archive.org/download/TIA-102_Series_Documents/TIA-102.BABA-1_P25_Half_Rate_Vocoder_Addendum.pdf" papers/TIA-102.BABA-1_halfrate_draft.pdf ||
  fetch "https://www.qsl.net/kb9mwr/projects/dv/codec/TIA-102.BABA-1%20%20P25%20Half%20Rate%20Vocoder%20Addendum%20.pdf" papers/TIA-102.BABA-1_halfrate_draft.pdf ||
  echo "warning: could not fetch TIA-102.BABA-1"
fetch "https://www.jarl.com/d-star/shogen.pdf" papers/dstar_shogen.pdf ||
  echo "warning: could not fetch the JARL D-STAR system description"
if command -v pdftotext >/dev/null 2>&1; then
  [ -s papers/BABA.txt ] || [ ! -s papers/TIA-102.BABA_IMBE.pdf ] || pdftotext -layout papers/TIA-102.BABA_IMBE.pdf papers/BABA.txt
  [ -s papers/BABA-1.txt ] || [ ! -s papers/TIA-102.BABA-1_halfrate_draft.pdf ] || pdftotext -layout papers/TIA-102.BABA-1_halfrate_draft.pdf papers/BABA-1.txt
fi

echo "== python environment (.venv/)"
[ -x .venv/bin/python ] || python3 -m venv .venv
.venv/bin/pip install -q -r requirements.txt

echo "== native reference tools and Go CLIs (bin/)"
make -s -C tools

echo "== speech corpus (Open Speech Repository, testdata/)"
for f in $(cat corpus/dev.txt corpus/heldout.txt); do
  fetch "https://www.voiptroubleshooter.com/open_speech/american/$f" "testdata/osr/$f"
done
tools/mkcorpus.sh testdata/matrix $(sed 's|^|testdata/osr/|' corpus/dev.txt)
tools/mkcorpus.sh testdata/heldout $(sed 's|^|testdata/osr/|' corpus/heldout.txt)

echo "== cross-check data for the Go tests"
[ -s testdata/fec/mbelib_xcheck.txt ] || bin/fec-xcheck 20000 > testdata/fec/mbelib_xcheck.txt
ref=testdata/matrix/OSR_us_000_0030_8k/ref.raw
[ -s testdata/speech/oracle.amb ] || .venv/bin/python oracle/unicorn/md380_uc.py enc $ref testdata/speech/oracle.amb
[ -s testdata/speech/oracle.bits ] || .venv/bin/python oracle/unicorn/md380_uc.py enc $ref testdata/speech/oracle.bits
mkdir -p testdata/imbe
for n in OSR_us_000_0011_8k OSR_us_000_0032_8k OSR_us_000_0057_8k; do
  r=testdata/heldout/$n/ref.raw o=testdata/imbe/$n
  [ -s $o.goimbe.imb ] || bin/ambe-enc -codec imbe $r $o.goimbe.imb 2>/dev/null
  [ -s $o.op25imbe.imb ] || bin/op25-imbe enc $r $o.op25imbe.imb 2>/dev/null
  for e in goimbe op25imbe; do
    [ -s $o.$e.mbelib.params ] || bin/mbelib-dec -imbe -p $o.$e.imb /dev/null 2> $o.$e.mbelib.params
  done
done
[ -s testdata/imbe/random.imb ] || .venv/bin/python tools/gen_random_frames.py imb testdata/imbe/random.imb 2000 7
[ -s testdata/imbe/random.op25cw.txt ] || bin/op25-imbe cw testdata/imbe/random.imb testdata/imbe/random.op25cw.txt 2>/dev/null
[ -s testdata/imbe/random.imbe144 ] || .venv/bin/python tools/gen_random_frames.py raw18 testdata/imbe/random.imbe144 3000 41
[ -s testdata/imbe/random.imbe144.neo.imb ] || bin/neo-codec p25-params testdata/imbe/random.imbe144 testdata/imbe/random.imbe144.neo.imb 2>/dev/null
mkdir -p testdata/provoice
[ -s testdata/provoice/random.pv ] || .venv/bin/python tools/gen_random_frames.py raw142 testdata/provoice/random.pv 3000 31
[ -s testdata/provoice/random.neo.imb ] || bin/neo-codec pv-params testdata/provoice/random.pv testdata/provoice/random.neo.imb 2>/dev/null
mkdir -p testdata/dstar
[ -s testdata/dstar/frames.dmb ] || .venv/bin/python tools/gen_random_frames.py dmb testdata/dstar/frames.dmb 2000 11
[ -s testdata/dstar/frames.neo.dv ] || bin/neo-codec dstar-dv testdata/dstar/frames.dmb testdata/dstar/frames.neo.dv 2>/dev/null
for n in OSR_us_000_0011_8k OSR_us_000_0032_8k OSR_us_000_0057_8k; do
  o=testdata/dstar/$n.godstar
  [ -s $o.dmb ] || bin/ambe-enc -codec dstar testdata/heldout/$n/ref.raw $o.dmb 2>/dev/null
  [ -s $o.mbelib.params ] || bin/mbelib-dec -dstar -p $o.dmb /dev/null 2> $o.mbelib.params
done
echo "done"
