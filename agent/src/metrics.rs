use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::Instant;

use tracing::warn;

use crate::cgroup;
use crate::ebpf;
use crate::histogram::Histogram;
use crate::node_metrics::NodeMetrics;
use crate::proto::v1::{MetricType, Sample, Series, TelemetryBatch};

/// Collector keeps the previous CPU counters needed for a rate.
pub struct Collector {
    started: Instant,
    previous_cpu: Option<(u64, u64)>,
    previous_cores: Option<Vec<(u64, u64)>>,
    ebpf: Option<ebpf::Collector>,
    mode: &'static str,
    cgroup_root: PathBuf,
    collect_histogram: Histogram,
    node_compat: bool,
}

impl Collector {
    pub fn new() -> Self {
        Self {
            started: Instant::now(),
            previous_cpu: None,
            previous_cores: None,
            ebpf: None,
            mode: "procfs",
            cgroup_root: PathBuf::from("/sys/fs/cgroup"),
            collect_histogram: Histogram::default(),
            node_compat: node_compat_enabled(),
        }
    }

    /// Overrides the node-exporter compatibility view (tests).
    #[cfg_attr(not(test), allow(dead_code))]
    pub fn with_node_compat(mut self, enabled: bool) -> Self {
        self.node_compat = enabled;
        self
    }

    /// Try to attach an eBPF object; failures fall back to procfs/cgroup.
    pub fn with_ebpf(object: Option<&Path>) -> Self {
        let mut collector = Self::new();
        if let Some(path) = object
            && ebpf::available(Some(path))
        {
            match ebpf::Collector::open(path) {
                Ok(loader) => {
                    collector.ebpf = Some(loader);
                    collector.mode = "ebpf";
                }
                Err(err) => {
                    warn!(error = %err, "eBPF collector unavailable; using procfs fallback")
                }
            }
        }
        collector
    }

    /// Collect one 10-second-friendly sample batch.
    pub fn collect(&mut self, agent_id: &str) -> TelemetryBatch {
        let started = Instant::now();
        let now = now_ms();
        let mut series = Vec::new();

        let mut cpu_usage: Option<f64> = None;
        let mut memory_pair: Option<(u64, u64)> = None;

        if let Some((idle, total)) = read_cpu() {
            let usage = match self.previous_cpu {
                Some((prev_idle, prev_total)) if total > prev_total && idle >= prev_idle => {
                    let total_delta = (total - prev_total) as f64;
                    let idle_delta = (idle - prev_idle) as f64;
                    ((total_delta - idle_delta) / total_delta).clamp(0.0, 1.0)
                }
                _ => 0.0,
            };
            self.previous_cpu = Some((idle, total));
            cpu_usage = Some(usage);
            series.push(gauge(
                "fleet_host_cpu_usage_ratio",
                HashMap::new(),
                now,
                usage,
            ));
        }

        let per_cpu_usage = collect_per_core(&mut self.previous_cores, &mut series, now);

        if let Some((total, available)) = read_memory() {
            memory_pair = Some((total, available));
            series.push(gauge(
                "fleet_host_memory_bytes",
                labels(&[("state", "total")]),
                now,
                total as f64,
            ));
            series.push(gauge(
                "fleet_host_memory_bytes",
                labels(&[("state", "available")]),
                now,
                available as f64,
            ));
            series.push(gauge(
                "fleet_host_memory_bytes",
                labels(&[("state", "used")]),
                now,
                total.saturating_sub(available) as f64,
            ));
        }

        series.push(gauge(
            "fleet_host_load",
            labels(&[("window", "1")]),
            now,
            read_load(),
        ));
        series.push(gauge(
            "fleet_host_uptime_seconds",
            HashMap::new(),
            now,
            self.started.elapsed().as_secs_f64(),
        ));

        let cgroup_series = cgroup::collect(&self.cgroup_root, now);
        series.extend(cgroup_series);

        let ebpf_snapshot = self.ebpf.as_ref().map(|collector| collector.collect());
        if let Some(snapshot) = ebpf_snapshot {
            series.extend(snapshot.series(now));
            series.push(counter(
                "fleet_host_tcp_retransmits_total",
                HashMap::new(),
                now,
                snapshot.tcp_retransmits as f64,
            ));
            series.push(counter(
                "fleet_host_oom_kills_total",
                HashMap::new(),
                now,
                snapshot.oom_kills as f64,
            ));
        } else {
            if let Some(value) = proc_tcp_retransmits() {
                series.push(counter(
                    "fleet_host_tcp_retransmits_total",
                    HashMap::new(),
                    now,
                    value as f64,
                ));
            }
            if let Some(value) = proc_oom_kills() {
                series.push(counter(
                    "fleet_host_oom_kills_total",
                    HashMap::new(),
                    now,
                    value as f64,
                ));
            }
        }
        series.push(gauge(
            "fleet_agent_telemetry_mode",
            labels(&[("mode", self.mode)]),
            now,
            1.0,
        ));

        // Standard node-exporter baselines: filesystem capacity, disk IOPS,
        // TCP connection states, and process counts.
        let filesystems = read_filesystems();
        for fs in &filesystems {
            series.push(gauge(
                "fleet_host_fs_bytes",
                labels(&[("mountpoint", &fs.mountpoint), ("state", "total")]),
                now,
                fs.total_bytes as f64,
            ));
            series.push(gauge(
                "fleet_host_fs_bytes",
                labels(&[("mountpoint", &fs.mountpoint), ("state", "available")]),
                now,
                fs.available_bytes as f64,
            ));
        }
        for (device, reads, writes) in read_diskstats() {
            series.push(counter(
                "fleet_host_disk_ops_total",
                labels(&[("device", &device), ("op", "read")]),
                now,
                reads as f64,
            ));
            series.push(counter(
                "fleet_host_disk_ops_total",
                labels(&[("device", &device), ("op", "write")]),
                now,
                writes as f64,
            ));
        }
        let procs = read_process_counts();
        if let Some(running) = procs.running {
            series.push(gauge(
                "fleet_host_procs",
                labels(&[("state", "running")]),
                now,
                running as f64,
            ));
        }
        if let Some(blocked) = procs.blocked {
            series.push(gauge(
                "fleet_host_procs",
                labels(&[("state", "blocked")]),
                now,
                blocked as f64,
            ));
        }
        let tcp_established = read_tcp_established();
        if let Some(established) = tcp_established {
            series.push(gauge(
                "fleet_host_tcp_established",
                HashMap::new(),
                now,
                established as f64,
            ));
        }

        for (device, rx, tx) in read_network() {
            series.push(counter(
                "fleet_host_network_bytes_total",
                labels(&[("device", &device), ("direction", "rx")]),
                now,
                rx as f64,
            ));
            series.push(counter(
                "fleet_host_network_bytes_total",
                labels(&[("device", &device), ("direction", "tx")]),
                now,
                tx as f64,
            ));
        }

        // node-exporter compatible view, derived from the readings above so no
        // host counter is probed twice. The compatibility switch exists for
        // deployments that only want the native namespace.
        if self.node_compat {
            let node_view = NodeMetrics {
                now_ms: now,
                cpu_usage_ratio: cpu_usage,
                per_cpu_usage,
                memory_total: memory_pair.map(|(total, _)| total),
                memory_available: memory_pair.map(|(_, available)| available),
                load1: Some(read_load()),
                uptime_seconds: self.started.elapsed().as_secs_f64(),
                network: read_network(),
                tcp_retransmits: proc_tcp_retransmits(),
                oom_kills: proc_oom_kills(),
                filesystems,
                disks: read_diskstats(),
                procs: procs.clone(),
                tcp_established,
            };
            series.extend(node_view.series());
        }

        self.collect_histogram
            .observe(started.elapsed().as_secs_f64());
        series.extend(self.collect_histogram.series(
            "fleet_agent_collect_duration_seconds",
            &HashMap::new(),
            now,
        ));

        TelemetryBatch {
            agent_id: agent_id.to_string(),
            interval_ms: 10_000,
            series,
        }
    }
}

fn gauge(name: &str, labels: HashMap<String, String>, at: i64, value: f64) -> Series {
    Series {
        name: name.into(),
        labels,
        r#type: MetricType::Gauge as i32,
        samples: vec![Sample { at_ms: at, value }],
    }
}

fn counter(name: &str, labels: HashMap<String, String>, at: i64, value: f64) -> Series {
    Series {
        name: name.into(),
        labels,
        r#type: MetricType::Counter as i32,
        samples: vec![Sample { at_ms: at, value }],
    }
}

fn labels(values: &[(&str, &str)]) -> HashMap<String, String> {
    values
        .iter()
        .map(|(k, v)| ((*k).to_string(), (*v).to_string()))
        .collect()
}

fn read_cpu() -> Option<(u64, u64)> {
    let content = fs::read_to_string("/proc/stat").ok()?;
    let line = content.lines().find(|line| line.starts_with("cpu "))?;
    let values = line
        .split_whitespace()
        .skip(1)
        .filter_map(|value| value.parse::<u64>().ok())
        .collect::<Vec<_>>();
    if values.len() < 4 {
        return None;
    }
    let idle = values[3] + values.get(4).copied().unwrap_or(0);
    let total: u64 = values.iter().sum();
    Some((idle, total))
}

/// Whether the node-exporter compatible `node_*` view is enabled. On by
/// default; `INCUS_FLEET_AGENT_NODE_COMPAT=0|false|off|no` turns it off for
/// deployments that only want the native `fleet_*` namespace.
fn node_compat_enabled() -> bool {
    match std::env::var("INCUS_FLEET_AGENT_NODE_COMPAT") {
        Ok(value) => !matches!(value.trim(), "0" | "false" | "off" | "no"),
        Err(_) => true,
    }
}

/// Per-core usage ratios from /proc/stat, keeping the previous counters for
/// the next rate calculation. Also emits the native per-core series.
fn collect_per_core(
    previous: &mut Option<Vec<(u64, u64)>>,
    series: &mut Vec<Series>,
    now: i64,
) -> Vec<(String, f64)> {
    let mut out = Vec::new();
    let Some(cores) = read_cpu_per_core() else {
        return out;
    };

    let prev = previous.take();
    let mut next = Vec::with_capacity(cores.len());
    for (index, (cpu_id, idle, total)) in cores.iter().enumerate() {
        let usage = match prev.as_ref().and_then(|p| p.get(index)) {
            Some((prev_idle, prev_total))
                if *prev_total > 0 && *total > *prev_total && idle >= prev_idle =>
            {
                let total_delta = (*total - *prev_total) as f64;
                let idle_delta = (*idle - *prev_idle) as f64;
                ((total_delta - idle_delta) / total_delta).clamp(0.0, 1.0)
            }
            _ => 0.0,
        };
        next.push((*idle, *total));
        out.push((cpu_id.clone(), usage));
        series.push(gauge(
            "fleet_host_cpu_core_usage_ratio",
            labels(&[("cpu", cpu_id)]),
            now,
            usage,
        ));
    }
    *previous = Some(next);

    out
}

/// Per-core (idle, total) counters from /proc/stat.
fn read_cpu_per_core() -> Option<Vec<(String, u64, u64)>> {
    let content = fs::read_to_string("/proc/stat").ok()?;
    let mut out = Vec::new();
    for line in content.lines() {
        let rest = match line.strip_prefix("cpu") {
            Some(rest) => rest,
            None => continue,
        };
        let mut parts = rest.split_whitespace();
        let index = match parts.next() {
            Some(value) if !value.is_empty() && value.chars().all(|c| c.is_ascii_digit()) => {
                value.to_string()
            }
            _ => continue,
        };
        let values: Vec<u64> = parts.filter_map(|value| value.parse().ok()).collect();
        if values.len() < 4 {
            continue;
        }
        let idle = values[3] + values.get(4).copied().unwrap_or(0);
        let total: u64 = values.iter().sum();
        out.push((index, idle, total));
    }
    if out.is_empty() { None } else { Some(out) }
}

/// One mounted filesystem's capacity readings.
#[derive(Clone, Debug)]
pub(crate) struct Filesystem {
    pub device: String,
    pub mountpoint: String,
    pub fstype: String,
    pub total_bytes: u64,
    pub free_bytes: u64,
    pub available_bytes: u64,
}

/// Filesystems worth reporting: real on-disk types from /proc/mounts measured
/// with statvfs. Pseudo-filesystems (proc, sysfs, cgroups, ...) are skipped to
/// keep the series list bounded.
fn read_filesystems() -> Vec<Filesystem> {
    const REPORTED: &[&str] = &[
        "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "vfat", "exfat", "ntfs", "ntfs3", "f2fs",
        "jfs", "reiserfs", "overlay",
    ];

    let Ok(content) = fs::read_to_string("/proc/mounts") else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for line in content.lines() {
        let mut parts = line.split_whitespace();
        let (Some(device), Some(mountpoint), Some(fstype)) =
            (parts.next(), parts.next(), parts.next())
        else {
            continue;
        };
        if !REPORTED.contains(&fstype) {
            continue;
        }
        if !device.starts_with("/dev/") && fstype != "overlay" {
            continue;
        }
        let mountpoint = unescape_mountpoint(mountpoint);
        let Ok(cpath) = std::ffi::CString::new(mountpoint.as_str()) else {
            continue;
        };
        let mut stats: libc::statvfs = unsafe { std::mem::zeroed() };
        // SAFETY: cpath is a valid NUL-terminated string owned by this call and
        // stats is a correctly sized, zeroed libc struct.
        if unsafe { libc::statvfs(cpath.as_ptr(), &mut stats) } != 0 {
            continue;
        }
        let block = stats.f_frsize.max(stats.f_bsize) as u64;
        out.push(Filesystem {
            device: device.to_string(),
            mountpoint,
            fstype: fstype.to_string(),
            total_bytes: block * stats.f_blocks as u64,
            free_bytes: block * stats.f_bfree as u64,
            available_bytes: block * stats.f_bavail as u64,
        });
    }
    out.sort_by(|a, b| a.mountpoint.cmp(&b.mountpoint));
    out
}

/// /proc/mounts escapes spaces, tabs, and backslashes in mount points.
fn unescape_mountpoint(value: &str) -> String {
    value
        .replace("\\040", " ")
        .replace("\\011", "\t")
        .replace("\\134", "\\")
}

/// Completed reads/writes per disk from /proc/diskstats: (device, reads,
/// writes). Loop and RAM devices are skipped to bound the label cardinality.
fn read_diskstats() -> Vec<(String, u64, u64)> {
    let Ok(content) = fs::read_to_string("/proc/diskstats") else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for line in content.lines() {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 8 {
            continue;
        }
        let device = fields[2];
        if device.starts_with("loop") || device.starts_with("ram") {
            continue;
        }
        let reads = fields[3].parse().unwrap_or(0);
        let writes = fields[7].parse().unwrap_or(0);
        out.push((device.to_string(), reads, writes));
    }
    out
}

/// Process and TCP counters read straight from procfs.
#[derive(Clone, Debug, Default)]
pub(crate) struct ProcCounts {
    pub running: Option<u64>,
    pub blocked: Option<u64>,
    pub forks_total: Option<u64>,
}

fn read_process_counts() -> ProcCounts {
    let mut counts = ProcCounts::default();
    if let Ok(content) = fs::read_to_string("/proc/stat") {
        for line in content.lines() {
            if let Some(value) = line.strip_prefix("procs_running ") {
                counts.running = value.trim().parse().ok();
            } else if let Some(value) = line.strip_prefix("procs_blocked ") {
                counts.blocked = value.trim().parse().ok();
            } else if let Some(value) = line.strip_prefix("processes ") {
                counts.forks_total = value.trim().parse().ok();
            }
        }
    }
    counts
}

/// Established TCP connections across IPv4 and IPv6.
fn read_tcp_established() -> Option<u64> {
    let mut total = 0u64;
    for path in ["/proc/net/tcp", "/proc/net/tcp6"] {
        let content = fs::read_to_string(path).ok()?;
        for line in content.lines().skip(1) {
            // The state is the fourth column; 01 is ESTABLISHED.
            if line.split_whitespace().nth(3) == Some("01") {
                total += 1;
            }
        }
    }
    Some(total)
}

fn read_memory() -> Option<(u64, u64)> {
    let content = fs::read_to_string("/proc/meminfo").ok()?;
    let total = meminfo_value(&content, "MemTotal")?;
    let available = meminfo_value(&content, "MemAvailable")?;
    Some((total * 1024, available * 1024))
}

fn meminfo_value(content: &str, key: &str) -> Option<u64> {
    content
        .lines()
        .find(|line| line.starts_with(key))?
        .split_whitespace()
        .nth(1)?
        .parse()
        .ok()
}

fn read_load() -> f64 {
    fs::read_to_string("/proc/loadavg")
        .ok()
        .and_then(|value| value.split_whitespace().next()?.parse().ok())
        .unwrap_or(0.0)
}

fn read_network() -> Vec<(String, u64, u64)> {
    let Ok(entries) = fs::read_dir("/sys/class/net") else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for entry in entries.flatten() {
        let name = entry.file_name().to_string_lossy().to_string();
        if name == "lo" {
            continue;
        }
        let rx = read_u64(&format!("/sys/class/net/{name}/statistics/rx_bytes")).unwrap_or(0);
        let tx = read_u64(&format!("/sys/class/net/{name}/statistics/tx_bytes")).unwrap_or(0);
        out.push((name, rx, tx));
    }
    out.sort_by(|a, b| a.0.cmp(&b.0));
    out
}

fn proc_tcp_retransmits() -> Option<u64> {
    let content = fs::read_to_string("/proc/net/snmp").ok()?;
    let mut header = false;
    for line in content.lines() {
        if line.starts_with("Tcp:") {
            if !header {
                header = true;
                continue;
            }
            let values: Vec<&str> = line.split_whitespace().collect();
            let idx = values.iter().position(|value| *value == "RetransSegs")?;
            return values.get(idx + 1)?.parse().ok();
        }
    }
    None
}

fn proc_oom_kills() -> Option<u64> {
    let content = fs::read_to_string("/proc/vmstat").ok()?;
    for line in content.lines() {
        if let Some(value) = line.strip_prefix("oom_kill ") {
            return value.trim().parse().ok();
        }
    }
    None
}

fn read_u64(path: &str) -> Option<u64> {
    fs::read_to_string(Path::new(path))
        .ok()?
        .trim()
        .parse()
        .ok()
}

fn now_ms() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_meminfo() {
        let content = "MemTotal:       1000 kB\nMemAvailable:    400 kB\n";
        assert_eq!(meminfo_value(content, "MemTotal"), Some(1000));
        assert_eq!(meminfo_value(content, "MemAvailable"), Some(400));
        assert_eq!(meminfo_value(content, "Missing"), None);
    }

    #[test]
    fn collector_always_reports_mode() {
        let mut collector = Collector::new();
        let batch = collector.collect("agent-1");
        assert_eq!(batch.agent_id, "agent-1");
        assert!(
            batch
                .series
                .iter()
                .any(|item| item.name == "fleet_agent_telemetry_mode")
        );
    }

    #[test]
    fn node_compat_switch_disables_the_view() {
        let mut collector = Collector::new().with_node_compat(false);
        let batch = collector.collect("agent-1");
        assert!(
            batch
                .series
                .iter()
                .all(|item| !item.name.starts_with("node_")),
            "node_* series emitted with compatibility off"
        );

        let mut collector = Collector::new().with_node_compat(true);
        let batch = collector.collect("agent-1");
        assert!(
            batch
                .series
                .iter()
                .any(|item| item.name.starts_with("node_")),
            "node_* series missing with compatibility on"
        );
    }

    #[test]
    fn maps_labels_and_types() {
        let series = gauge("x", labels(&[("state", "used")]), 10, 1.5);
        assert_eq!(series.r#type, MetricType::Gauge as i32);
        assert_eq!(series.labels.get("state").map(String::as_str), Some("used"));
        let series = counter("y", HashMap::new(), 11, 2.0);
        assert_eq!(series.r#type, MetricType::Counter as i32);
    }
}
