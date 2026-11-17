use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;

const IDENTITY_DIR: &str = "identity";

/// Identity is the persisted agent registration.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Identity {
    pub agent_id: String,
    pub cluster: String,
    pub member: String,
    pub center_url: String,
    pub hostname: String,
}

/// IdentityFiles are the PEM materials next to the identity.
pub struct IdentityFiles {
    pub cert_pem: Vec<u8>,
    pub key_pem: Vec<u8>,
    pub ca_pem: Vec<u8>,
}

impl Identity {
    /// Load the identity and PEM files from a state directory.
    pub fn load(state_dir: &Path) -> Result<(Self, IdentityFiles)> {
        let dir = state_dir.join(IDENTITY_DIR);
        let identity: Identity =
            serde_json::from_slice(&fs::read(dir.join("agent.json")).context("read agent.json")?)
                .context("parse agent.json")?;
        let cert_pem = fs::read(dir.join("client.pem")).context("read client.pem")?;
        let key_pem = fs::read(dir.join("client.key")).context("read client.key")?;
        let ca_pem = fs::read(dir.join("ca.pem")).context("read ca.pem")?;

        Ok((
            identity,
            IdentityFiles {
                cert_pem,
                key_pem,
                ca_pem,
            },
        ))
    }

    /// Save the identity and PEM files atomically enough for a single host.
    pub fn save(
        state_dir: &Path,
        identity: &Identity,
        cert_pem: &[u8],
        key_pem: &[u8],
        ca_pem: &[u8],
    ) -> Result<()> {
        if identity.agent_id.is_empty() || identity.center_url.is_empty() {
            bail!("identity is incomplete");
        }
        let dir = state_dir.join(IDENTITY_DIR);
        fs::create_dir_all(&dir).context("create identity directory")?;
        set_mode(&dir, 0o700)?;

        write_file(
            &dir.join("agent.json"),
            &serde_json::to_vec_pretty(identity)?,
            0o600,
        )?;
        write_file(&dir.join("client.pem"), cert_pem, 0o644)?;
        write_file(&dir.join("client.key"), key_pem, 0o600)?;
        write_file(&dir.join("ca.pem"), ca_pem, 0o644)?;

        Ok(())
    }
}

fn write_file(path: &Path, data: &[u8], mode: u32) -> Result<()> {
    let tmp = path.with_extension("tmp");
    fs::write(&tmp, data).with_context(|| format!("write {}", tmp.display()))?;
    set_mode(&tmp, mode)?;
    fs::rename(&tmp, path).with_context(|| format!("rename {}", path.display()))?;
    Ok(())
}

#[cfg(unix)]
fn set_mode(path: &Path, mode: u32) -> Result<()> {
    use std::os::unix::fs::PermissionsExt;
    fs::set_permissions(path, fs::Permissions::from_mode(mode))
        .with_context(|| format!("chmod {}", path.display()))
}

#[cfg(not(unix))]
fn set_mode(_path: &Path, _mode: u32) -> Result<()> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn save_and_load_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let identity = Identity {
            agent_id: "agent-1".into(),
            cluster: "c1".into(),
            member: "node-a".into(),
            center_url: "https://fleet.example:8444".into(),
            hostname: "node-a".into(),
        };
        Identity::save(dir.path(), &identity, b"cert", b"key", b"ca").unwrap();
        let (loaded, files) = Identity::load(dir.path()).unwrap();
        assert_eq!(loaded.agent_id, "agent-1");
        assert_eq!(files.cert_pem, b"cert");
        assert_eq!(files.key_pem, b"key");
        assert_eq!(files.ca_pem, b"ca");
    }

    #[test]
    fn rejects_incomplete_identity() {
        let dir = tempfile::tempdir().unwrap();
        let identity = Identity {
            agent_id: String::new(),
            cluster: "c1".into(),
            member: "node-a".into(),
            center_url: "https://fleet.example:8444".into(),
            hostname: "node-a".into(),
        };
        assert!(Identity::save(dir.path(), &identity, b"c", b"k", b"ca").is_err());
    }
}
