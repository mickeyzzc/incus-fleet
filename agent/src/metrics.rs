use std::collections::HashMap;
use std::fs;
use std::path::Path;
use std::time::Instant;

use crate::proto::v1::{MetricType, Sample, Series, TelemetryBatch};

/// Collector keeps the previous CPU counters needed for a rate.
pub struct Collector {
    started: Instant,
    previous_cpu: Option<(u64, u64)>,
}

impl Collector {
    pub fn new() -> Self {
        Self {
            started: Instant::now(),
            previous_cpu: None,
        }
    }

    /// Collect one 10-second-friendly sample batch.
    pub fn collect(&mut self, agent_id: &str) -> TelemetryBatch {
        let now = now_ms();
        let mut series = Vec::new();

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
            series.push(gauge(
                "fleet_host_cpu_usage_ratio",
                HashMap::new(),
                now,
                usage,
            ));
        }

        if let Some((total, available)) = read_memory() {
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
    fn maps_labels_and_types() {
        let series = gauge("x", labels(&[("state", "used")]), 10, 1.5);
        assert_eq!(series.r#type, MetricType::Gauge as i32);
        assert_eq!(series.labels.get("state").map(String::as_str), Some("used"));
        let series = counter("y", HashMap::new(), 11, 2.0);
        assert_eq!(series.r#type, MetricType::Counter as i32);
    }
}
