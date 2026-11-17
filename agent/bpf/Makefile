# Build the optional eBPF object on Linux.
# Requires clang with the BPF target plus <bpf/bpf_helpers.h> (libbpf-dev).
CLANG ?= clang
ARCH ?= $(shell uname -m | sed 's/x86_64/x86/;s/aarch64/arm64/')
MULTIARCH ?= $(shell gcc -print-multiarch 2>/dev/null)
BPF_CFLAGS := -O2 -g -target bpf -D__TARGET_ARCH_$(ARCH)
ifneq ($(MULTIARCH),)
BPF_CFLAGS += -I/usr/include/$(MULTIARCH)
endif
TARGET := metrics.bpf.o

all: $(TARGET)

$(TARGET): metrics.bpf.c
	$(CLANG) $(BPF_CFLAGS) -c $< -o $@

clean:
	rm -f $(TARGET)

.PHONY: all clean
