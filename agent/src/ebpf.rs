use std::collections::HashMap;
use std::path::Path;

use crate::proto::v1::{MetricType, Sample, Series};

/// Number of block-latency buckets. Bucket b covers up to 2^b nanoseconds.
pub const BLOCK_BUCKETS: usize = 31;

/// Block-latency histogram keyed by the issuing cgroup.
#[derive(Clone, Debug, Default)]
pub struct BlockHistogram {
    pub buckets: [u64; BLOCK_BUCKETS],
    pub sum_ns: u64,
}

impl BlockHistogram {
    /// Number of completed requests.
    pub fn count(&self) -> u64 {
        self.buckets.iter().sum()
    }

    /// Render Prometheus-style `_bucket`, `_sum`, and `_count` series.
    pub fn series(&self, labels: &HashMap<String, String>, at_ms: i64) -> Vec<Series> {
        let mut out = Vec::with_capacity(BLOCK_BUCKETS + 2);
        let mut cumulative = 0u64;
        for (index, count) in self.buckets.iter().enumerate() {
            cumulative += count;
            let bound = (1u64 << index) as f64 / 1_000_000_000.0;
            let mut bucket_labels = labels.clone();
            bucket_labels.insert("le".into(), format!("{bound}"));
            out.push(Series {
                name: "fleet_instance_block_latency_seconds_bucket".into(),
                labels: bucket_labels,
                r#type: MetricType::Counter as i32,
                samples: vec![Sample {
                    at_ms,
                    value: cumulative as f64,
                }],
            });
        }
        let mut inf = labels.clone();
        inf.insert("le".into(), "+Inf".into());
        out.push(Series {
            name: "fleet_instance_block_latency_seconds_bucket".into(),
            labels: inf,
            r#type: MetricType::Counter as i32,
            samples: vec![Sample {
                at_ms,
                value: self.count() as f64,
            }],
        });
        out.push(Series {
            name: "fleet_instance_block_latency_seconds_sum".into(),
            labels: labels.clone(),
            r#type: MetricType::Counter as i32,
            samples: vec![Sample {
                at_ms,
                value: self.sum_ns as f64 / 1_000_000_000.0,
            }],
        });
        out.push(Series {
            name: "fleet_instance_block_latency_seconds_count".into(),
            labels: labels.clone(),
            r#type: MetricType::Counter as i32,
            samples: vec![Sample {
                at_ms,
                value: self.count() as f64,
            }],
        });
        out
    }
}

/// Per-instance eBPF counters attributed by cgroup.
#[derive(Clone, Debug, Default)]
pub struct InstanceSnapshot {
    pub instance: String,
    pub kind: String,
    pub tcp_retransmits: u64,
    pub oom_kills: u64,
    pub block: BlockHistogram,
}

/// Snapshot of the optional eBPF maps.
#[derive(Clone, Debug, Default)]
pub struct Snapshot {
    pub tcp_retransmits: u64,
    pub oom_kills: u64,
    pub instances: Vec<InstanceSnapshot>,
}

impl Snapshot {
    /// Render all per-instance counters and histograms as fleet series.
    pub fn series(&self, at_ms: i64) -> Vec<Series> {
        let mut out = Vec::new();
        for item in &self.instances {
            let labels = HashMap::from([
                ("project".to_string(), "default".to_string()),
                ("instance".to_string(), item.instance.clone()),
                ("type".to_string(), item.kind.clone()),
            ]);
            if item.tcp_retransmits > 0 {
                out.push(counter(
                    "fleet_instance_tcp_retransmits_total",
                    labels.clone(),
                    at_ms,
                    item.tcp_retransmits as f64,
                ));
            }
            if item.oom_kills > 0 {
                out.push(counter(
                    "fleet_instance_oom_kills_total",
                    labels.clone(),
                    at_ms,
                    item.oom_kills as f64,
                ));
            }
            if item.block.count() > 0 {
                out.extend(item.block.series(&labels, at_ms));
            }
        }
        out
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

/// Whether the platform and object can support eBPF collection.
pub fn available(object: Option<&Path>) -> bool {
    #[cfg(target_os = "linux")]
    {
        return object.is_some_and(Path::exists) && Path::new("/sys/kernel/btf/vmlinux").exists();
    }
    #[cfg(not(target_os = "linux"))]
    {
        let _ = object;
        false
    }
}

#[cfg(target_os = "linux")]
mod platform {
    use std::collections::BTreeMap;
    use std::ffi::CString;
    use std::os::raw::{c_char, c_int};
    use std::path::Path;

    use aya::maps::{HashMap as AyaHashMap, MapData};
    use aya::programs::{CgroupAttachMode, RawTracePoint, SockOps, TracePoint};
    use aya::{Ebpf, Pod};

    use super::{BLOCK_BUCKETS, BlockHistogram, InstanceSnapshot, Snapshot};

    #[repr(C)]
    #[derive(Clone, Copy, Debug)]
    struct BlockKey {
        cgroup_id: u64,
        bucket: u32,
        pad: u32,
    }

    unsafe impl Pod for BlockKey {}

    #[repr(C)]
    #[derive(Clone, Copy, Debug)]
    pub struct FlowKey {
        saddr: [u8; 16],
        daddr: [u8; 16],
        sport: u16,
        dport: u16,
        family: u16,
        pad: u16,
    }

    unsafe impl Pod for FlowKey {}

    const _: () = assert!(std::mem::size_of::<FlowKey>() == 40);

    pub struct Collector {
        _ebpf: Ebpf,
        retransmits: aya::maps::PerCpuArray<MapData, u64>,
        oom_kills: aya::maps::PerCpuArray<MapData, u64>,
        cgroup_retransmits: AyaHashMap<MapData, u64, u64>,
        cgroup_oom_kills: AyaHashMap<MapData, u64, u64>,
        block_latency: AyaHashMap<MapData, BlockKey, u64>,
        block_sum_ns: AyaHashMap<MapData, u64, u64>,
        socket_cgroups: AyaHashMap<MapData, FlowKey, u64>,
    }

    impl Collector {
        pub fn open(path: &Path) -> Result<Self, String> {
            let mut ebpf =
                Ebpf::load_file(path).map_err(|err| format!("load eBPF object: {err}"))?;
            if let Err(err) =
                attach_sockops(&mut ebpf, "fleet_sockops", Path::new("/sys/fs/cgroup"))
            {
                tracing::warn!(
                    error = %err,
                    "sockops attach failed; TCP cgroup attribution falls back to the current task"
                );
            }
            attach_tracepoint(&mut ebpf, "tcp_retransmit", "tcp", "tcp_retransmit_skb")?;
            attach_tracepoint(&mut ebpf, "oom_mark", "oom", "mark_victim")?;
            attach_raw_tracepoint(&mut ebpf, "block_issue", "block_rq_issue")?;
            attach_raw_tracepoint(&mut ebpf, "block_complete", "block_rq_complete")?;

            let retransmits = aya::maps::PerCpuArray::try_from(
                ebpf.take_map("RETRANSMITS")
                    .ok_or_else(|| "missing RETRANSMITS map".to_string())?,
            )
            .map_err(|err| format!("open RETRANSMITS: {err}"))?;
            let oom_kills = aya::maps::PerCpuArray::try_from(
                ebpf.take_map("OOM_KILLS")
                    .ok_or_else(|| "missing OOM_KILLS map".to_string())?,
            )
            .map_err(|err| format!("open OOM_KILLS: {err}"))?;
            let cgroup_retransmits = AyaHashMap::try_from(
                ebpf.take_map("CGROUP_RETRANSMITS")
                    .ok_or_else(|| "missing CGROUP_RETRANSMITS map".to_string())?,
            )
            .map_err(|err| format!("open CGROUP_RETRANSMITS: {err}"))?;
            let cgroup_oom_kills = AyaHashMap::try_from(
                ebpf.take_map("CGROUP_OOM_KILLS")
                    .ok_or_else(|| "missing CGROUP_OOM_KILLS map".to_string())?,
            )
            .map_err(|err| format!("open CGROUP_OOM_KILLS: {err}"))?;
            let block_latency = AyaHashMap::try_from(
                ebpf.take_map("BLOCK_LATENCY")
                    .ok_or_else(|| "missing BLOCK_LATENCY map".to_string())?,
            )
            .map_err(|err| format!("open BLOCK_LATENCY: {err}"))?;
            let block_sum_ns = AyaHashMap::try_from(
                ebpf.take_map("BLOCK_SUM_NS")
                    .ok_or_else(|| "missing BLOCK_SUM_NS map".to_string())?,
            )
            .map_err(|err| format!("open BLOCK_SUM_NS: {err}"))?;
            let socket_cgroups = AyaHashMap::try_from(
                ebpf.take_map("SOCKET_CGROUPS")
                    .ok_or_else(|| "missing SOCKET_CGROUPS map".to_string())?,
            )
            .map_err(|err| format!("open SOCKET_CGROUPS: {err}"))?;

            Ok(Self {
                _ebpf: ebpf,
                retransmits,
                oom_kills,
                cgroup_retransmits,
                cgroup_oom_kills,
                block_latency,
                block_sum_ns,
                socket_cgroups,
            })
        }

        pub fn socket_cgroup_count(&self) -> usize {
            self.socket_cgroups.iter().count()
        }

        pub fn observed_cgroup_ids(&self) -> Vec<u64> {
            let mut ids = Vec::new();
            for entry in self.cgroup_retransmits.iter() {
                if let Ok((id, _)) = entry {
                    ids.push(id);
                }
            }
            for entry in self.cgroup_oom_kills.iter() {
                if let Ok((id, _)) = entry {
                    ids.push(id);
                }
            }
            for entry in self.block_latency.iter() {
                if let Ok((key, _)) = entry {
                    ids.push(key.cgroup_id);
                }
            }
            ids.sort_unstable();
            ids.dedup();
            ids
        }

        pub fn cgroup_table_debug(&self) -> Vec<(u64, String, String)> {
            cgroup_table()
                .into_iter()
                .map(|(id, (name, kind))| (id, name, kind.to_string()))
                .collect()
        }

        pub fn collect(&self) -> Snapshot {
            let cgroups = cgroup_table();
            let mut instances: BTreeMap<(String, String), InstanceSnapshot> = BTreeMap::new();

            for entry in self.cgroup_retransmits.iter() {
                let Ok((cgroup_id, value)) = entry else {
                    continue;
                };
                if let Some((name, kind)) = cgroups.get(&cgroup_id) {
                    let item = instances
                        .entry((name.clone(), (*kind).to_string()))
                        .or_insert_with(|| InstanceSnapshot {
                            instance: name.clone(),
                            kind: (*kind).to_string(),
                            ..Default::default()
                        });
                    item.tcp_retransmits += value;
                }
            }
            for entry in self.cgroup_oom_kills.iter() {
                let Ok((cgroup_id, value)) = entry else {
                    continue;
                };
                if let Some((name, kind)) = cgroups.get(&cgroup_id) {
                    let item = instances
                        .entry((name.clone(), (*kind).to_string()))
                        .or_insert_with(|| InstanceSnapshot {
                            instance: name.clone(),
                            kind: (*kind).to_string(),
                            ..Default::default()
                        });
                    item.oom_kills += value;
                }
            }

            for entry in self.block_latency.iter() {
                let Ok((key, value)) = entry else {
                    continue;
                };
                if key.bucket as usize >= BLOCK_BUCKETS || !cgroups.contains_key(&key.cgroup_id) {
                    continue;
                }
                let (name, kind) = cgroups.get(&key.cgroup_id).expect("checked above");
                let item = instances
                    .entry((name.clone(), (*kind).to_string()))
                    .or_insert_with(|| InstanceSnapshot {
                        instance: name.clone(),
                        kind: (*kind).to_string(),
                        ..Default::default()
                    });
                item.block.buckets[key.bucket as usize] += value;
            }
            for entry in self.block_sum_ns.iter() {
                let Ok((cgroup_id, value)) = entry else {
                    continue;
                };
                if let Some((name, kind)) = cgroups.get(&cgroup_id) {
                    let item = instances
                        .entry((name.clone(), (*kind).to_string()))
                        .or_insert_with(|| InstanceSnapshot {
                            instance: name.clone(),
                            kind: (*kind).to_string(),
                            ..Default::default()
                        });
                    item.block.sum_ns += value;
                }
            }

            let mut instances: Vec<_> = instances.into_values().collect();
            instances.sort_by(|a, b| {
                a.instance
                    .cmp(&b.instance)
                    .then_with(|| a.kind.cmp(&b.kind))
            });

            Snapshot {
                tcp_retransmits: total(&self.retransmits),
                oom_kills: total(&self.oom_kills),
                instances,
            }
        }
    }

    fn cgroup_table() -> BTreeMap<u64, (String, &'static str)> {
        let mut out = BTreeMap::new();
        let Ok(entries) = std::fs::read_dir("/sys/fs/cgroup") else {
            return out;
        };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            let Some((instance, kind)) = instance_name(&name) else {
                continue;
            };
            let root = entry.path();
            let mut stack = vec![(root, 0u32)];
            while let Some((path, depth)) = stack.pop() {
                if let Some(id) = cgroup_id(&path) {
                    out.insert(id, (instance.clone(), kind));
                }
                if depth >= 6 {
                    continue;
                }
                let Ok(children) = std::fs::read_dir(&path) else {
                    continue;
                };
                for child in children.flatten() {
                    if child.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
                        stack.push((child.path(), depth + 1));
                    }
                }
            }
        }
        out
    }

    #[repr(C)]
    struct FileHandle {
        handle_bytes: u32,
        handle_type: i32,
        f_handle: [u8; 8],
    }

    unsafe extern "C" {
        fn name_to_handle_at(
            dirfd: c_int,
            pathname: *const c_char,
            handle: *mut FileHandle,
            mount_id: *mut c_int,
            flags: c_int,
        ) -> c_int;
    }

    fn cgroup_id(path: &Path) -> Option<u64> {
        let path = CString::new(path.as_os_str().as_encoded_bytes()).ok()?;
        let mut handle = FileHandle {
            handle_bytes: 8,
            handle_type: 0,
            f_handle: [0; 8],
        };
        let mut mount_id: c_int = 0;
        // SAFETY: the handle buffer is exactly handle_bytes long and the path
        // is a valid NUL-terminated string for the duration of the call.
        let result =
            unsafe { name_to_handle_at(-100, path.as_ptr(), &mut handle, &mut mount_id, 0) };
        if result != 0 || handle.handle_bytes != 8 {
            return None;
        }
        Some(u64::from_le_bytes(handle.f_handle))
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

    fn attach_sockops(ebpf: &mut Ebpf, name: &str, cgroup_path: &Path) -> Result<(), String> {
        let program: &mut SockOps = ebpf
            .program_mut(name)
            .ok_or_else(|| format!("missing eBPF program {name}"))?
            .try_into()
            .map_err(|err| format!("cast {name}: {err}"))?;
        program
            .load()
            .map_err(|err| format!("load {name}: {err}"))?;
        let cgroup = std::fs::File::open(cgroup_path)
            .map_err(|err| format!("open {}: {err}", cgroup_path.display()))?;
        program
            .attach(&cgroup, CgroupAttachMode::Single)
            .map_err(|err| format!("attach {name}: {err}"))?;
        Ok(())
    }

    fn attach_tracepoint(
        ebpf: &mut Ebpf,
        name: &str,
        category: &str,
        event: &str,
    ) -> Result<(), String> {
        let program: &mut TracePoint = ebpf
            .program_mut(name)
            .ok_or_else(|| format!("missing eBPF program {name}"))?
            .try_into()
            .map_err(|err| format!("cast {name}: {err}"))?;
        program
            .load()
            .map_err(|err| format!("load {name}: {err}"))?;
        program
            .attach(category, event)
            .map_err(|err| format!("attach {name}: {err}"))?;
        Ok(())
    }

    fn attach_raw_tracepoint(ebpf: &mut Ebpf, name: &str, event: &str) -> Result<(), String> {
        let program: &mut RawTracePoint = ebpf
            .program_mut(name)
            .ok_or_else(|| format!("missing eBPF program {name}"))?
            .try_into()
            .map_err(|err| format!("cast {name}: {err}"))?;
        program
            .load()
            .map_err(|err| format!("load {name}: {err}"))?;
        program
            .attach(event)
            .map_err(|err| format!("attach {name}: {err}"))?;
        Ok(())
    }

    fn total(map: &aya::maps::PerCpuArray<MapData, u64>) -> u64 {
        map.get(&0, 0)
            .map(|values| values.iter().sum())
            .unwrap_or(0)
    }
}

#[cfg(not(target_os = "linux"))]
mod platform {
    use std::path::Path;

    use super::Snapshot;

    pub struct Collector;

    impl Collector {
        pub fn open(_path: &Path) -> Result<Self, String> {
            Err("eBPF is only supported on Linux".into())
        }

        pub fn collect(&self) -> Snapshot {
            Snapshot::default()
        }

        pub fn observed_cgroup_ids(&self) -> Vec<u64> {
            Vec::new()
        }

        pub fn socket_cgroup_count(&self) -> usize {
            0
        }

        pub fn cgroup_table_debug(&self) -> Vec<(u64, String, String)> {
            Vec::new()
        }
    }
}

pub use platform::Collector;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unavailable_without_object() {
        assert!(!available(None));
        assert!(!available(Some(Path::new("/definitely/missing/object.o"))));
    }

    #[test]
    fn histogram_series_are_cumulative() {
        let mut block = BlockHistogram::default();
        block.buckets[10] = 2;
        block.buckets[20] = 1;
        block.sum_ns = 3_000_000;
        let series = block.series(&HashMap::new(), 100);
        assert_eq!(block.count(), 3);
        let bucket10 = series
            .iter()
            .find(|item| item.name.ends_with("_bucket") && item.labels["le"] == "0.000001024")
            .expect("1us bucket");
        assert_eq!(bucket10.samples[0].value, 2.0);
        let inf = series
            .iter()
            .find(|item| item.labels.get("le").map(String::as_str) == Some("+Inf"))
            .expect("+Inf bucket");
        assert_eq!(inf.samples[0].value, 3.0);
        assert!(series.iter().any(
            |item| item.name.ends_with("_sum") && (item.samples[0].value - 0.003).abs() < 1e-9
        ));
    }

    #[test]
    fn instance_series_include_labels() {
        let snapshot = Snapshot {
            tcp_retransmits: 5,
            oom_kills: 1,
            instances: vec![InstanceSnapshot {
                instance: "demo".into(),
                kind: "container".into(),
                tcp_retransmits: 4,
                oom_kills: 1,
                block: BlockHistogram::default(),
            }],
        };
        let series = snapshot.series(100);
        assert!(
            series
                .iter()
                .any(|item| item.name == "fleet_instance_tcp_retransmits_total"
                    && item.labels["instance"] == "demo"
                    && item.samples[0].value == 4.0)
        );
        assert!(
            series
                .iter()
                .any(|item| item.name == "fleet_instance_oom_kills_total")
        );
    }
}
