// Optional Incus Fleet host eBPF collector.
// Build on Linux with clang/LLVM and libbpf headers:
//   make -C agent/bpf
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u64);
} RETRANSMITS SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u64);
} OOM_KILLS SEC(".maps");

SEC("tracepoint/tcp/tcp_retransmit_skb")
int tcp_retransmit(void *ctx)
{
    __u32 key = 0;
    __u64 *value = bpf_map_lookup_elem(&RETRANSMITS, &key);
    if (value)
        __sync_fetch_and_add(value, 1);
    return 0;
}

SEC("tracepoint/oom/mark_victim")
int oom_mark(void *ctx)
{
    __u32 key = 0;
    __u64 *value = bpf_map_lookup_elem(&OOM_KILLS, &key);
    if (value)
        __sync_fetch_and_add(value, 1);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
