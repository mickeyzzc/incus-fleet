# Build the optional eBPF object on Linux.
# Requires clang with the BPF target plus <bpf/bpf_helpers.h> (libbpf-dev).
CLANG ?= clang
ARCH ?= $(shell uname -m | sed 's/x86_64/x86/;s/aarch64/arm64/')
TARGET := metrics.bpf.o

all: $(TARGET)

$(TARGET): metrics.bpf.c
	$(CLANG) -O2 -g -target bpf -D__TARGET_ARCH_$(ARCH) -c $< -o $@

clean:
	rm -f $(TARGET)

.PHONY: all clean
