use std::sync::{Arc, Mutex};

use subtle::ConstantTimeEq;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};
use tracing::{info, warn};

use crate::proto::v1::{MetricType, TelemetryBatch};

/// Configuration for the optional direct Prometheus endpoint.
#[derive(Clone, Debug)]
pub struct ServeConfig {
    pub listen: String,
    pub advertise: String,
    pub token: String,
}

/// Latest batch shared between the telemetry loop and the HTTP listener.
pub type Latest = Arc<Mutex<Option<TelemetryBatch>>>;

/// Serve /metrics until the task is cancelled.
pub async fn serve(config: ServeConfig, latest: Latest) {
    let Ok(listener) = TcpListener::bind(&config.listen).await else {
        warn!(address = %config.listen, "failed to bind agent metrics listener");
        return;
    };
    info!(address = %config.listen, advertise = %config.advertise, "agent metrics listener started");
    loop {
        let Ok((stream, _)) = listener.accept().await else {
            continue;
        };
        let token = config.token.clone();
        let latest = latest.clone();
        tokio::spawn(async move {
            if let Err(err) = handle(stream, &token, latest).await {
                warn!(error = %err, "metrics scrape failed");
            }
        });
    }
}

async fn handle(mut stream: TcpStream, token: &str, latest: Latest) -> std::io::Result<()> {
    let mut buffer = vec![0u8; 8192];
    let read = stream.read(&mut buffer).await?;
    let request = String::from_utf8_lossy(&buffer[..read]);
    let mut lines = request.lines();
    let request_line = lines.next().unwrap_or_default();
    let authorized = lines.any(|line| authorized(line, token));

    if !request_line.starts_with("GET /metrics ") {
        return respond(&mut stream, 404, "not found").await;
    }
    if !authorized || token.is_empty() {
        return respond(&mut stream, 401, "unauthorized").await;
    }

    let batch = latest.lock().ok().and_then(|guard| guard.clone());
    let body = batch.as_ref().map(render_prometheus).unwrap_or_default();
    let response = format!(
        "HTTP/1.1 200 OK\r\nContent-Type: text/plain; version=0.0.4; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    );
    stream.write_all(response.as_bytes()).await?;
    stream.shutdown().await
}

fn authorized(header: &str, token: &str) -> bool {
    if token.is_empty() {
        return false;
    }
    let lowered = header.to_ascii_lowercase();
    let Some(value) = lowered.strip_prefix("authorization:").map(str::trim) else {
        return false;
    };
    let expected = format!("bearer {token}");
    value.len() == expected.len() && value.as_bytes().ct_eq(expected.as_bytes()).into()
}

async fn respond(stream: &mut TcpStream, status: u16, body: &str) -> std::io::Result<()> {
    let reason = if status == 401 {
        "Unauthorized"
    } else {
        "Not Found"
    };
    let response = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: text/plain\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    );
    stream.write_all(response.as_bytes()).await?;
    stream.shutdown().await
}

/// Render a batch in Prometheus text exposition format.
pub fn render_prometheus(batch: &TelemetryBatch) -> String {
    let mut out = String::new();
    for series in &batch.series {
        let kind = if series.r#type == MetricType::Counter as i32 {
            "counter"
        } else {
            "gauge"
        };
        out.push_str(&format!("# TYPE {} {kind}\n", series.name));
        for sample in &series.samples {
            out.push_str(&series.name);
            if !series.labels.is_empty() {
                let mut labels = series.labels.iter().collect::<Vec<_>>();
                labels.sort_by(|a, b| a.0.cmp(b.0));
                out.push('{');
                for (index, (key, value)) in labels.iter().enumerate() {
                    if index > 0 {
                        out.push(',');
                    }
                    out.push_str(key);
                    out.push_str("=\"");
                    out.push_str(
                        &value
                            .replace('\\', "\\\\")
                            .replace('"', "\\\"")
                            .replace('\n', "\\n"),
                    );
                    out.push('"');
                }
                out.push('}');
            }
            out.push(' ');
            out.push_str(&sample.value.to_string());
            out.push(' ');
            out.push_str(&sample.at_ms.to_string());
            out.push('\n');
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proto::v1::Sample;
    use std::collections::HashMap;

    #[test]
    fn checks_bearer_token() {
        assert!(authorized("Authorization: Bearer secret", "secret"));
        assert!(authorized("authorization: bearer secret", "secret"));
        assert!(!authorized("Authorization: Bearer other", "secret"));
        assert!(!authorized("Authorization: Bearer secret", ""));
        assert!(!authorized("X-Other: value", "secret"));
    }

    #[test]
    fn renders_prometheus_text() {
        let batch = TelemetryBatch {
            agent_id: "agent-1".into(),
            interval_ms: 10_000,
            series: vec![crate::proto::v1::Series {
                name: "fleet_host_load".into(),
                labels: HashMap::from([("window".to_string(), "1".to_string())]),
                r#type: MetricType::Gauge as i32,
                samples: vec![Sample {
                    at_ms: 1000,
                    value: 0.5,
                }],
            }],
        };
        let text = render_prometheus(&batch);
        assert!(text.contains("# TYPE fleet_host_load gauge"));
        assert!(text.contains("fleet_host_load{window=\"1\"} 0.5 1000"));
    }
}
