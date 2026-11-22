use std::io::BufReader;
use std::path::Path;
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::{Context, Result, bail};
use futures_util::{SinkExt, StreamExt};
use prost::Message;
use rustls::ClientConfig;
use tokio::net::TcpStream;
use tokio_tungstenite::tungstenite::Message as WsMessage;
use tokio_tungstenite::{
    Connector, MaybeTlsStream, WebSocketStream, connect_async_tls_with_config,
};
use tracing::{info, warn};

use crate::facts;
use crate::identity::Identity;
use crate::metrics;
use crate::proto::v1::{
    Envelope, Heartbeat, Hello, IncusBootstrapResult, NodenetApplyResult, NodenetApplySpec,
    TaskResult, TaskState, TaskType, envelope,
};
use crate::snapshot;

type Socket = WebSocketStream<MaybeTlsStream<TcpStream>>;

/// Run the agent forever, reconnecting with exponential backoff.
pub async fn run(
    state_dir: &Path,
    helper: &Path,
    ebpf_object: Option<&Path>,
    metrics: Option<crate::metrics_server::ServeConfig>,
) -> Result<()> {
    let bootstrap_ca = state_dir.join("identity/ca.pem");
    let (identity, files) = Identity::load(state_dir)?;
    if let Err(err) = crate::renew::renew_if_needed(&identity, &files, state_dir).await {
        warn!(error = %err, "certificate renewal failed; continuing with the current certificate");
    }
    let (identity, files) = Identity::load(state_dir)?;
    let tls = client_tls(&files.ca_pem, &files.cert_pem, &files.key_pem)?;
    let connect_url = websocket_url(&identity.center_url)?;
    let latest = std::sync::Arc::new(std::sync::Mutex::new(None));
    let advertise = metrics.as_ref().map(|config| config.advertise.clone());
    if let Some(config) = metrics {
        let latest = latest.clone();
        tokio::spawn(crate::metrics_server::serve(config, latest));
    }

    let mut backoff = Duration::from_secs(1);
    loop {
        match session(
            &connect_url,
            &identity,
            tls.clone(),
            helper,
            &bootstrap_ca,
            ebpf_object,
            &latest,
            advertise.as_deref(),
        )
        .await
        {
            Ok(()) => {
                info!("agent session closed");
                backoff = Duration::from_secs(1);
            }
            Err(err) => {
                warn!(error = %err, "agent session failed");
            }
        }
        tokio::time::sleep(backoff).await;
        backoff = (backoff * 2).min(Duration::from_secs(60));
    }
}

fn client_tls(ca_pem: &[u8], cert_pem: &[u8], key_pem: &[u8]) -> Result<Arc<ClientConfig>> {
    let mut roots = rustls::RootCertStore::empty();
    for cert in rustls_pemfile::certs(&mut BufReader::new(ca_pem)) {
        roots.add(cert.context("parse CA certificate")?)?;
    }
    let certs = rustls_pemfile::certs(&mut BufReader::new(cert_pem))
        .collect::<Result<Vec<_>, _>>()
        .context("parse client certificate")?;
    let key = rustls_pemfile::private_key(&mut BufReader::new(key_pem))
        .context("parse client key")?
        .context("client key is empty")?;
    let config = ClientConfig::builder()
        .with_root_certificates(roots)
        .with_client_auth_cert(certs, key)
        .context("build TLS client config")?;

    Ok(Arc::new(config))
}

fn websocket_url(center_url: &str) -> Result<String> {
    let mut url = url::Url::parse(center_url).context("parse center URL")?;
    match url.scheme() {
        "https" => {
            url.set_scheme("wss").expect("https to wss");
        }
        "http" => {
            url.set_scheme("ws").expect("http to ws");
        }
        "wss" | "ws" => {}
        other => bail!("unsupported center URL scheme {other}"),
    }
    url.set_path("/api/v1/agent/connect");
    url.set_query(None);

    Ok(url.to_string())
}

#[allow(clippy::too_many_arguments)]
async fn session(
    url: &str,
    identity: &Identity,
    tls: Arc<ClientConfig>,
    helper: &Path,
    bootstrap_ca: &Path,
    ebpf_object: Option<&Path>,
    latest: &crate::metrics_server::Latest,
    advertise: Option<&str>,
) -> Result<()> {
    let (mut ws, _) = tokio::time::timeout(
        Duration::from_secs(10),
        connect_async_tls_with_config(url, None, false, Some(Connector::Rustls(tls))),
    )
    .await
    .context("connect timeout")?
    .context("connect to center")?;

    let hello = Hello {
        agent_id: identity.agent_id.clone(),
        version: env!("CARGO_PKG_VERSION").to_string(),
        os: facts::collect().await.os,
        arch: std::env::consts::ARCH.to_string(),
        kernel: read_trim("/proc/sys/kernel/osrelease"),
        hostname: identity.hostname.clone(),
        capabilities: vec!["nodenet.snapshot".into()],
        started_at_ms: now_ms(),
    };
    send(&mut ws, mk_envelope(envelope::Body::Hello(hello))).await?;

    let mut current_facts = facts::collect().await;
    if let Some(address) = advertise {
        current_facts
            .extra
            .insert("metrics_address".into(), address.to_string());
    }
    send(
        &mut ws,
        mk_envelope(envelope::Body::Facts(current_facts.to_proto())),
    )
    .await?;

    let mut heartbeat = tokio::time::interval(Duration::from_secs(15));
    heartbeat.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    let mut telemetry_tick = tokio::time::interval(Duration::from_secs(10));
    telemetry_tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    let mut telemetry = metrics::Collector::with_ebpf(ebpf_object);
    let started = Instant::now();

    loop {
        tokio::select! {
            _ = heartbeat.tick() => {
                send(&mut ws, mk_envelope(envelope::Body::Heartbeat(Heartbeat {
                        uptime_ms: started.elapsed().as_millis() as i64,
                        load1: read_load(),
                        mem_total: 0,
                        mem_used: 0,
                        incus_version: String::new(),
                        running_tasks: 0,
                    }))).await?;
            }
            _ = telemetry_tick.tick() => {
                let batch = telemetry.collect(&identity.agent_id);
                if let Ok(mut guard) = latest.lock() {
                    *guard = Some(batch.clone());
                }
                send(&mut ws, mk_envelope(envelope::Body::Telemetry(batch))).await?;
            }
            message = ws.next() => {
                let Some(message) = message else { break; };
                let message = message.context("read websocket")?;
                match message {
                    WsMessage::Binary(data) => {
                        let envelope = Envelope::decode(data.as_ref()).context("decode envelope")?;
                        if let Some(envelope::Body::Task(task)) = envelope.body {
                            let result = handle_task(task, helper, bootstrap_ca).await;
                            send(&mut ws, mk_envelope(envelope::Body::Result(result))).await?;
                        }
                    }
                    WsMessage::Close(_) => break,
                    WsMessage::Ping(_) | WsMessage::Pong(_) | WsMessage::Text(_) | WsMessage::Frame(_) => {}
                }
            }
        }
    }

    Ok(())
}

async fn handle_task(
    task: crate::proto::v1::TaskDispatch,
    helper: &Path,
    bootstrap_ca: &Path,
) -> TaskResult {
    let task_id = task.task_id.clone();
    match task.r#type() {
        TaskType::NodenetSnapshot => match snapshot::collect().await {
            Ok(snapshot) => TaskResult {
                task_id,
                state: TaskState::Succeeded as i32,
                payload: snapshot.encode_to_vec(),
                error_code: String::new(),
                error: String::new(),
                at_ms: now_ms(),
                plan_hash: task.plan_hash,
            },
            Err(err) => TaskResult {
                task_id,
                state: TaskState::Failed as i32,
                payload: Vec::new(),
                error_code: "snapshot_failed".into(),
                error: err.to_string(),
                at_ms: now_ms(),
                plan_hash: task.plan_hash,
            },
        },
        TaskType::NodenetApply | TaskType::NodenetConfirm | TaskType::NodenetRollback => {
            let mut spec = if task.payload.is_empty() {
                NodenetApplySpec::default()
            } else {
                match NodenetApplySpec::decode(task.payload.as_slice()) {
                    Ok(spec) => spec,
                    Err(err) => {
                        return TaskResult {
                            task_id,
                            state: TaskState::Failed as i32,
                            payload: Vec::new(),
                            error_code: "invalid_spec".into(),
                            error: err.to_string(),
                            at_ms: now_ms(),
                            plan_hash: task.plan_hash,
                        };
                    }
                }
            };
            if spec.task_id.is_empty() {
                spec.task_id = task.task_id.clone();
            }
            let request = crate::helper::HelperRequest::from_spec(&spec);
            let result = match crate::helper::invoke(
                helper.to_string_lossy().as_ref(),
                &request,
                Some(bootstrap_ca),
            )
            .await
            {
                Ok(response) => NodenetApplyResult {
                    ok: true,
                    action: response.action,
                    task_id: response.task_id,
                    backend: response.backend,
                    persistence: response.persistence,
                    error: String::new(),
                },
                Err(err) => NodenetApplyResult {
                    ok: false,
                    action: spec.action,
                    task_id: spec.task_id,
                    backend: "helper".into(),
                    persistence: String::new(),
                    error: err.to_string(),
                },
            };
            TaskResult {
                task_id,
                state: if result.ok {
                    TaskState::Succeeded as i32
                } else {
                    TaskState::Failed as i32
                },
                payload: result.encode_to_vec(),
                error_code: if result.ok {
                    String::new()
                } else {
                    "helper_failed".into()
                },
                error: result.error.clone(),
                at_ms: now_ms(),
                plan_hash: task.plan_hash,
            }
        }
        TaskType::IncusBootstrap => {
            let mut request = crate::helper::HelperRequest::from_spec(&NodenetApplySpec::default());
            request.action = "incus-bootstrap".into();
            request.task_id = task.task_id.clone();
            request.spec = String::from_utf8_lossy(&task.payload).to_string();
            let result = match crate::helper::invoke(
                helper.to_string_lossy().as_ref(),
                &request,
                Some(bootstrap_ca),
            )
            .await
            {
                Ok(response) => IncusBootstrapResult {
                    ok: true,
                    task_id: response.task_id,
                    mode: "bootstrap".into(),
                    message: response.message,
                    steps: response.steps,
                },
                Err(err) => IncusBootstrapResult {
                    ok: false,
                    task_id: task.task_id.clone(),
                    mode: "bootstrap".into(),
                    message: err.to_string(),
                    steps: Vec::new(),
                },
            };
            TaskResult {
                task_id,
                state: if result.ok {
                    TaskState::Succeeded as i32
                } else {
                    TaskState::Failed as i32
                },
                payload: result.encode_to_vec(),
                error_code: if result.ok {
                    String::new()
                } else {
                    "bootstrap_failed".into()
                },
                error: result.message.clone(),
                at_ms: now_ms(),
                plan_hash: task.plan_hash,
            }
        }
        _ => TaskResult {
            task_id,
            state: TaskState::Failed as i32,
            payload: Vec::new(),
            error_code: "unsupported_task".into(),
            error: "unsupported task type".into(),
            at_ms: now_ms(),
            plan_hash: task.plan_hash,
        },
    }
}

async fn send(ws: &mut Socket, envelope: Envelope) -> Result<()> {
    ws.send(WsMessage::Binary(envelope.encode_to_vec().into()))
        .await
        .context("send websocket envelope")
}

fn read_trim(path: &str) -> String {
    std::fs::read_to_string(path)
        .unwrap_or_default()
        .trim()
        .to_string()
}

fn read_load() -> f64 {
    std::fs::read_to_string("/proc/loadavg")
        .ok()
        .and_then(|value| value.split_whitespace().next()?.parse().ok())
        .unwrap_or(0.0)
}

fn now_ms() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

fn mk_envelope(body: envelope::Body) -> Envelope {
    Envelope {
        message_id: String::new(),
        sent_at_ms: now_ms(),
        body: Some(body),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn builds_websocket_url() {
        assert_eq!(
            websocket_url("https://fleet.example:8444/").unwrap(),
            "wss://fleet.example:8444/api/v1/agent/connect"
        );
        assert_eq!(
            websocket_url("http://127.0.0.1:8080").unwrap(),
            "ws://127.0.0.1:8080/api/v1/agent/connect"
        );
        assert!(websocket_url("ftp://example.com").is_err());
    }

    #[test]
    fn builds_tls_config() {
        // Real PEMs are exercised by the Go enrollment tests; this asserts the
        // parser fails cleanly on invalid material.
        assert!(client_tls(b"not-pem", b"not-pem", b"not-pem").is_err());
    }
}
