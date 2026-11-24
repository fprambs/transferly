//! Spike: can a Rust client stream a large upload from disk, interrupt it, resume it from the staged offset, download with `Range`
//! and follow the events WebSocket, with flat memory? Run against the isolated test stack; see docs/spikes/rust-streaming.md.

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Instant;

use anyhow::{bail, Context, Result};
use futures_util::{StreamExt, TryStreamExt};
use reqwest::cookie::{CookieStore, Jar};
use reqwest::{Client, StatusCode, Url};
use serde_json::{json, Value};
use tokio::io::{AsyncSeekExt, AsyncWriteExt};
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_util::io::ReaderStream;

/// The size of the file the spike uploads and downloads.
const SIZE: u64 = 512 << 20;
/// Where the upload is cut, to resume it afterwards.
const CUT_AT: u64 = SIZE / 2;
/// The remote path of the file, inside the SFTP user's chroot.
const REMOTE: &str = "/upload/spike.bin";

/// A signed-in client with its CSRF token, workspace and quick connection.
struct Session {
    http: Client,
    base: String,
    csrf: String,
    files: String,
}

/// Counters the events task fills while the spike runs.
#[derive(Default)]
struct Events {
    by_type: Mutex<std::collections::BTreeMap<String, u64>>,
    states: Mutex<Vec<String>>,
}

/// Runs the spike and prints a report.
#[tokio::main]
async fn main() -> Result<()> {
    let base = std::env::var("BASE").unwrap_or_else(|_| "http://transferly:8080".into());
    let jar = Arc::new(Jar::default());
    let http = Client::builder().cookie_provider(jar.clone()).build()?;
    let session = sign_in(http, &base).await?;
    let events = Arc::new(Events::default());
    follow_events(&base, &jar, events.clone()).await?;

    let local = "/tmp/spike.bin";
    let file = tokio::fs::File::create(local).await?;
    file.set_len(SIZE).await?;
    drop(file);
    let _ = session.http.delete(format!("{}?path={REMOTE}", session.files)).header("X-CSRF-Token", &session.csrf).send().await;

    let transfer = uuid::Uuid::new_v4().to_string();
    let started = Instant::now();
    if std::env::var("WHOLE").is_ok() {
        let entry = upload(&session, local, &transfer, "create", 0, None).await?;
        println!("whole upload: {} bytes, {:.1} MB/s", entry["size"], SIZE as f64 / started.elapsed().as_secs_f64() / 1e6);
        return Ok(());
    }
    let cut = upload(&session, local, &transfer, "create", 0, Some(CUT_AT)).await;
    println!("upload cut at {} MiB: {}", CUT_AT >> 20, cut.err().map(|e| format!("{e:#}")).unwrap_or("not cut".into()));

    let stopped = wait_stopped(&session, &transfer).await?;
    println!("server recorded the cut upload as {stopped} after {:.1} s", started.elapsed().as_secs_f64());
    let partial = format!("/upload/transferly-{transfer}.part");
    let staged = stat(&session, &partial).await?["size"].as_u64().context("size of the staged file")?;
    println!("staged file {partial}: {} MiB", staged >> 20);
    let entry = upload(&session, local, &transfer, "resume", staged, None).await?;
    let secs = started.elapsed().as_secs_f64();
    println!("resumed upload done: {} bytes, {:.1} MB/s overall", entry["size"], SIZE as f64 / secs / 1e6);
    if entry["size"].as_u64() != Some(SIZE) {
        bail!("the remote file has the wrong size");
    }

    let from = SIZE - (128 << 20);
    let started = Instant::now();
    let got = download(&session, from, "/tmp/spike.down").await?;
    println!("ranged download from {} MiB: {} MiB, {:.1} MB/s", from >> 20, got >> 20, got as f64 / started.elapsed().as_secs_f64() / 1e6);
    if got != SIZE - from {
        bail!("the ranged download has the wrong length");
    }

    session.http.delete(format!("{}?path={REMOTE}", session.files)).header("X-CSRF-Token", &session.csrf).send().await?.error_for_status()?;
    tokio::time::sleep(std::time::Duration::from_secs(1)).await;
    println!("events by type: {:?}", events.by_type.lock().unwrap());
    println!("transfer states: {:?}", events.states.lock().unwrap());
    println!("peak resident memory: {}", peak_memory());
    Ok(())
}

/// Creates the first account or signs in, sets up and unlocks the vault, and opens a quick SFTP connection, trusting its key.
async fn sign_in(http: Client, base: &str) -> Result<Session> {
    let credentials = json!({"email": "e2e@example.com", "password": "a long e2e password"});
    let mut response = http.post(format!("{base}/api/v1/setup")).json(&credentials).send().await?;
    if response.status() == StatusCode::CONFLICT {
        response = http.post(format!("{base}/api/v1/auth/login")).json(&credentials).send().await?;
    }
    let info: Value = response.error_for_status()?.json().await?;
    let csrf = info["csrfToken"].as_str().context("csrfToken")?.to_owned();
    let master = json!({"masterPassword": "a long e2e master password"});
    for step in ["setup", "unlock"] {
        http.post(format!("{base}/api/v1/vault/{step}")).header("X-CSRF-Token", &csrf).json(&master).send().await?;
    }
    let workspaces: Value = http.get(format!("{base}/api/v1/workspaces")).send().await?.error_for_status()?.json().await?;
    let workspace = workspaces[0]["id"].as_str().context("workspace")?.to_owned();
    let quick: Value = http
        .post(format!("{base}/api/v1/workspaces/{workspace}/quick-connections"))
        .header("X-CSRF-Token", &csrf)
        .json(&json!({"connector": "sftp", "config": {"host": "sftp", "port": "22", "username": "transferly", "auth": "password"}, "secrets": {"password": "transferly"}}))
        .send()
        .await?
        .error_for_status()?
        .json()
        .await?;
    let files = format!("{base}/api/v1/workspaces/{workspace}/sites/{}/files", quick["id"].as_str().context("quick id")?);
    let listing = http.get(format!("{files}?path=/")).send().await?;
    if listing.status() == StatusCode::CONFLICT {
        let problem: Value = listing.json().await?;
        http.post(format!("{base}/api/v1/workspaces/{workspace}/known-hosts"))
            .header("X-CSRF-Token", &csrf)
            .json(&json!({"host": "sftp", "port": 22, "keyType": problem["params"]["keyType"], "fingerprint": problem["params"]["fingerprint"]}))
            .send()
            .await?
            .error_for_status()?;
    }
    Ok(Session { http, base: base.to_owned(), csrf, files })
}

/// Opens the events stream with the session cookie in the upgrade request and counts what arrives.
async fn follow_events(base: &str, jar: &Jar, events: Arc<Events>) -> Result<()> {
    let cookie = jar.cookies(&Url::parse(base)?).context("session cookie")?;
    let mut request = format!("{}/api/v1/ws", base.replacen("http", "ws", 1)).into_client_request()?;
    request.headers_mut().insert("Cookie", cookie);
    let (socket, _) = tokio_tungstenite::connect_async(request).await?;
    tokio::spawn(async move {
        let (_, mut read) = socket.split();
        while let Some(Ok(message)) = read.next().await {
            let Ok(text) = message.into_text() else { continue };
            let Ok(event) = serde_json::from_str::<Value>(&text) else { continue };
            let kind = event["type"].as_str().unwrap_or("?").to_owned();
            if kind == "transfer.state" {
                events.states.lock().unwrap().push(format!("{}:{}", event["data"]["direction"], event["data"]["status"]));
            }
            *events.by_type.lock().unwrap().entry(kind).or_default() += 1;
        }
    });
    Ok(())
}

/// Streams the local file from offset to the remote path, counting bytes; with stop, fails the body after that many bytes.
async fn upload(session: &Session, local: &str, transfer: &str, mode: &str, offset: u64, stop: Option<u64>) -> Result<Value> {
    let mut file = tokio::fs::File::open(local).await?;
    file.seek(std::io::SeekFrom::Start(offset)).await?;
    let sent = Arc::new(AtomicU64::new(offset));
    let counter = sent.clone();
    let body = ReaderStream::with_capacity(file, 256 << 10).map(move |chunk| {
        let chunk = chunk?;
        let total = counter.fetch_add(chunk.len() as u64, Ordering::Relaxed) + chunk.len() as u64;
        match stop {
            Some(limit) if total >= limit => Err(std::io::Error::other("interrupted by the spike")),
            _ => Ok(chunk),
        }
    });
    let mut request = session
        .http
        .put(format!("{}/content", session.files))
        .header("X-CSRF-Token", &session.csrf)
        .query(&[("path", REMOTE), ("mode", mode), ("transferId", transfer), ("size", &SIZE.to_string())]);
    if mode == "resume" {
        request = request.query(&[("offset", offset)]);
    }
    let response = request.body(reqwest::Body::wrap_stream(body)).send().await?;
    let status = response.status();
    let value: Value = response.json().await.unwrap_or(Value::Null);
    if !status.is_success() {
        bail!("upload answered {status}: {value}");
    }
    Ok(value)
}

/// Polls the transfer record until the server stopped it, since a resume that starts earlier races with the cut upload.
async fn wait_stopped(session: &Session, transfer: &str) -> Result<String> {
    for _ in 0..600 {
        let record: Value = session.http.get(format!("{}/api/v1/transfers/{transfer}", session.base)).send().await?.json().await?;
        let status = record["status"].as_str().unwrap_or_default().to_owned();
        if ["failed", "interrupted", "canceled"].contains(&status.as_str()) {
            return Ok(status);
        }
        tokio::time::sleep(std::time::Duration::from_millis(100)).await;
    }
    bail!("the cut upload never stopped")
}

/// Describes one remote path.
async fn stat(session: &Session, path: &str) -> Result<Value> {
    Ok(session.http.get(format!("{}/entry", session.files)).query(&[("path", path)]).send().await?.error_for_status()?.json().await?)
}

/// Downloads the remote file from a byte offset into a local file, streaming, and returns the bytes written.
async fn download(session: &Session, from: u64, local: &str) -> Result<u64> {
    let response = session
        .http
        .get(format!("{}/content", session.files))
        .query(&[("path", REMOTE)])
        .header("Range", format!("bytes={from}-"))
        .send()
        .await?;
    if response.status() != StatusCode::PARTIAL_CONTENT {
        bail!("expected 206, got {}", response.status());
    }
    let mut out = tokio::fs::File::create(local).await?;
    let mut stream = response.bytes_stream().map_err(std::io::Error::other);
    let mut written = 0u64;
    while let Some(chunk) = stream.next().await {
        let chunk = chunk?;
        out.write_all(&chunk).await?;
        written += chunk.len() as u64;
    }
    let _ = &session.base;
    Ok(written)
}

/// Reads the peak resident memory of this process from /proc.
fn peak_memory() -> String {
    std::fs::read_to_string("/proc/self/status")
        .ok()
        .and_then(|s| s.lines().find(|l| l.starts_with("VmHWM")).map(|l| l.split_whitespace().skip(1).collect::<Vec<_>>().join(" ")))
        .unwrap_or_else(|| "unknown".into())
}
