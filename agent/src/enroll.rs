use std::fs;
use std::io::Read;
use std::path::Path;

use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};

use crate::facts;
use crate::identity::Identity;

#[derive(Serialize)]
struct EnrollRequest<'a> {
    token: &'a str,
    cluster: &'a str,
    member: &'a str,
    hostname: &'a str,
    version: &'a str,
    os: &'a str,
    arch: &'a str,
    kernel: &'a str,
    public_key: &'a str,
}

#[derive(Deserialize)]
struct EnrollResponse {
    agent_id: String,
    cluster: String,
    member: String,
    certificate: String,
    ca: String,
}

/// Enroll this host with the center and persist identity material.
pub async fn enroll(
    center_url: &str,
    ca_path: &Path,
    token: &str,
    cluster: &str,
    member: &str,
    hostname: &str,
    state_dir: &Path,
) -> Result<String> {
    if token.trim().is_empty() {
        bail!("enrollment token is empty");
    }
    let center = normalize_center(center_url);
    if !center.starts_with("https://") {
        bail!("center URL must use https");
    }
    let ca_pem =
        fs::read(ca_path).with_context(|| format!("read CA certificate {}", ca_path.display()))?;
    let ca_cert = reqwest::Certificate::from_pem(&ca_pem).context("parse center CA certificate")?;
    let client = reqwest::Client::builder()
        .add_root_certificate(ca_cert)
        .timeout(std::time::Duration::from_secs(20))
        .build()
        .context("build enrollment client")?;

    let key = rcgen::KeyPair::generate().context("generate agent key")?;
    let public_key = key.public_key_pem();
    let private_key = key.serialize_pem();
    let facts = facts::collect().await;
    let hostname = if hostname.trim().is_empty() {
        facts.hostname.clone()
    } else {
        hostname.trim().to_string()
    };

    let request = EnrollRequest {
        token: token.trim(),
        cluster: cluster.trim(),
        member: member.trim(),
        hostname: &hostname,
        version: env!("CARGO_PKG_VERSION"),
        os: &facts.os,
        arch: &facts.arch,
        kernel: &facts.kernel,
        public_key: &public_key,
    };
    let response = client
        .post(format!("{center}/api/v1/agent/enroll"))
        .json(&request)
        .send()
        .await
        .context("send enrollment request")?;
    let status = response.status();
    let body = response.text().await.context("read enrollment response")?;
    if !status.is_success() {
        bail!("enrollment failed: HTTP {status}: {body}");
    }
    let enrolled: EnrollResponse =
        serde_json::from_str(&body).context("parse enrollment response")?;

    let identity = Identity {
        agent_id: enrolled.agent_id.clone(),
        cluster: enrolled.cluster,
        member: enrolled.member,
        center_url: center,
        hostname,
    };
    Identity::save(
        state_dir,
        &identity,
        enrolled.certificate.as_bytes(),
        private_key.as_bytes(),
        enrolled.ca.as_bytes(),
    )?;

    Ok(enrolled.agent_id)
}

fn normalize_center(value: &str) -> String {
    let trimmed = value.trim().trim_end_matches('/');
    trimmed.to_string()
}

/// Read a token from a file, or stdin when no path is supplied.
pub fn read_token(path: Option<&Path>) -> Result<String> {
    let mut token = String::new();
    match path {
        Some(path) => {
            token = fs::read_to_string(path)
                .with_context(|| format!("read token file {}", path.display()))?;
        }
        None => {
            std::io::stdin()
                .read_to_string(&mut token)
                .context("read enrollment token from stdin")?;
        }
    }
    Ok(token.trim().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalizes_center_url() {
        assert_eq!(
            normalize_center("https://fleet.example:8444/"),
            "https://fleet.example:8444"
        );
        assert_eq!(
            normalize_center(" https://fleet.example:8444 "),
            "https://fleet.example:8444"
        );
    }

    #[test]
    fn reads_token_file() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("token");
        fs::write(&path, " secret-token\n").unwrap();
        assert_eq!(read_token(Some(&path)).unwrap(), "secret-token");
    }
}

#[cfg(test)]
mod extra_tests {
    use super::*;

    #[tokio::test]
    async fn rejects_plain_http_center() {
        let dir = tempfile::tempdir().unwrap();
        let result = enroll(
            "http://fleet.example:8444",
            &dir.path().join("missing-ca.pem"),
            "token",
            "c1",
            "node-a",
            "",
            dir.path(),
        )
        .await;
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("https"));
    }
}
