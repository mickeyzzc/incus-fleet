use std::collections::BTreeMap;
use std::fs;
use std::path::Path;
use std::time::Duration;

use tokio::process::Command;

use crate::proto::v1::FactsReport;

/// Facts is the host inventory reported after Hello.
#[derive(Clone, Debug, Default)]
pub struct Facts {
    pub hostname: String,
    pub os: String,
    pub os_version: String,
    pub kernel: String,
    pub arch: String,
    pub network_manager: String,
    pub incus_version: String,
    pub incus_status: String,
    pub ebpf_available: bool,
    pub boot_id: String,
    pub extra: BTreeMap<String, String>,
}

impl Facts {
    /// Convert to the wire representation.
    pub fn to_proto(&self) -> FactsReport {
        FactsReport {
            hostname: self.hostname.clone(),
            os: self.os.clone(),
            os_version: self.os_version.clone(),
            kernel: self.kernel.clone(),
            arch: self.arch.clone(),
            network_manager: self.network_manager.clone(),
            incus_version: self.incus_version.clone(),
            incus_status: self.incus_status.clone(),
            ebpf_available: self.ebpf_available,
            boot_id: self.boot_id.clone(),
            extra: self.extra.clone().into_iter().collect(),
        }
    }
}

/// Collect host facts. Missing files or commands degrade to empty values.
pub async fn collect() -> Facts {
    let (os, os_version, pretty) = read_os_release(Path::new("/etc/os-release"));
    let mut extra = BTreeMap::new();
    if !pretty.is_empty() {
        extra.insert("pretty_name".into(), pretty);
    }
    let incus_version = incus_version().await;
    let incus_status = if incus_version.is_empty() {
        "missing".to_string()
    } else {
        "installed".to_string()
    };

    Facts {
        hostname: read_trim(Path::new("/proc/sys/kernel/hostname")),
        os,
        os_version,
        kernel: read_trim(Path::new("/proc/sys/kernel/osrelease")),
        arch: std::env::consts::ARCH.to_string(),
        network_manager: detect_network_manager(Path::new("/")),
        incus_version,
        incus_status,
        ebpf_available: Path::new("/sys/kernel/btf/vmlinux").exists(),
        boot_id: read_trim(Path::new("/proc/sys/kernel/random/boot_id")),
        extra,
    }
}

fn read_trim(path: &Path) -> String {
    fs::read_to_string(path)
        .map(|value| value.trim().to_string())
        .unwrap_or_default()
}

fn read_os_release(path: &Path) -> (String, String, String) {
    let Ok(content) = fs::read_to_string(path) else {
        return (String::new(), String::new(), String::new());
    };
    let mut id = String::new();
    let mut version = String::new();
    let mut pretty = String::new();
    for line in content.lines() {
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        let value = value.trim().trim_matches('"').to_string();
        match key.trim() {
            "ID" => id = value,
            "VERSION_ID" => version = value,
            "PRETTY_NAME" => pretty = value,
            _ => {}
        }
    }
    (id, version, pretty)
}

fn detect_network_manager(root: &Path) -> String {
    if root.join("etc/netplan").is_dir() {
        return "netplan".into();
    }
    if root.join("etc/NetworkManager/NetworkManager.conf").exists()
        || root.join("etc/NetworkManager/system-connections").exists()
    {
        return "NetworkManager".into();
    }
    if root.join("etc/systemd/network").is_dir() {
        return "systemd-networkd".into();
    }
    if root.join("etc/network/interfaces").exists() {
        return "ifupdown".into();
    }
    "unknown".into()
}

async fn incus_version() -> String {
    let result = tokio::time::timeout(
        Duration::from_secs(3),
        Command::new("incus").arg("--version").output(),
    )
    .await;
    match result {
        Ok(Ok(output)) if output.status.success() => {
            String::from_utf8_lossy(&output.stdout).trim().to_string()
        }
        _ => String::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_os_release() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("os-release");
        fs::write(
            &path,
            "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\n",
        )
        .unwrap();
        let (id, version, pretty) = read_os_release(&path);
        assert_eq!(id, "debian");
        assert_eq!(version, "13");
        assert!(pretty.contains("Debian"));
    }

    #[test]
    fn detects_network_manager() {
        let dir = tempfile::tempdir().unwrap();
        fs::create_dir_all(dir.path().join("etc/netplan")).unwrap();
        assert_eq!(detect_network_manager(dir.path()), "netplan");
    }

    #[test]
    fn facts_proto_round_trip() {
        let facts = Facts {
            hostname: "node-a".into(),
            os: "debian".into(),
            os_version: "13".into(),
            kernel: "6.12".into(),
            arch: "amd64".into(),
            network_manager: "systemd-networkd".into(),
            incus_version: "7.0.2".into(),
            incus_status: "installed".into(),
            ebpf_available: true,
            boot_id: "boot".into(),
            extra: BTreeMap::new(),
        };
        let proto = facts.to_proto();
        assert_eq!(proto.hostname, "node-a");
        assert!(proto.ebpf_available);
    }
}
