# Native reference tools built from the sources cloned into ../refs by
# ../setup.sh, plus the Go command-line tools of this module.  Run: make -C tools
REFS   ?= ../refs
MBELIB := $(REFS)/mbelib
OP25   := $(REFS)/op25/op25/gr-op25_repeater/lib
DSDFME := $(REFS)/dsd-fme
BIN    := ../bin
CFLAGS   ?= -O2 -g -w
CXXFLAGS ?= -O2 -g -w -std=c++17
MBELIB_SRC := $(MBELIB)/mbelib.c $(MBELIB)/ambe3600x2450.c $(MBELIB)/ambe3600x2400.c \
              $(MBELIB)/imbe7200x4400.c $(MBELIB)/imbe7100x4400.c $(MBELIB)/ecc.c

all: go $(BIN)/mbelib-dec $(BIN)/op25-enc $(BIN)/fec-xcheck

# Go CLIs of this module (always rebuilt; go build is incremental).
go:
	mkdir -p $(BIN)
	cd ../.. && go build -o research/bin/ambe-enc ./cmd/ambe-enc && \
	  go build -o research/bin/ambe-dec ./cmd/ambe-dec && \
	  go build -o research/bin/ambe-params ./cmd/ambe-params

$(BIN)/mbelib-dec: mbelib-dec/main.c common_amb.h
	mkdir -p $(BIN)
	$(CC) $(CFLAGS) -I$(MBELIB) -o $@ mbelib-dec/main.c $(MBELIB_SRC) -lm

OP25_CXX := $(OP25)/ambe_encoder.cc $(OP25)/p25p2_vf.cc $(OP25)/rs.cc $(wildcard $(OP25)/imbe_vocoder/*.cc)
OP25_C   := $(OP25)/ambe.c $(OP25)/mbelib.c

$(BIN)/op25-enc: op25-enc/main.cc common_amb.h
	mkdir -p $(BIN) build/op25
	for f in $(OP25_C); do $(CC) $(CFLAGS) -I$(OP25) -c $$f -o build/op25/$$(basename $$f).o || exit 1; done
	$(CXX) $(CXXFLAGS) -I$(OP25) -o $@ op25-enc/main.cc $(OP25_CXX) build/op25/*.o -lm

fec_xcheck/rW.inc: fec_xcheck/gen_inc.py
	python3 fec_xcheck/gen_inc.py $(DSDFME)/include/dmr_const.h fec_xcheck

$(BIN)/fec-xcheck: fec_xcheck/main.c fec_xcheck/rW.inc
	mkdir -p $(BIN)
	$(CC) $(CFLAGS) -I$(MBELIB) -Ifec_xcheck -o $@ fec_xcheck/main.c $(MBELIB_SRC) -lm

clean:
	rm -rf build fec_xcheck/*.inc $(BIN)/mbelib-dec $(BIN)/op25-enc $(BIN)/fec-xcheck \
	  $(BIN)/ambe-enc $(BIN)/ambe-dec $(BIN)/ambe-params
.PHONY: all go clean
