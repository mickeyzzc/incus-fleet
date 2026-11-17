mod enroll;
mod facts;
mod helper;
mod identity;
mod proto;
mod runtime;
mod snapshot;

use std::path::PathBuf;

use anyhow::Result;
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
        Command::Run { state_dir, helper } => {
            runtime::run(&state_dir, &helper).await?;
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
