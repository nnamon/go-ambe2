# DVSwitch's md380-emu (fork of md380tools with the "-S <port>" UDP server
# that bridges use), built as-is to check ambe-server's wire compatibility.
# Like the other oracle, it runs the MD-380 firmware D002.032 vocoder under
# qemu-user and is an external check only.
#
# Build from the research/ directory after setup.sh (needs
# refs/dvswitch-md380tools and the unwrapped firmware in refs/md380tools):
#   docker build -f oracle/dvswitch-md380-emu/Dockerfile -t dvswitch-md380-emu .
#   docker run --rm -p 2471:2470/udp dvswitch-md380-emu
FROM debian:bookworm-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      gcc-arm-linux-gnueabihf libc6-dev-armhf-cross binutils-arm-linux-gnueabihf \
      qemu-user make ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /md380tools
COPY refs/dvswitch-md380tools/emulator ./emulator
COPY refs/dvswitch-md380tools/applet/src/symbols_d02.032 ./applet/src/symbols_d02.032
COPY refs/dvswitch-md380tools/cores/d02032-core.img ./cores/d02032-core.img
COPY refs/md380tools/firmware/unwrapped/D002.032.img ./firmware/unwrapped/D002.032.img
WORKDIR /md380tools/emulator
# 1) Skip firmware.o's recursive "make -C ../firmware" (the unwrapped image is supplied).
# 2) The fork's server mode jumps into the linked firmware without making it
#    executable (upstream's file modes call mprotect first); with the
#    non-executable data pages of current kernels and qemu it segfaults on the
#    first request (cf. md380tools issue #925).  Add the same mprotect call
#    before the server loop; the protocol code is left untouched.
RUN sed -i 's/^\t"\${MAKE}" -C ..\/firmware$/\t@true/' Makefile \
 && sed -i 's|^    ambeServer(serverPort);|    mprotect((void*)0x0800c000, 0xf2c00, PROT_READ\|PROT_WRITE\|PROT_EXEC);\n    ambeServer(serverPort);|' md380-emu.c \
 && grep -q "mprotect((void\*)0x0800c000" md380-emu.c \
 && make md380-emu \
 && test -x md380-emu
EXPOSE 2470/udp
ENTRYPOINT ["qemu-arm", "/md380tools/emulator/md380-emu", "-S", "2470"]
