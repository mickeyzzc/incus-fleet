// Optional Incus Fleet host eBPF collector.
// Build on Linux with clang/LLVM and libbpf headers:
//   make -C agent/bpf
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Minimal CO-RE definitions for resolving an OOM victim's cgroup. Keeping these
// partial (with preserve_access_index) avoids shipping a full vmlinux.h while
// still letting the loader relocate cgroup->kn->id against the running kernel.
struct css_set {
    struct cgroup *dfl_cgrp;
} __attribute__((preserve_access_index));
struct task_struct {
    struct css_set *cgroups;
} __attribute__((preserve_access_index));
struct kernfs_node {
    __u64 id;
} __attribute__((preserve_access_index));
struct cgroup {
    struct kernfs_node *kn;
} __attribute__((preserve_access_index));

extern struct task_struct *bpf_task_from_pid(__s32 pid) __ksym __weak;
extern void bpf_task_release(struct task_struct *p) __ksym __weak;

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

// [0] exact victim cgroup, [1] fallback to current task,
// [2] task_from_pid returned NULL, [3] css_set was NULL,
// [4] css_set.dfl_cgrp was NULL, [5] resolved cgroup id was zero.
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 6);
    __type(key, __u32);
    __type(value, __u64);
} OOM_STATS SEC(".maps");

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

static __always_inline void bump_oom_stat(__u32 key)
{
    __u64 *value = bpf_map_lookup_elem(&OOM_STATS, &key);
    if (value)
        __sync_fetch_and_add(value, 1);
}

static __always_inline void emit_oom(__u64 cgroup_id, int pid)
{
    struct oom_event *event = bpf_ringbuf_reserve(&OOM_EVENTS, sizeof(*event), 0);
    if (event) {
        event->ts = bpf_ktime_get_ns();
        event->pid = pid;
        event->cgroup_id = cgroup_id;
        bpf_ringbuf_submit(event, 0);
    }
}

// Resolve the victim task's cgroup in kernel context. This is exact for both
// memcg OOM and root-context global OOM, where bpf_get_current_cgroup_id()
// refers to the OOM killer rather than the victim.
static __always_inline __u64 victim_cgroup_id(int pid)
{
    struct task_struct *task = bpf_task_from_pid(pid);
    if (!task) {
        bump_oom_stat(2);
        return 0;
    }
    __u64 cgroup_id = 0;
    struct css_set *cgroups = BPF_CORE_READ(task, cgroups);
    if (!cgroups) {
        bump_oom_stat(3);
    } else {
        struct cgroup *cgrp = BPF_CORE_READ(cgroups, dfl_cgrp);
        if (!cgrp) {
            bump_oom_stat(4);
        } else {
            cgroup_id = BPF_CORE_READ(cgrp, kn, id);
            if (!cgroup_id)
                bump_oom_stat(5);
        }
    }
    bpf_task_release(task);
    return cgroup_id;
}

SEC("tracepoint/oom/mark_victim")
int oom_mark_exact(void *raw_ctx)
{
    struct oom_mark_ctx *ctx = raw_ctx;
    __u64 cgroup_id = victim_cgroup_id(ctx->pid);
    if (cgroup_id) {
        bump_oom_stat(0);
    } else {
        cgroup_id = bpf_get_current_cgroup_id();
        bump_oom_stat(1);
    }
    bump_host(&OOM_KILLS);
    bump_cgroup(&CGROUP_OOM_KILLS, cgroup_id);
    emit_oom(cgroup_id, ctx->pid);
    return 0;
}

// Fallback for kernels without the task/cgroup kfuncs. Userspace still tries
// /proc/<pid>/cgroup from the ring-buffer event when the cgroup is not an Incus
// instance, but that path is racy once the victim exits.
SEC("tracepoint/oom/mark_victim")
int oom_mark(void *raw_ctx)
{
    struct oom_mark_ctx *ctx = raw_ctx;
    __u64 cgroup_id = bpf_get_current_cgroup_id();
    bump_oom_stat(1);
    bump_host(&OOM_KILLS);
    bump_cgroup(&CGROUP_OOM_KILLS, cgroup_id);
    emit_oom(cgroup_id, ctx->pid);
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
