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

// IPv4/IPv6 4-tuple -> cgroup id, populated by sockops in process context so
// retransmit tracepoints can resolve the owning container even when the timer
// runs in softirq context.
struct flow_key {
    union {
        __u32 v4[4];
        __u8 v6[16];
    } saddr;
    union {
        __u32 v4[4];
        __u8 v6[16];
    } daddr;
    __u16 sport;
    __u16 dport;
    __u16 family;
    __u16 pad;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 65536);
    __type(key, struct flow_key);
    __type(value, __u64);
} SOCKET_CGROUPS SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, __u64);
    __type(value, __u64);
} CGROUP_OOM_KILLS SEC(".maps");

struct oom_event {
    __u64 ts;
    __u32 pid;
    __u32 pad;
    __u64 cgroup_id;
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 4096);
} OOM_EVENTS SEC(".maps");

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

SEC("sockops")
int fleet_sockops(struct bpf_sock_ops *skops)
{
    __u32 op = skops->op;
    if (op != BPF_SOCK_OPS_TCP_CONNECT_CB &&
        op != BPF_SOCK_OPS_PASSIVE_ESTABLISHED_CB)
        return 0;

    __u32 family = skops->family;
    if (family != 2 && family != 10)
        return 0;
    struct bpf_sock *sk = skops->sk;
    if (!sk)
        return 0;
    struct flow_key key = {};
    if (family == 2) {
        key.saddr.v4[0] = sk->src_ip4;
        key.daddr.v4[0] = sk->dst_ip4;
    } else {
        __builtin_memcpy(key.saddr.v6, sk->src_ip6, 16);
        __builtin_memcpy(key.daddr.v6, sk->dst_ip6, 16);
    }
    key.sport = (__u16)sk->src_port;
    key.dport = __builtin_bswap16(sk->dst_port);
    key.family = (__u16)family;
    __u64 cgroup_id = bpf_get_current_cgroup_id();
    if (cgroup_id)
        bpf_map_update_elem(&SOCKET_CGROUPS, &key, &cgroup_id, BPF_NOEXIST);
    return 0;
}

struct tcp_retransmit_ctx {
    __u64 common;
    void *skb;
    void *sk;
    int state;
    __u16 sport;
    __u16 dport;
    __u16 family;
    __u8 saddr[4];
    __u8 daddr[4];
    __u8 saddr_v6[16];
    __u8 daddr_v6[16];
};

SEC("tracepoint/tcp/tcp_retransmit_skb")
int tcp_retransmit(void *raw_ctx)
{
    struct tcp_retransmit_ctx *ctx = raw_ctx;
    __u64 cgroup_id = 0;
    if (ctx->family == 2 || ctx->family == 10) {
        struct flow_key key = {};
        if (ctx->family == 2) {
            __builtin_memcpy(key.saddr.v4, ctx->saddr, 4);
            __builtin_memcpy(key.daddr.v4, ctx->daddr, 4);
        } else {
            __builtin_memcpy(key.saddr.v6, ctx->saddr_v6, 16);
            __builtin_memcpy(key.daddr.v6, ctx->daddr_v6, 16);
        }
        key.sport = ctx->sport;
        key.dport = ctx->dport;
        key.family = ctx->family;
        __u64 *mapped = bpf_map_lookup_elem(&SOCKET_CGROUPS, &key);
        if (mapped)
            cgroup_id = *mapped;
    }
    if (!cgroup_id)
        cgroup_id = bpf_get_current_cgroup_id();
    bump_host(&RETRANSMITS);
    bump_cgroup(&CGROUP_RETRANSMITS, cgroup_id);
    return 0;
}

struct oom_mark_ctx {
    __u64 common;
    int pid;
};

SEC("tracepoint/oom/mark_victim")
int oom_mark(void *raw_ctx)
{
    __u64 cgroup_id = bpf_get_current_cgroup_id();
    bump_host(&OOM_KILLS);
    bump_cgroup(&CGROUP_OOM_KILLS, cgroup_id);

    // Userspace resolves this pid's cgroup when the OOM context is the root
    // cgroup (for example a host-triggered global OOM that kills a container
    // task). The BPF map above already covers container-memcg OOM exactly.
    struct oom_mark_ctx *ctx = raw_ctx;
    struct oom_event *event = bpf_ringbuf_reserve(&OOM_EVENTS, sizeof(*event), 0);
    if (event) {
        event->ts = bpf_ktime_get_ns();
        event->pid = ctx->pid;
        event->cgroup_id = cgroup_id;
        bpf_ringbuf_submit(event, 0);
    }
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
