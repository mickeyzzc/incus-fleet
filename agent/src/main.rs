mod cgroup;
mod ebpf;
mod enroll;
mod facts;
mod helper;
mod histogram;
mod identity;
mod metrics;
mod metrics_server;
mod proto;
mod renew;
mod runtime;
mod snapshot;
mod upgrade;

use std::path::PathBuf;

use anyhow::{Result, bail};
use clap::{Parser, Subcommand};
use tracing::info;

#[derive(Parser)]
#[command(name = "incus-fleet-agent", version, about = "Incus Fleet host agent")]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Enroll this host with the center and write identity material.
    Enroll {
        /// Center base URL, for example https://fleet.example:8444.
        #[arg(long)]
        center: String,
        /// Center CA certificate in PEM form.
        #[arg(long)]
        ca: PathBuf,
        /// File containing the one-time enrollment token. Reads stdin if omitted.
        #[arg(long)]
        token_file: Option<PathBuf>,
        #[arg(long)]
        cluster: String,
        #[arg(long)]
        member: String,
        #[arg(long, default_value = "")]
        name: String,
        #[arg(long, default_value = "/var/lib/incus-fleet-agent")]
        state_dir: PathBuf,
    },
    /// Run the agent and reconnect to the center forever.
    Run {
        #[arg(long, default_value = "/var/lib/incus-fleet-agent")]
        state_dir: PathBuf,
        #[arg(long, default_value = "/usr/libexec/incus-fleet-helper")]
        helper: PathBuf,
        /// Optional eBPF object built from agent/bpf/metrics.bpf.c.
        #[arg(long)]
        ebpf_object: Option<PathBuf>,
        /// Optional direct Prometheus listener, for example 0.0.0.0:9100.
        #[arg(long)]
        metrics_listen: Option<String>,
        /// Address advertised to center HTTP SD; defaults to --metrics-listen.
        #[arg(long, default_value = "")]
        metrics_advertise: String,
        /// File containing the bearer token for /metrics; falls back to
        /// INCUS_FLEET_METRICS_TOKEN.
        #[arg(long)]
        metrics_token_file: Option<PathBuf>,
    },
    /// Verify and install a signed agent binary with rollback on a failed health check.
    Upgrade {
        #[arg(long)]
        binary: PathBuf,
        #[arg(long)]
        signature: PathBuf,
        /// Raw 32-byte Ed25519 public key, hex or base64.
        #[arg(long)]
        public_key: PathBuf,
        #[arg(long, default_value = "/usr/local/bin/incus-fleet-agent")]
        target: PathBuf,
        /// Do not restart incus-fleet-agent.service after installing.
        #[arg(long)]
        no_restart: bool,
    },
    /// Load the eBPF object once and print the collected host counters.
    EbpfSelfTest {
        #[arg(long)]
        object: PathBuf,
        /// Keep the programs attached for this many seconds before exiting.
        #[arg(long, default_value_t = 0)]
        hold_secs: u64,
    },
    /// Print the persisted identity.
    Status {
        #[arg(long, default_value = "/var/lib/incus-fleet-agent")]
        state_dir: PathBuf,
    },
}

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_max_level(tracing::Level::INFO)
        .init();

    let cli = Cli::parse();
    match cli.command {
        Command::Enroll {
            center,
            ca,
            token_file,
            cluster,
            member,
            name,
            state_dir,
        } => {
            let token = enroll::read_token(token_file.as_deref())?;
            let agent_id =
                enroll::enroll(&center, &ca, &token, &cluster, &member, &name, &state_dir).await?;
            info!(agent = %agent_id, "enrolled");
            println!("{agent_id}");
        }
        Command::Run {
            state_dir,
            helper,
            ebpf_object,
            metrics_listen,
            metrics_advertise,
            metrics_token_file,
        } => {
            let metrics = match metrics_listen {
                Some(listen) => {
                    let token = match metrics_token_file {
                        Some(path) => std::fs::read_to_string(path)?,
                        None => std::env::var("INCUS_FLEET_METRICS_TOKEN").unwrap_or_default(),
                    };
                    if token.trim().is_empty() {
                        bail!("metrics token is required when --metrics-listen is set");
                    }
                    let advertise = if metrics_advertise.trim().is_empty() {
                        listen.clone()
                    } else {
                        metrics_advertise
                    };
                    Some(metrics_server::ServeConfig {
                        listen,
                        advertise,
                        token: token.trim().to_string(),
                    })
                }
                None => None,
            };
            runtime::run(&state_dir, &helper, ebpf_object.as_deref(), metrics).await?;
        }
        Command::Upgrade {
            binary,
            signature,
            public_key,
            target,
            no_restart,
        } => {
            upgrade::verify_and_install(&binary, &signature, &public_key, &target)?;
            println!("installed {}", target.display());
            if !no_restart {
                upgrade::restart_service();
            }
        }
        Command::EbpfSelfTest { object, hold_secs } => {
            if !ebpf::available(Some(&object)) {
                bail!("eBPF object or kernel BTF is unavailable");
            }
            let collector = ebpf::Collector::open(&object).map_err(|err| anyhow::anyhow!(err))?;
            let initial = collector.collect();
            println!("initial_tcp_retransmits={}", initial.tcp_retransmits);
            println!("initial_oom_kills={}", initial.oom_kills);
            if hold_secs > 0 {
                tokio::time::sleep(std::time::Duration::from_secs(hold_secs)).await;
                let final_snapshot = collector.collect();
                println!("tcp_retransmits={}", final_snapshot.tcp_retransmits);
                println!("oom_kills={}", final_snapshot.oom_kills);
            }
        }
        Command::Status { state_dir } => {
            let (identity, _) = identity::Identity::load(&state_dir)?;
            println!("agent_id={}", identity.agent_id);
            println!("cluster={}", identity.cluster);
            println!("member={}", identity.member);
            println!("center={}", identity.center_url);
            println!("hostname={}", identity.hostname);
        }
    }

    Ok(())
}
