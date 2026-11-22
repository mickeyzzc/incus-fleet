use std::io::BufReader;
use std::path::Path;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use anyhow::{Context, Result, bail};
use serde::Deserialize;
use x509_parser::parse_x509_certificate;

use crate::identity::{Identity, IdentityFiles};

const RENEW_WINDOW: Duration = Duration::from_secs(30 * 24 * 60 * 60);

#[derive(Deserialize)]
struct RenewResponse {
    certificate: String,
    ca: String,
}

/// Renew the client certificate when it expires inside the renewal window.
/// Returns true when a new certificate was written.
pub async fn renew_if_needed(
    identity: &Identity,
    files: &IdentityFiles,
    state_dir: &Path,
) -> Result<bool> {
    let not_after = certificate_not_after(&files.cert_pem)?;
    if !needs_renewal(not_after, SystemTime::now(), RENEW_WINDOW) {
        return Ok(false);
    }

    let ca_cert = reqwest::Certificate::from_pem(&files.ca_pem).context("parse center CA")?;
    let mut identity_pem = files.cert_pem.clone();
    if !identity_pem.ends_with(b"\n") {
        identity_pem.push(b'\n');
    }
    identity_pem.extend_from_slice(&files.key_pem);
    let client_identity =
        reqwest::Identity::from_pem(&identity_pem).context("build client identity")?;
    let client = reqwest::Client::builder()
        .add_root_certificate(ca_cert)
        .identity(client_identity)
        .timeout(Duration::from_secs(20))
        .build()
        .context("build renewal client")?;

    let key = rcgen::KeyPair::from_pem(
        std::str::from_utf8(&files.key_pem).context("client key is not UTF-8")?,
    )
    .context("parse client key")?;
    let request = serde_json::json!({ "public_key": key.public_key_pem() });
    let response = client
        .post(format!("{}/api/v1/agent/renew", identity.center_url))
        .json(&request)
        .send()
        .await
        .context("send renewal request")?;
    let status = response.status();
    let body = response.text().await.context("read renewal response")?;
    if !status.is_success() {
        bail!("renewal failed: HTTP {status}: {body}");
    }
    let renewed: RenewResponse = serde_json::from_str(&body).context("parse renewal response")?;

    Identity::save(
        state_dir,
        identity,
        renewed.certificate.as_bytes(),
        &files.key_pem,
        renewed.ca.as_bytes(),
    )?;

    Ok(true)
}

fn certificate_not_after(cert_pem: &[u8]) -> Result<SystemTime> {
    let der = rustls_pemfile::certs(&mut BufReader::new(cert_pem))
        .next()
        .context("client certificate is empty")?
        .context("parse client certificate")?;
    let (_, cert) =
        parse_x509_certificate(&der).map_err(|err| anyhow::anyhow!("parse certificate: {err}"))?;
    let timestamp = cert.validity().not_after.timestamp();
    if timestamp <= 0 {
        bail!("certificate expiry is invalid");
    }

    Ok(UNIX_EPOCH + Duration::from_secs(timestamp as u64))
}

fn needs_renewal(not_after: SystemTime, now: SystemTime, window: Duration) -> bool {
    match not_after.duration_since(now) {
        Ok(remaining) => remaining <= window,
        Err(_) => true,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn renewal_window() {
        let now = UNIX_EPOCH + Duration::from_secs(1_000_000);
        assert!(needs_renewal(
            now + Duration::from_secs(10),
            now,
            RENEW_WINDOW
        ));
        assert!(!needs_renewal(now + RENEW_WINDOW * 2, now, RENEW_WINDOW));
        assert!(needs_renewal(
            now - Duration::from_secs(1),
            now,
            RENEW_WINDOW
        ));
    }

    #[test]
    fn parses_expiry() {
        let params = rcgen::CertificateParams::new(vec!["node-a".into()]).unwrap();
        let key = rcgen::KeyPair::generate().unwrap();
        let cert = params.self_signed(&key).unwrap();
        let not_after = certificate_not_after(cert.pem().as_bytes()).unwrap();
        assert!(not_after > SystemTime::now());
    }

    #[test]
    fn rejects_invalid_pem() {
        assert!(certificate_not_after(b"not-pem").is_err());
    }
}
