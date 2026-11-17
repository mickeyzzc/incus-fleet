use std::collections::HashMap;
use std::fs;
use std::path::Path;

use crate::proto::v1::{MetricType, Sample, Series};

/// Collect per-instance cgroup v2 metrics from the Incus hierarchy.
pub fn collect(root: &Path, at_ms: i64) -> Vec<Series> {
    let Ok(entries) = fs::read_dir(root) else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for entry in entries.flatten() {
        let name = entry.file_name().to_string_lossy().to_string();
        let Some((instance, kind)) = instance_name(&name) else {
            continue;
        };
        let dir = entry.path();
        if !dir.join("cpu.stat").exists() && !dir.join("memory.current").exists() {
            continue;
        }
        let labels = instance_labels(&instance, kind);
        if let Some(usage) = cpu_usage(&dir) {
            out.push(counter(
                "fleet_instance_cpu_seconds_total",
                labels.clone(),
                at_ms,
                usage,
            ));
        }
        if let Some(usage) = read_number(&dir.join("memory.current")) {
            out.push(gauge(
                "fleet_instance_memory_bytes",
                with_label(&labels, "state", "usage"),
                at_ms,
                usage as f64,
            ));
        }
        if let Some(limit) = read_number(&dir.join("memory.max")) {
            out.push(gauge(
                "fleet_instance_memory_bytes",
                with_label(&labels, "state", "limit"),
                at_ms,
                limit as f64,
            ));
        }
        if let Some(pids) = read_number(&dir.join("pids.current")) {
            out.push(gauge(
                "fleet_instance_processes",
                labels.clone(),
                at_ms,
                pids as f64,
            ));
        }
        if let Some((read, write)) = io_bytes(&dir) {
            out.push(counter(
                "fleet_instance_io_bytes_total",
                with_label(&labels, "operation", "read"),
                at_ms,
                read as f64,
            ));
            out.push(counter(
                "fleet_instance_io_bytes_total",
                with_label(&labels, "operation", "write"),
                at_ms,
                write as f64,
            ));
        }
    }
    out.sort_by(|a, b| a.name.cmp(&b.name));
    out
}

fn instance_name(dir: &str) -> Option<(String, &'static str)> {
    for (prefix, kind) in [
        ("lxc.payload.", "container"),
        ("lxc.monitor.", "vm"),
        ("lxc.", "container"),
    ] {
        if let Some(name) = dir.strip_prefix(prefix)
            && !name.is_empty()
        {
            return Some((name.to_string(), kind));
        }
    }
    None
}

fn instance_labels(instance: &str, kind: &str) -> HashMap<String, String> {
    HashMap::from([
        ("project".to_string(), "default".to_string()),
        ("instance".to_string(), instance.to_string()),
        ("type".to_string(), kind.to_string()),
    ])
}

fn with_label(labels: &HashMap<String, String>, key: &str, value: &str) -> HashMap<String, String> {
    let mut out = labels.clone();
    out.insert(key.to_string(), value.to_string());
    out
}

fn cpu_usage(dir: &Path) -> Option<f64> {
    let content = fs::read_to_string(dir.join("cpu.stat")).ok()?;
    for line in content.lines() {
        if let Some(value) = line.strip_prefix("usage_usec ") {
            return value
                .trim()
                .parse::<f64>()
                .ok()
                .map(|usec| usec / 1_000_000.0);
        }
    }
    None
}

fn read_number(path: &Path) -> Option<u64> {
    let value = fs::read_to_string(path).ok()?;
    let value = value.trim();
    if value == "max" {
        return None;
    }
    value.parse().ok()
}

fn io_bytes(dir: &Path) -> Option<(u64, u64)> {
    let content = fs::read_to_string(dir.join("io.stat")).ok()?;
    let mut read = 0;
    let mut write = 0;
    let mut found = false;
    for line in content.lines() {
        for field in line.split_whitespace() {
            if let Some(value) = field.strip_prefix("rbytes=") {
                read += value.parse::<u64>().unwrap_or(0);
                found = true;
            }
            if let Some(value) = field.strip_prefix("wbytes=") {
                write += value.parse::<u64>().unwrap_or(0);
                found = true;
            }
        }
    }
    found.then_some((read, write))
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn collects_instance_metrics() {
        let dir = tempfile::tempdir().unwrap();
        let payload = dir.path().join("lxc.payload.demo");
        fs::create_dir_all(&payload).unwrap();
        fs::write(payload.join("cpu.stat"), "usage_usec 2500000\n").unwrap();
        fs::write(payload.join("memory.current"), "1048576\n").unwrap();
        fs::write(payload.join("memory.max"), "max\n").unwrap();
        fs::write(payload.join("pids.current"), "7\n").unwrap();
        fs::write(payload.join("io.stat"), "8:0 rbytes=100 wbytes=200\n").unwrap();
        let monitor = dir.path().join("lxc.monitor.vm1");
        fs::create_dir_all(&monitor).unwrap();
        fs::write(monitor.join("cpu.stat"), "usage_usec 1000000\n").unwrap();
        fs::write(monitor.join("memory.current"), "2048\n").unwrap();

        let series = collect(dir.path(), 1000);
        assert!(
            series
                .iter()
                .any(|item| item.name == "fleet_instance_cpu_seconds_total"
                    && item.labels["instance"] == "demo"
                    && item.samples[0].value == 2.5)
        );
        assert!(series.iter().any(|item| item.name == "fleet_instance_processes"
            && item.samples[0].value == 7.0));
        assert!(
            series
                .iter()
                .any(|item| item.name == "fleet_instance_io_bytes_total"
                    && item.labels["operation"] == "write"
                    && item.samples[0].value == 200.0)
        );
        assert!(series.iter().any(|item| item.labels["type"] == "vm"));
        assert!(
            !series
                .iter()
                .any(|item| item.labels.get("state") == Some(&"limit".into()))
        );
    }

    #[test]
    fn ignores_unrelated_directories() {
        let dir = tempfile::tempdir().unwrap();
        fs::create_dir_all(dir.path().join("system.slice")).unwrap();
        assert!(collect(dir.path(), 1).is_empty());
        assert_eq!(
            instance_name("lxc.payload.x"),
            Some(("x".into(), "container"))
        );
        assert_eq!(instance_name("lxc.monitor.x"), Some(("x".into(), "vm")));
        assert_eq!(instance_name("other"), None);
    }
}
