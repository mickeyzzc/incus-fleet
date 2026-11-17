use std::path::Path;

/// Host-level counters read from the optional eBPF maps.
#[derive(Clone, Copy, Debug, Default)]
pub struct Snapshot {
    pub tcp_retransmits: u64,
    pub oom_kills: u64,
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
    use std::path::Path;

    use aya::Ebpf;
    use aya::maps::{MapData, PerCpuArray};
    use aya::programs::TracePoint;

    use super::Snapshot;

    pub struct Collector {
        _ebpf: Ebpf,
        retransmits: PerCpuArray<MapData, u64>,
        oom_kills: PerCpuArray<MapData, u64>,
    }

    impl Collector {
        pub fn open(path: &Path) -> Result<Self, String> {
            let mut ebpf =
                Ebpf::load_file(path).map_err(|err| format!("load eBPF object: {err}"))?;
            attach(&mut ebpf, "tcp_retransmit", "tcp", "tcp_retransmit_skb")?;
            attach(&mut ebpf, "oom_mark", "oom", "mark_victim")?;
            let retransmits = PerCpuArray::try_from(
                ebpf.take_map("RETRANSMITS")
                    .ok_or_else(|| "missing RETRANSMITS map".to_string())?,
            )
            .map_err(|err| format!("open RETRANSMITS: {err}"))?;
            let oom_kills = PerCpuArray::try_from(
                ebpf.take_map("OOM_KILLS")
                    .ok_or_else(|| "missing OOM_KILLS map".to_string())?,
            )
            .map_err(|err| format!("open OOM_KILLS: {err}"))?;
            Ok(Self {
                _ebpf: ebpf,
                retransmits,
                oom_kills,
            })
        }

        pub fn collect(&self) -> Snapshot {
            Snapshot {
                tcp_retransmits: total(&self.retransmits),
                oom_kills: total(&self.oom_kills),
            }
        }
    }

    fn attach(ebpf: &mut Ebpf, name: &str, category: &str, event: &str) -> Result<(), String> {
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
            .map_err(|err| format!("attach {name}: {err}"))
    }

    fn total(map: &PerCpuArray<MapData, u64>) -> u64 {
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
}
