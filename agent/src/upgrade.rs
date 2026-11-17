use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::process::Command;

use anyhow::{Context, Result, bail};
use base64::Engine;
use ed25519_dalek::{Signature, VerifyingKey};

/// Verify a detached Ed25519 signature, install the binary atomically, and
/// restore the previous binary if `--version` fails.
pub fn verify_and_install(
    binary: &Path,
    signature: &Path,
    public_key: &Path,
    target: &Path,
) -> Result<()> {
    let payload = fs::read(binary).with_context(|| format!("read {}", binary.display()))?;
    let sig_text = fs::read_to_string(signature).context("read signature")?;
    let key_text = fs::read_to_string(public_key).context("read public key")?;
    let key_bytes = decode_key(key_text.trim())?;
    let key = VerifyingKey::from_bytes(&key_bytes).context("parse Ed25519 public key")?;
    let sig_bytes = base64::engine::general_purpose::STANDARD
        .decode(sig_text.trim())
        .context("decode signature")?;
    let signature = Signature::from_slice(&sig_bytes).context("parse signature")?;
    key.verify_strict(&payload, &signature)
        .context("signature verification failed")?;

    let backup = target.with_extension("prev");
    let staged = target.with_extension("new");
    if target.exists() {
        fs::copy(target, &backup).context("backup current binary")?;
    }
    fs::write(&staged, &payload).context("write staged binary")?;
    fs::set_permissions(&staged, fs::Permissions::from_mode(0o755))
        .context("chmod staged binary")?;
    fs::rename(&staged, target).context("install staged binary")?;

    let healthy = Command::new(target)
        .arg("--version")
        .output()
        .map(|output| output.status.success())
        .unwrap_or(false);
    if !healthy {
        let _ = fs::remove_file(target);
        if backup.exists() {
            fs::copy(&backup, target).context("restore previous binary")?;
            fs::set_permissions(target, fs::Permissions::from_mode(0o755))?;
        }
        bail!("new agent binary failed its --version health check; previous binary restored");
    }
    let _ = fs::remove_file(&backup);
    Ok(())
}

/// Restart the systemd unit after a successful install. It is a no-op outside
/// systemd or when INCUS_FLEET_UPGRADE_NO_RESTART=1.
pub fn restart_service() {
    if !should_restart(Path::new("/")) {
        return;
    }
    match Command::new("systemctl")
        .args(["try-restart", "incus-fleet-agent"])
        .status()
    {
        Ok(status) if status.success() => {}
        Ok(status) => eprintln!("systemctl try-restart exited with {status}"),
        Err(err) => eprintln!("systemctl try-restart failed: {err}"),
    }
}

fn should_restart(root: &Path) -> bool {
    if std::env::var("INCUS_FLEET_UPGRADE_NO_RESTART").as_deref() == Ok("1") {
        return false;
    }
    root.join("run/systemd/system").exists()
}

fn decode_key(value: &str) -> Result<[u8; 32]> {
    if let Ok(bytes) = hex_decode(value)
        && bytes.len() == 32
    {
        let mut key = [0u8; 32];
        key.copy_from_slice(&bytes);
        return Ok(key);
    }
    if let Ok(bytes) = base64::engine::general_purpose::STANDARD.decode(value)
        && bytes.len() == 32
    {
        let mut key = [0u8; 32];
        key.copy_from_slice(&bytes);
        return Ok(key);
    }
    bail!("public key must be 32 raw bytes in hex or base64")
}

fn hex_decode(value: &str) -> Result<Vec<u8>> {
    if !value.len().is_multiple_of(2) {
        bail!("odd hex length");
    }
    let mut out = Vec::with_capacity(value.len() / 2);
    for chunk in value.as_bytes().chunks(2) {
        let text = std::str::from_utf8(chunk)?;
        out.push(u8::from_str_radix(text, 16)?);
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::{Signer, SigningKey};

    fn setup() -> (tempfile::TempDir, SigningKey, [u8; 32]) {
        let dir = tempfile::tempdir().unwrap();
        let key = SigningKey::from_bytes(&[7u8; 32]);
        let public = key.verifying_key().to_bytes();
        (dir, key, public)
    }

    fn sign(key: &SigningKey, data: &[u8]) -> String {
        base64::engine::general_purpose::STANDARD.encode(key.sign(data).to_bytes())
    }

    #[test]
    fn verifies_and_installs() {
        let (dir, key, public) = setup();
        let binary = dir.path().join("new-agent");
        let target = dir.path().join("agent");
        fs::write(&binary, "#!/bin/sh\nexit 0\n").unwrap();
        fs::write(&target, "old").unwrap();
        fs::write(
            dir.path().join("sig"),
            sign(&key, &fs::read(&binary).unwrap()),
        )
        .unwrap();
        fs::write(dir.path().join("pub"), hex_encode(&public)).unwrap();

        verify_and_install(
            &binary,
            &dir.path().join("sig"),
            &dir.path().join("pub"),
            &target,
        )
        .unwrap();
        assert_eq!(fs::read_to_string(&target).unwrap(), "#!/bin/sh\nexit 0\n");
        assert!(!dir.path().join("agent.prev").exists());
    }

    #[test]
    fn rejects_tampering() {
        let (dir, key, public) = setup();
        let binary = dir.path().join("new-agent");
        let target = dir.path().join("agent");
        fs::write(&binary, "#!/bin/sh\nexit 0\n").unwrap();
        fs::write(&target, "old").unwrap();
        fs::write(dir.path().join("sig"), sign(&key, b"different")).unwrap();
        fs::write(dir.path().join("pub"), hex_encode(&public)).unwrap();
        assert!(
            verify_and_install(
                &binary,
                &dir.path().join("sig"),
                &dir.path().join("pub"),
                &target
            )
            .is_err()
        );
        assert_eq!(fs::read_to_string(&target).unwrap(), "old");
    }

    #[test]
    fn detects_systemd_root() {
        let dir = tempfile::tempdir().unwrap();
        assert!(!should_restart(dir.path()));
        fs::create_dir_all(dir.path().join("run/systemd/system")).unwrap();
        assert!(should_restart(dir.path()));
    }

    #[test]
    fn restores_after_failed_health_check() {
        let (dir, key, public) = setup();
        let binary = dir.path().join("new-agent");
        let target = dir.path().join("agent");
        fs::write(&binary, "#!/bin/sh\nexit 3\n").unwrap();
        fs::write(&target, "old").unwrap();
        fs::write(
            dir.path().join("sig"),
            sign(&key, &fs::read(&binary).unwrap()),
        )
        .unwrap();
        fs::write(dir.path().join("pub"), hex_encode(&public)).unwrap();
        assert!(
            verify_and_install(
                &binary,
                &dir.path().join("sig"),
                &dir.path().join("pub"),
                &target
            )
            .is_err()
        );
        assert_eq!(fs::read_to_string(&target).unwrap(), "old");
    }

    fn hex_encode(bytes: &[u8]) -> String {
        bytes.iter().map(|byte| format!("{byte:02x}")).collect()
    }
}
