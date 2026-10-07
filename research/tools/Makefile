# Native reference tools built from the sources cloned into ../refs by
# ../setup.sh, plus the Go command-line tools of this module.  Run: make -C tools
REFS   ?= ../refs
MBELIB := $(REFS)/mbelib
OP25   := $(REFS)/op25/op25/gr-op25_repeater/lib
DSDFME := $(REFS)/dsd-fme
NEO    := $(REFS)/mbelib-neo
BIN    := ../bin
CFLAGS   ?= -O2 -g -w
CXXFLAGS ?= -O2 -g -w -std=c++17
MBELIB_SRC := $(MBELIB)/mbelib.c $(MBELIB)/ambe3600x2450.c $(MBELIB)/ambe3600x2400.c \
              $(MBELIB)/imbe7200x4400.c $(MBELIB)/imbe7100x4400.c $(MBELIB)/ecc.c

all: go $(BIN)/mbelib-dec $(BIN)/op25-enc $(BIN)/op25-imbe $(BIN)/neo-codec $(BIN)/fec-xcheck

# Go CLIs of this module (always rebuilt; go build is incremental).
go:
	mkdir -p $(BIN)
	cd ../.. && go build -o research/bin/ambe-enc ./cmd/ambe-enc && \
	  go build -o research/bin/ambe-dec ./cmd/ambe-dec && \
	  go build -o research/bin/ambe-params ./cmd/ambe-params && \
	  go build -o research/bin/ambe-server ./cmd/ambe-server

$(BIN)/mbelib-dec: mbelib-dec/main.c common_amb.h
	mkdir -p $(BIN)
	$(CC) $(CFLAGS) -I$(MBELIB) -o $@ mbelib-dec/main.c $(MBELIB_SRC) -lm

OP25_CXX := $(OP25)/ambe_encoder.cc $(OP25)/p25p2_vf.cc $(OP25)/rs.cc $(wildcard $(OP25)/imbe_vocoder/*.cc)
OP25_C   := $(OP25)/ambe.c $(OP25)/mbelib.c

$(BIN)/op25-enc: op25-enc/main.cc common_amb.h
	mkdir -p $(BIN) build/op25
	for f in $(OP25_C); do $(CC) $(CFLAGS) -I$(OP25) -c $$f -o build/op25/$$(basename $$f).o || exit 1; done
	$(CXX) $(CXXFLAGS) -I$(OP25) -o $@ op25-enc/main.cc $(OP25_CXX) build/op25/*.o -lm

$(BIN)/op25-imbe: op25-imbe/main.cc
	mkdir -p $(BIN)
	$(CXX) $(CXXFLAGS) -I$(OP25) -o $@ op25-imbe/main.cc $(wildcard $(OP25)/imbe_vocoder/*.cc) -lm

# mbelib-neo (GPL), compiled from its sources without CMake; research use only.
NEO_SRC := $(wildcard $(NEO)/src/core/*.c $(NEO)/src/ambe/*.c $(NEO)/src/imbe/*.c $(NEO)/src/ecc/*.c) \
           $(NEO)/src/external/pffft/pffft.c $(NEO)/src/external/pffft/fftpack.c
$(BIN)/neo-codec: neo-codec/main.c
	mkdir -p $(BIN) build/neo/mbelib-neo
	sed 's/@PROJECT_VERSION@/research/' $(NEO)/include/mbelib-neo/version.h.in > build/neo/mbelib-neo/version.h
	$(CC) $(CFLAGS) -DMBE_STATIC=1 -I$(NEO)/include -Ibuild/neo -I$(NEO)/src/internal -I$(NEO)/src/external/pffft \
	  -o $@ neo-codec/main.c $(NEO_SRC) -lm

fec_xcheck/rW.inc: fec_xcheck/gen_inc.py
	python3 fec_xcheck/gen_inc.py $(DSDFME)/include/dmr_const.h fec_xcheck

$(BIN)/fec-xcheck: fec_xcheck/main.c fec_xcheck/rW.inc
	mkdir -p $(BIN)
	$(CC) $(CFLAGS) -I$(MBELIB) -Ifec_xcheck -o $@ fec_xcheck/main.c $(MBELIB_SRC) -lm

clean:
	rm -rf build fec_xcheck/*.inc $(BIN)/mbelib-dec $(BIN)/op25-enc $(BIN)/op25-imbe $(BIN)/neo-codec $(BIN)/fec-xcheck \
	  $(BIN)/ambe-enc $(BIN)/ambe-dec $(BIN)/ambe-params $(BIN)/ambe-server
.PHONY: all go clean
