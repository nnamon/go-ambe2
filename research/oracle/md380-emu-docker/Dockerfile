# Black-box AMBE+2 oracle (an external check, not a source of the implementation):
# the Tytera MD-380 firmware D002.032 vocoder, run via qemu-user.
# Build from the research/ directory (refs/ must be real directories, not symlinks,
# since Docker does not follow symlinks out of the build context):
#   docker build -f oracle/md380-emu-docker/Dockerfile -t md380-emu:d02032 .
# Then run it through oracle/md380-emu-docker/ambe-oracle.
# Produces two binaries:
#   /opt/md380/md380-emu    upstream md380tools emulator (+ .amb magic-check fix)
#   /opt/md380/ambe-oracle  our harness (no dropped frames, .bits text format)
FROM debian:bookworm-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      gcc-arm-linux-gnueabi libc6-dev-armel-cross binutils-arm-linux-gnueabi \
      qemu-user make ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /md380tools
COPY refs/md380tools/emulator ./emulator
COPY refs/md380tools/applet/src/symbols_d02.032 ./applet/src/symbols_d02.032
COPY refs/md380tools/cores/d02032-core.img ./cores/d02032-core.img
COPY refs/md380tools/firmware/unwrapped/D002.032.img ./firmware/unwrapped/D002.032.img
COPY oracle/md380-emu-docker/oracle.c ./emulator/oracle.c
WORKDIR /md380tools/emulator
ENV CC="arm-linux-gnueabi-gcc -static -g -O1"
# 1) Skip firmware.o's recursive "make -C ../firmware" (we supply the unwrapped image).
# 2) Upstream bug: .amb magic check sets packed[5]=0 (packed[4] uninitialized) and inverts strcmp.
RUN sed -i 's/^\t"\${MAKE}" -C ..\/firmware$/\t@true/' Makefile \
 && sed -i 's/packed\[5\]=0;/packed[4]=0;/; s/if(!strcmp(packed,".amb")){/if(strcmp((char*)packed,".amb")){/' ambe.c \
 && grep -q 'packed\[4\]=0;' ambe.c \
 && make md380-emu CC="$CC" \
 && $CC -std=gnu99 -o ambe-oracle oracle.c firmware.o ram.o \
      -Xlinker --just-symbols=../applet/src/symbols_d02.032 \
      -Xlinker --section-start=.firmware=0x0800C000 \
      -Xlinker --section-start=.sram=0x20000000 \
 && mkdir -p /opt/md380 && cp md380-emu ambe-oracle /opt/md380/ \
 && printf '#!/bin/sh\nexec qemu-arm /opt/md380/md380-emu "$@"\n' > /usr/local/bin/md380-emu \
 && printf '#!/bin/sh\nexec qemu-arm /opt/md380/ambe-oracle "$@"\n' > /usr/local/bin/ambe-oracle \
 && chmod +x /usr/local/bin/md380-emu /usr/local/bin/ambe-oracle
WORKDIR /w
ENTRYPOINT ["ambe-oracle"]
