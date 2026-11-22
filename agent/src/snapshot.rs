use std::time::Duration;

use anyhow::{Context, Result, bail};
use serde::Deserialize;
use tokio::process::Command;

use crate::proto::v1::{Address, Nic, NodenetSnapshot, Route};

#[derive(Debug, Deserialize)]
struct IpLink {
    ifname: String,
    #[serde(default)]
    operstate: String,
    #[serde(default)]
    mtu: i32,
    #[serde(default)]
    address: String,
    #[serde(default)]
    link_type: String,
    #[serde(default)]
    addr_info: Vec<IpAddr>,
}

#[derive(Debug, Deserialize)]
struct IpAddr {
    #[serde(default)]
    family: String,
    #[serde(default)]
    local: String,
    #[serde(default)]
    prefixlen: i32,
}

#[derive(Debug, Deserialize)]
struct IpRoute {
    #[serde(default)]
    dst: String,
    #[serde(default)]
    gateway: String,
    #[serde(default)]
    dev: String,
    #[serde(default)]
    metric: Option<serde_json::Value>,
}

/// Collect a read-only host network snapshot.
pub async fn collect() -> Result<NodenetSnapshot> {
    let host = std::fs::read_to_string("/proc/sys/kernel/hostname")
        .unwrap_or_default()
        .trim()
        .to_string();
    let links = run_ip(&["-j", "addr", "show"]).await?;
    let routes = run_ip(&["-j", "route", "show"]).await?;
    parse(&links, &routes, &host)
}

async fn run_ip(args: &[&str]) -> Result<String> {
    let output = tokio::time::timeout(
        Duration::from_secs(5),
        Command::new("ip").args(args).output(),
    )
    .await
    .context("ip command timed out")?
    .context("run ip command")?;
    if !output.status.success() {
        bail!(
            "ip {} failed: {}",
            args.join(" "),
            String::from_utf8_lossy(&output.stderr)
        );
    }
    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

fn parse(links: &str, routes: &str, host: &str) -> Result<NodenetSnapshot> {
    let links: Vec<IpLink> = serde_json::from_str(links).context("parse ip addr JSON")?;
    let routes: Vec<IpRoute> = serde_json::from_str(routes).context("parse ip route JSON")?;

    let nics = links
        .into_iter()
        .map(|link| Nic {
            name: link.ifname,
            state: link.operstate.to_ascii_lowercase(),
            kind: match link.link_type.as_str() {
                "wlan" => "wifi".into(),
                "ether" => "ethernet".into(),
                other => other.to_string(),
            },
            mac: link.address,
            mtu: link.mtu,
            addresses: link
                .addr_info
                .into_iter()
                .map(|addr| Address {
                    family: addr.family,
                    address: addr.local,
                    prefixlen: addr.prefixlen,
                })
                .collect(),
            wifi: None,
        })
        .collect();

    let routes = routes
        .into_iter()
        .map(|route| Route {
            destination: if route.dst.is_empty() {
                "default".into()
            } else {
                route.dst
            },
            gateway: route.gateway,
            device: route.dev,
            metric: route.metric.map(metric_string).unwrap_or_default(),
        })
        .collect();

    Ok(NodenetSnapshot {
        host: host.to_string(),
        nics,
        routes,
        firewall: Vec::new(),
        wifi_scan: Vec::new(),
        wifi_device: String::new(),
        capabilities: vec!["nics".into(), "routes".into()],
    })
}

fn metric_string(value: serde_json::Value) -> String {
    match value {
        serde_json::Value::String(value) => value,
        other => other.to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_iproute2_json() {
        let links = r#"[
            {"ifname":"eth0","operstate":"UP","mtu":1500,"address":"aa:bb:cc:dd:ee:ff",
             "link_type":"ether","addr_info":[{"family":"inet","local":"10.0.0.50","prefixlen":24}]},
            {"ifname":"wlan0","operstate":"DOWN","link_type":"wlan","addr_info":[]}
        ]"#;
        let routes = r#"[
            {"dst":"default","gateway":"10.0.0.1","dev":"eth0","metric":100},
            {"dst":"10.0.1.0/24","dev":"eth0"}
        ]"#;
        let snapshot = parse(links, routes, "node-a").unwrap();
        assert_eq!(snapshot.host, "node-a");
        assert_eq!(snapshot.nics.len(), 2);
        assert_eq!(snapshot.nics[0].name, "eth0");
        assert_eq!(snapshot.nics[0].kind, "ethernet");
        assert_eq!(snapshot.nics[0].addresses[0].address, "10.0.0.50");
        assert_eq!(snapshot.nics[1].kind, "wifi");
        assert_eq!(snapshot.routes[0].destination, "default");
        assert_eq!(snapshot.routes[0].metric, "100");
        assert_eq!(snapshot.routes[1].destination, "10.0.1.0/24");
    }

    #[test]
    fn rejects_invalid_json() {
        assert!(parse("not-json", "[]", "node-a").is_err());
    }
}
