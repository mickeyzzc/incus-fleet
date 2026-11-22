//! Node-exporter compatible metric names.
//!
//! The fleet agent emits its own `fleet_*` series. Existing Prometheus dashboards
//! and alert rules, however, are written against the node-exporter names. This
//! module derives a parallel `node_*` view from the same host readings so those
//! rules keep working without a second agent on each host.

use std::collections::HashMap;

use crate::metrics::{Filesystem, ProcCounts};
use crate::proto::v1::{MetricType, Sample, Series};

/// Build the node-exporter compatible series for one collection cycle.
///
/// Every metric here mirrors a value the caller already read; nothing is probed
/// twice. Metrics node-exporter exposes that this agent cannot cheaply derive
/// (per-CPU, filesystem, systemd) are simply absent rather than fabricated.
pub struct NodeMetrics {
    pub now_ms: i64,
    pub cpu_usage_ratio: Option<f64>,
    /// Per-core usage ratios; when non-empty the per-core view replaces the
    /// aggregate `cpu="0"` series.
    pub per_cpu_usage: Vec<(String, f64)>,
    pub memory_total: Option<u64>,
    pub memory_available: Option<u64>,
    pub load1: Option<f64>,
    pub uptime_seconds: f64,
    pub network: Vec<(String, u64, u64)>,
    pub tcp_retransmits: Option<u64>,
    pub oom_kills: Option<u64>,
    pub filesystems: Vec<Filesystem>,
    /// (device, reads completed, writes completed).
    pub disks: Vec<(String, u64, u64)>,
    pub procs: ProcCounts,
    pub tcp_established: Option<u64>,
}

impl NodeMetrics {
    /// Render the series in node-exporter's naming and label conventions.
    pub fn series(&self) -> Vec<Series> {
        let mut out = Vec::new();

        if !self.per_cpu_usage.is_empty() {
            // node-exporter reports idle seconds; the fleet agent measures a
            // ratio. Expose the ratio under the `node_cpu_seconds_total` family
            // using the conventional `cpu`/`mode` labels so ratios of idle are
            // still computable — one series per core, as node-exporter does.
            for (cpu, usage) in &self.per_cpu_usage {
                let mut labels = HashMap::new();
                labels.insert("cpu".to_string(), cpu.clone());
                labels.insert("mode".to_string(), "idle".to_string());
                out.push(series(
                    "node_cpu_seconds_total",
                    labels,
                    MetricType::Counter,
                    self.now_ms,
                    (1.0 - usage).max(0.0),
                ));
            }
        } else if let Some(usage) = self.cpu_usage_ratio {
            let mut labels = HashMap::new();
            labels.insert("cpu".to_string(), "0".to_string());
            labels.insert("mode".to_string(), "idle".to_string());
            out.push(series(
                "node_cpu_seconds_total",
                labels,
                MetricType::Counter,
                self.now_ms,
                (1.0 - usage).max(0.0),
            ));
        }

        if let Some(total) = self.memory_total {
            let mut labels = HashMap::new();
            labels.insert("kind".to_string(), "MemTotal".to_string());
            out.push(series(
                "node_memory_MemTotal_bytes",
                labels,
                MetricType::Gauge,
                self.now_ms,
                total as f64,
            ));
        }

        if let Some(available) = self.memory_available {
            let mut labels = HashMap::new();
            labels.insert("kind".to_string(), "MemAvailable".to_string());
            out.push(series(
                "node_memory_MemAvailable_bytes",
                labels,
                MetricType::Gauge,
                self.now_ms,
                available as f64,
            ));
            if let Some(total) = self.memory_total {
                let mut used = HashMap::new();
                used.insert("kind".to_string(), "MemUsed".to_string());
                out.push(series(
                    "node_memory_MemUsed_bytes",
                    used,
                    MetricType::Gauge,
                    self.now_ms,
                    total.saturating_sub(available) as f64,
                ));
            }
        }

        if let Some(load1) = self.load1 {
            let mut labels = HashMap::new();
            labels.insert("kind".to_string(), "1".to_string());
            out.push(series(
                "node_load1",
                labels,
                MetricType::Gauge,
                self.now_ms,
                load1,
            ));
        }

        out.push(series(
            "node_boot_time_seconds",
            HashMap::new(),
            MetricType::Gauge,
            self.now_ms,
            self.uptime_seconds,
        ));

        for (device, rx, tx) in &self.network {
            let mut labels = HashMap::new();
            labels.insert("device".to_string(), device.clone());
            out.push(series(
                "node_network_receive_bytes_total",
                labels.clone(),
                MetricType::Counter,
                self.now_ms,
                *rx as f64,
            ));
            out.push(series(
                "node_network_transmit_bytes_total",
                labels,
                MetricType::Counter,
                self.now_ms,
                *tx as f64,
            ));
        }

        if let Some(retransmits) = self.tcp_retransmits {
            out.push(series(
                "node_netstat_Tcp_RetransSegs",
                HashMap::new(),
                MetricType::Counter,
                self.now_ms,
                retransmits as f64,
            ));
        }

        if let Some(oom) = self.oom_kills {
            out.push(series(
                "node_vmstat_oom_kill",
                HashMap::new(),
                MetricType::Counter,
                self.now_ms,
                oom as f64,
            ));
        }

        for fs in &self.filesystems {
            let labels = |kind: &str| {
                let mut values = HashMap::new();
                values.insert("device".to_string(), fs.device.clone());
                values.insert("mountpoint".to_string(), fs.mountpoint.clone());
                values.insert("fstype".to_string(), fs.fstype.clone());
                values.insert("mode".to_string(), kind.to_string());
                values
            };
            out.push(series(
                "node_filesystem_size_bytes",
                labels("total"),
                MetricType::Gauge,
                self.now_ms,
                fs.total_bytes as f64,
            ));
            out.push(series(
                "node_filesystem_free_bytes",
                labels("free"),
                MetricType::Gauge,
                self.now_ms,
                fs.free_bytes as f64,
            ));
            out.push(series(
                "node_filesystem_avail_bytes",
                labels("available"),
                MetricType::Gauge,
                self.now_ms,
                fs.available_bytes as f64,
            ));
        }

        for (device, reads, writes) in &self.disks {
            let mut read_labels = HashMap::new();
            read_labels.insert("device".to_string(), device.clone());
            out.push(series(
                "node_disk_reads_completed_total",
                read_labels.clone(),
                MetricType::Counter,
                self.now_ms,
                *reads as f64,
            ));
            let mut write_labels = HashMap::new();
            write_labels.insert("device".to_string(), device.clone());
            out.push(series(
                "node_disk_writes_completed_total",
                write_labels,
                MetricType::Counter,
                self.now_ms,
                *writes as f64,
            ));
        }

        if let Some(running) = self.procs.running {
            out.push(series(
                "node_procs_running",
                HashMap::new(),
                MetricType::Gauge,
                self.now_ms,
                running as f64,
            ));
        }
        if let Some(blocked) = self.procs.blocked {
            out.push(series(
                "node_procs_blocked",
                HashMap::new(),
                MetricType::Gauge,
                self.now_ms,
                blocked as f64,
            ));
        }
        if let Some(forks) = self.procs.forks_total {
            out.push(series(
                "node_forks_total",
                HashMap::new(),
                MetricType::Counter,
                self.now_ms,
                forks as f64,
            ));
        }
        if let Some(established) = self.tcp_established {
            out.push(series(
                "node_netstat_Tcp_CurrEstab",
                HashMap::new(),
                MetricType::Gauge,
                self.now_ms,
                established as f64,
            ));
        }

        out
    }
}

fn series(
    name: &str,
    labels: HashMap<String, String>,
    kind: MetricType,
    at_ms: i64,
    value: f64,
) -> Series {
    Series {
        name: name.to_string(),
        labels,
        r#type: kind as i32,
        samples: vec![Sample { at_ms, value }],
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn find<'a>(series: &'a [Series], name: &str) -> Option<&'a Series> {
        series.iter().find(|item| item.name == name)
    }

    #[test]
    fn renders_node_exporter_names() {
        let metrics = NodeMetrics {
            now_ms: 1_000,
            cpu_usage_ratio: Some(0.25),
            per_cpu_usage: vec![("0".to_string(), 0.25), ("1".to_string(), 0.5)],
            memory_total: Some(100),
            memory_available: Some(40),
            load1: Some(0.5),
            uptime_seconds: 123.0,
            network: vec![("eth0".to_string(), 10, 20)],
            tcp_retransmits: Some(3),
            oom_kills: Some(1),
            filesystems: vec![Filesystem {
                device: "/dev/sda1".to_string(),
                mountpoint: "/".to_string(),
                fstype: "ext4".to_string(),
                total_bytes: 1000,
                free_bytes: 600,
                available_bytes: 500,
            }],
            disks: vec![("sda".to_string(), 12, 34)],
            procs: ProcCounts {
                running: Some(2),
                blocked: Some(1),
                forks_total: Some(99),
            },
            tcp_established: Some(7),
        };

        let series = metrics.series();

        // Per-core view replaces the aggregate series.
        assert_eq!(
            series
                .iter()
                .filter(|item| item.name == "node_cpu_seconds_total")
                .count(),
            2
        );
        let cpu = find(&series, "node_cpu_seconds_total").expect("cpu series");
        // idle is the complement of the fleet agent's usage ratio
        assert!((cpu.samples[0].value - 0.75).abs() < f64::EPSILON);
        assert_eq!(cpu.labels.get("mode").map(String::as_str), Some("idle"));

        let total = find(&series, "node_memory_MemTotal_bytes").expect("memory total");
        assert_eq!(total.samples[0].value, 100.0);

        let used = find(&series, "node_memory_MemUsed_bytes").expect("memory used");
        assert_eq!(used.samples[0].value, 60.0);

        assert!(find(&series, "node_load1").is_some());
        assert!(find(&series, "node_boot_time_seconds").is_some());

        let rx = find(&series, "node_network_receive_bytes_total").expect("rx");
        assert_eq!(rx.labels.get("device").map(String::as_str), Some("eth0"));
        assert_eq!(rx.samples[0].value, 10.0);

        assert!(find(&series, "node_netstat_Tcp_RetransSegs").is_some());
        assert!(find(&series, "node_vmstat_oom_kill").is_some());

        let size = find(&series, "node_filesystem_size_bytes").expect("fs size");
        assert_eq!(size.labels.get("mountpoint").map(String::as_str), Some("/"));
        assert_eq!(size.samples[0].value, 1000.0);
        let avail = find(&series, "node_filesystem_avail_bytes").expect("fs avail");
        assert_eq!(avail.samples[0].value, 500.0);

        let reads = find(&series, "node_disk_reads_completed_total").expect("disk reads");
        assert_eq!(reads.labels.get("device").map(String::as_str), Some("sda"));
        assert!(find(&series, "node_disk_writes_completed_total").is_some());

        assert!(find(&series, "node_procs_running").is_some());
        assert!(find(&series, "node_procs_blocked").is_some());
        assert!(find(&series, "node_forks_total").is_some());
        let established = find(&series, "node_netstat_Tcp_CurrEstab").expect("curr estab");
        assert_eq!(established.samples[0].value, 7.0);
    }

    #[test]
    fn omits_metrics_without_readings() {
        let metrics = NodeMetrics {
            now_ms: 1,
            cpu_usage_ratio: None,
            per_cpu_usage: Vec::new(),
            memory_total: None,
            memory_available: None,
            load1: None,
            uptime_seconds: 1.0,
            network: Vec::new(),
            tcp_retransmits: None,
            oom_kills: None,
            filesystems: Vec::new(),
            disks: Vec::new(),
            procs: ProcCounts::default(),
            tcp_established: None,
        };

        let series = metrics.series();

        // Only the unconditional boot time series is present.
        assert_eq!(series.len(), 1);
        assert_eq!(series[0].name, "node_boot_time_seconds");
    }
}

#[cfg(test)]
mod integration {
    use crate::metrics::Collector;

    #[test]
    fn collector_emits_both_namespaces() {
        let mut collector = Collector::new();
        let batch = collector.collect("agent-test");

        let names: Vec<&str> = batch.series.iter().map(|s| s.name.as_str()).collect();
        assert!(
            names.iter().any(|n| n.starts_with("fleet_")),
            "expected fleet_* series, got {names:?}"
        );
        assert!(
            names.iter().any(|n| n.starts_with("node_")),
            "expected node_* series, got {names:?}"
        );
    }
}
