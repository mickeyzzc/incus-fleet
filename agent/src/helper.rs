use std::process::Stdio;

use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};
use tokio::io::AsyncWriteExt;
use tokio::process::Command;

use crate::proto::v1::NodenetApplySpec;

/// HelperRequest is the JSON contract with the Go helper.
#[derive(Debug, Serialize)]
pub struct HelperRequest {
    pub action: String,
    pub task_id: String,
    pub interface: String,
    pub up: Option<bool>,
    pub dhcp: bool,
    pub address: String,
    pub gateway: String,
    pub persist: bool,
    pub manager: String,
    pub destination: String,
    pub device: String,
    pub port: i32,
    pub proto: String,
    pub firewall_action: String,
    pub handle: String,
    pub comment: String,
    pub ssid: String,
    pub password: String,
    pub spec: String,
    pub rollback_after_seconds: i32,
}

#[derive(Debug, Deserialize)]
pub struct HelperResponse {
    pub ok: bool,
    pub action: String,
    pub task_id: String,
    pub backend: String,
    pub persistence: String,
    pub message: String,
    #[serde(default)]
    pub steps: Vec<String>,
    pub error: String,
}

impl HelperRequest {
    /// Convert a protobuf apply spec to the helper JSON contract.
    pub fn from_spec(spec: &NodenetApplySpec) -> Self {
        Self {
            action: spec.action.clone(),
            task_id: spec.task_id.clone(),
            interface: spec.interface.clone(),
            up: if spec.action == "link-state" {
                Some(spec.up)
            } else {
                None
            },
            dhcp: spec.dhcp,
            address: spec.address.clone(),
            gateway: spec.gateway.clone(),
            persist: spec.persist,
            manager: spec.manager.clone(),
            destination: spec.destination.clone(),
            device: spec.device.clone(),
            port: spec.port,
            proto: spec.proto.clone(),
            firewall_action: spec.firewall_action.clone(),
            handle: spec.handle.clone(),
            comment: spec.comment.clone(),
            ssid: spec.ssid.clone(),
            password: spec.password.clone(),
            spec: String::new(),
            rollback_after_seconds: spec.rollback_after_seconds,
        }
    }
}

/// Run the privileged helper via sudo and parse its JSON response.
pub async fn invoke(
    helper: &str,
    request: &HelperRequest,
    bootstrap_ca: Option<&std::path::Path>,
) -> Result<HelperResponse> {
    let mut command = Command::new("sudo");
    command.args(["-n", helper]);
    if let Some(ca) = bootstrap_ca {
        command.env("INCUS_FLEET_BOOTSTRAP_CA", ca);
    }
    let mut child = command
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .context("spawn incus-fleet-helper via sudo")?;
    let mut stdin = child.stdin.take().context("helper stdin")?;
    let payload = serde_json::to_vec(request)?;
    stdin.write_all(&payload).await?;
    drop(stdin);

    let output = child.wait_with_output().await.context("wait for helper")?;
    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);
    let response: HelperResponse = serde_json::from_str(stdout.trim())
        .with_context(|| format!("parse helper response; stderr={stderr}"))?;
    if !response.ok {
        bail!("helper failed: {}", response.error);
    }

    Ok(response)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_apply_spec() {
        let spec = NodenetApplySpec {
            action: "link-state".into(),
            task_id: "task-1".into(),
            interface: "eth0".into(),
            up: true,
            ..Default::default()
        };
        let request = HelperRequest::from_spec(&spec);
        assert_eq!(request.action, "link-state");
        assert_eq!(request.interface, "eth0");
        assert_eq!(request.up, Some(true));
        assert_eq!(request.task_id, "task-1");
    }
}
