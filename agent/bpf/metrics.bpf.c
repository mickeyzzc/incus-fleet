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

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, __u64);
    __type(value, __u64);
} CGROUP_RETRANSMITS SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, __u64);
    __type(value, __u64);
} CGROUP_OOM_KILLS SEC(".maps");

struct block_start_value {
    __u64 ts;
    __u64 cgroup_id;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 16384);
    __type(key, __u64);
    __type(value, struct block_start_value);
} BLOCK_START_VALUES SEC(".maps");

struct block_key {
    __u64 cgroup_id;
    __u32 bucket;
    __u32 pad;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 65536);
    __type(key, struct block_key);
    __type(value, __u64);
} BLOCK_LATENCY SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, __u64);
    __type(value, __u64);
} BLOCK_SUM_NS SEC(".maps");

static __always_inline void bump_cgroup(void *map, __u64 cgroup_id)
{
    __u64 *value = bpf_map_lookup_elem(map, &cgroup_id);
    if (value) {
        __sync_fetch_and_add(value, 1);
    } else {
        __u64 one = 1;
        bpf_map_update_elem(map, &cgroup_id, &one, BPF_ANY);
    }
}

static __always_inline void bump_host(void *map)
{
    __u32 key = 0;
    __u64 *value = bpf_map_lookup_elem(map, &key);
    if (value)
        __sync_fetch_and_add(value, 1);
}

SEC("tracepoint/tcp/tcp_retransmit_skb")
int tcp_retransmit(void *ctx)
{
    // Retransmit timers can run in softirq context; the cgroup attribution is
    // best-effort and falls back to the root cgroup when current is not the
    // socket owner. The host counter remains exact.
    __u64 cgroup_id = bpf_get_current_cgroup_id();
    bump_host(&RETRANSMITS);
    bump_cgroup(&CGROUP_RETRANSMITS, cgroup_id);
    return 0;
}

SEC("tracepoint/oom/mark_victim")
int oom_mark(void *ctx)
{
    __u64 cgroup_id = bpf_get_current_cgroup_id();
    bump_host(&OOM_KILLS);
    bump_cgroup(&CGROUP_OOM_KILLS, cgroup_id);
    return 0;
}

SEC("raw_tracepoint/block_rq_issue")
int block_issue(struct bpf_raw_tracepoint_args *ctx)
{
    __u64 rq = ctx->args[0];
    if (!rq)
        return 0;
    struct block_start_value start = {
        .ts = bpf_ktime_get_ns(),
        .cgroup_id = bpf_get_current_cgroup_id(),
    };
    bpf_map_update_elem(&BLOCK_START_VALUES, &rq, &start, BPF_ANY);
    return 0;
}

SEC("raw_tracepoint/block_rq_complete")
int block_complete(struct bpf_raw_tracepoint_args *ctx)
{
    __u64 rq = ctx->args[0];
    if (!rq)
        return 0;
    struct block_start_value *start = bpf_map_lookup_elem(&BLOCK_START_VALUES, &rq);
    if (!start)
        return 0;

    __u64 delta = bpf_ktime_get_ns() - start->ts;
    __u64 cgroup_id = start->cgroup_id;
    bpf_map_delete_elem(&BLOCK_START_VALUES, &rq);

    __u32 bucket = 1;
    if (delta > 0)
        bucket = 64 - __builtin_clzll(delta);
    if (bucket > 30)
        bucket = 30;

    struct block_key key = {
        .cgroup_id = cgroup_id,
        .bucket = bucket,
    };
    __u64 *value = bpf_map_lookup_elem(&BLOCK_LATENCY, &key);
    if (value) {
        __sync_fetch_and_add(value, 1);
    } else {
        __u64 one = 1;
        bpf_map_update_elem(&BLOCK_LATENCY, &key, &one, BPF_ANY);
    }

    __u64 *sum = bpf_map_lookup_elem(&BLOCK_SUM_NS, &cgroup_id);
    if (sum) {
        __sync_fetch_and_add(sum, delta);
    } else {
        bpf_map_update_elem(&BLOCK_SUM_NS, &cgroup_id, &delta, BPF_ANY);
    }
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
