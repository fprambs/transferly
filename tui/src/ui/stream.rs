//! The event stream of the server (`/api/v1/ws`, ADR-0016 and ADR-0011): while signed in, the client listens for what a server asks
//! while a site connects (`connection.prompt`, `connection.prompt_closed`) and for the steps of each connection (`connection.log`), and
//! hands them to the interface as replies. It connects with the bearer token, verifies certificates as the API does, and connects again
//! with a growing pause when the stream drops, until the server refuses the session.

use std::sync::Arc;
use std::time::Duration;

use futures::StreamExt;
use serde::Deserialize;
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::http::{HeaderValue, StatusCode};
use tokio_tungstenite::tungstenite::{Error as WsError, Message};
use url::Url;
use uuid::Uuid;

use super::app::Event;
use super::home::{LogLine, Prompt, Reply, Session};
use super::transfers::Row;

/// The longest pause between two attempts to connect.
const MOST_PAUSE: Duration = Duration::from_secs(30);

/// A message of the stream: its type and its payload.
#[derive(Deserialize)]
struct Wire {
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    data: serde_json::Value,
}

/// A question of a prompt, as the stream sends it.
#[derive(Deserialize)]
struct Question {
    text: String,
    echo: bool,
}

/// The payload of `connection.prompt`.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct PromptData {
    id: Uuid,
    workspace_id: Uuid,
    site_id: Uuid,
    #[serde(default)]
    name: String,
    #[serde(default)]
    instruction: String,
    questions: Vec<Question>,
}

/// The payload of the first message: the transfers running when the stream connects.
#[derive(Deserialize)]
struct SnapshotData {
    transfers: Vec<Row>,
}

/// The payload of `transfer.progress`.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ProgressData {
    id: Uuid,
    bytes_done: u64,
    #[serde(default)]
    total_bytes: Option<u64>,
}

/// The payload of `connection.prompt_closed`.
#[derive(Deserialize)]
struct ClosedData {
    id: Uuid,
}

/// The payload of `connection.log`.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct LogData {
    site_id: Uuid,
    time: String,
    level: String,
    step: String,
    #[serde(default)]
    params: std::collections::BTreeMap<String, String>,
}

/// The reply of a message of the stream, or none for the messages the terminal does not use.
#[must_use]
pub fn reply(text: &str) -> Option<Reply> {
    let message: Wire = serde_json::from_str(text).ok()?;
    match message.kind.as_str() {
        "connection.prompt" => {
            let data: PromptData = serde_json::from_value(message.data).ok()?;
            Some(Reply::Prompt(Prompt {
                id: data.id,
                workspace: data.workspace_id,
                site: data.site_id,
                name: data.name,
                instruction: data.instruction,
                questions: data
                    .questions
                    .into_iter()
                    .map(|question| (question.text, question.echo))
                    .collect(),
            }))
        }
        "snapshot" => {
            let data: SnapshotData = serde_json::from_value(message.data).ok()?;
            Some(Reply::Snapshot(data.transfers))
        }
        "transfer.state" => serde_json::from_value::<Row>(message.data)
            .ok()
            .map(Reply::TransferState),
        "transfer.progress" => {
            let data: ProgressData = serde_json::from_value(message.data).ok()?;
            Some(Reply::Progress {
                id: data.id,
                done: data.bytes_done,
                total: data.total_bytes,
                at: std::time::Instant::now(),
            })
        }
        "connection.prompt_closed" => serde_json::from_value::<ClosedData>(message.data)
            .ok()
            .map(|data| Reply::PromptClosed(data.id)),
        "connection.log" => {
            let data: LogData = serde_json::from_value(message.data).ok()?;
            Some(Reply::Log(LogLine {
                site: data.site_id,
                time: data.time.parse().ok(),
                level: data.level,
                step: data.step,
                params: data.params.into_iter().collect(),
            }))
        }
        _ => None,
    }
}

/// The address of the stream of a server: `ws` for `http`, `wss` for `https`.
///
/// # Panics
///
/// The server is not an http or https address, which the address of a signed-in server always is.
#[must_use]
pub fn address(server: &Url) -> Url {
    let mut url = server.join("/api/v1/ws").expect("the path of the stream");
    let scheme = if server.scheme() == "https" {
        "wss"
    } else {
        "ws"
    };
    url.set_scheme(scheme).expect("a scheme of WebSocket");
    url
}

/// The connector of secure streams: rustls with the certificates of the system, as the API verifies them.
#[rustfmt::skip]
fn connector() -> Option<tokio_tungstenite::Connector> {
    use rustls_platform_verifier::BuilderVerifierExt;
    let provider = Arc::new(rustls::crypto::aws_lc_rs::default_provider());
    let config = rustls::ClientConfig::builder_with_provider(provider).with_safe_default_protocol_versions().ok()?.with_platform_verifier().ok()?.with_no_client_auth();
    Some(tokio_tungstenite::Connector::Rustls(Arc::new(config)))
}

/// Listens to the stream of a session and hands its replies to the interface, connecting again after a pause that doubles up to 30
/// seconds when it drops; it ends when the server refuses the session or the interface stops listening.
pub async fn listen(session: Session, results: mpsc::Sender<Event>, first_pause: Duration) {
    let mut pause = first_pause;
    loop {
        match connect(&session).await {
            Ok(mut stream) => {
                pause = first_pause;
                while let Some(Ok(message)) = stream.next().await {
                    if let Message::Text(text) = message
                        && let Some(reply) = reply(&text)
                    {
                        let _ = results.send(Event::Replied(reply)).await;
                    }
                }
            }
            Err(WsError::Http(response)) if response.status() == StatusCode::UNAUTHORIZED => return,
            Err(_) => {}
        }
        if results.is_closed() {
            return;
        }
        tokio::time::sleep(pause).await;
        pause = (pause * 2).min(MOST_PAUSE);
    }
}

/// Opens the stream with the bearer token of the session.
async fn connect(
    session: &Session,
) -> Result<
    tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>,
    WsError,
> {
    let mut request = address(&session.server).as_str().into_client_request()?;
    let bearer = HeaderValue::from_str(&format!("Bearer {}", session.token))
        .map_err(tokio_tungstenite::tungstenite::http::Error::from)?;
    request.headers_mut().insert("Authorization", bearer);
    let (stream, _) =
        tokio_tungstenite::connect_async_tls_with_config(request, None, false, connector()).await?;
    Ok(stream)
}

#[cfg(test)]
mod tests {
    use futures::SinkExt;
    use tokio::io::AsyncWriteExt;
    use tokio::net::TcpListener;
    use tokio_tungstenite::tungstenite::handshake::server::{Request, Response};

    use super::*;

    /// Messages become replies: the questions of a server, a prompt that closed and the steps of a connection; others and malformed ones
    /// are dropped.
    #[test]
    fn reads_messages() {
        let prompt = r#"{"type":"connection.prompt","data":{"id":"00000000-0000-0000-0000-000000000001","workspaceId":"00000000-0000-0000-0000-000000000002","siteId":"00000000-0000-0000-0000-000000000003","name":"Login","instruction":"","questions":[{"text":"Code:","echo":false}]}}"#;
        #[rustfmt::skip]
        let Some(Reply::Prompt(read)) = reply(prompt) else { panic!("a prompt") };
        assert_eq!(
            (read.name.as_str(), read.questions),
            ("Login", vec![("Code:".to_owned(), false)])
        );
        assert_eq!(
            reply(
                r#"{"type":"connection.prompt_closed","data":{"id":"00000000-0000-0000-0000-000000000001"}}"#
            ),
            Some(Reply::PromptClosed(Uuid::from_u128(1)))
        );
        let log = r#"{"type":"connection.log","data":{"workspaceId":"00000000-0000-0000-0000-000000000002","siteId":"00000000-0000-0000-0000-000000000003","time":"2026-01-01T00:00:00Z","level":"info","step":"connected","params":{"host":"nas"}}}"#;
        #[rustfmt::skip]
        let Some(Reply::Log(line)) = reply(log) else { panic!("a log line") };
        assert_eq!(
            (line.step.as_str(), line.params, line.time.is_some()),
            (
                "connected",
                vec![("host".to_owned(), "nas".to_owned())],
                true
            )
        );
        let snapshot = r#"{"type":"snapshot","data":{"transfers":[{"id":"00000000-0000-0000-0000-000000000001","direction":"copy","name":"a","status":"running"}]}}"#;
        assert!(matches!(reply(snapshot), Some(Reply::Snapshot(rows)) if rows.len() == 1));
        assert!(matches!(
            reply(
                r#"{"type":"transfer.state","data":{"id":"00000000-0000-0000-0000-000000000001","direction":"upload","name":"a","status":"done"}}"#
            ),
            Some(Reply::TransferState(_))
        ));
        assert!(matches!(
            reply(
                r#"{"type":"transfer.progress","data":{"id":"00000000-0000-0000-0000-000000000001","bytesDone":5,"totalBytes":9}}"#
            ),
            Some(Reply::Progress {
                done: 5,
                total: Some(9),
                ..
            })
        ));
        assert_eq!(reply(r#"{"type":"snapshot","data":{}}"#), None);
        for other in [
            r#"{"type":"transfer.progress","data":{}}"#,
            r#"{"type":"connection.state","data":{}}"#,
            "not json",
            r#"{"type":"connection.prompt","data":{}}"#,
            r#"{"type":"connection.log","data":{}}"#,
            r#"{"type":"connection.prompt_closed"}"#,
        ] {
            assert_eq!(reply(other), None, "{other}");
        }
        assert_eq!(
            address(&Url::parse("https://files.example.com").expect("url")).as_str(),
            "wss://files.example.com/api/v1/ws"
        );
        assert_eq!(
            address(&Url::parse("http://localhost:5173").expect("url")).as_str(),
            "ws://localhost:5173/api/v1/ws"
        );
        assert!(connector().is_some());
    }

    /// The stream carries the bearer token and its replies reach the interface; when it drops it connects again, and a refused session
    /// ends it; an interface that stopped listening ends it too.
    #[allow(
        clippy::result_large_err,
        reason = "the callback of the handshake of tungstenite returns its own response"
    )]
    #[tokio::test]
    async fn listens_and_connects_again() {
        let listener = TcpListener::bind("127.0.0.1:0").await.expect("a port");
        let port = listener.local_addr().expect("address").port();
        let server = tokio::spawn(async move {
            for round in 0..3 {
                let (socket, _) = listener.accept().await.expect("a client");
                if round == 2 {
                    let mut socket = socket;
                    let _ = socket
                        .write_all(b"HTTP/1.1 401 Unauthorized\r\ncontent-length: 0\r\n\r\n")
                        .await;
                    continue;
                }
                let check = |request: &Request, response: Response| {
                    assert_eq!(request.headers()["Authorization"], "Bearer t");
                    Ok(response)
                };
                let mut stream = tokio_tungstenite::accept_hdr_async(socket, check)
                    .await
                    .expect("a handshake");
                stream.send(Message::Text(r#"{"type":"connection.prompt_closed","data":{"id":"00000000-0000-0000-0000-000000000009"}}"#.into())).await.expect("sent");
                stream
                    .send(Message::Binary(vec![1].into()))
                    .await
                    .expect("sent");
                let _ = stream.close(None).await;
            }
        });
        let session = Session {
            server: Url::parse(&format!("http://127.0.0.1:{port}")).expect("url"),
            token: "t".to_owned(),
        };
        let (results, mut received) = mpsc::channel(8);
        tokio::time::timeout(
            Duration::from_secs(10),
            listen(session.clone(), results, Duration::from_millis(10)),
        )
        .await
        .expect("ended by the refusal");
        let mut closed = 0;
        while let Ok(event) = received.try_recv() {
            assert_eq!(
                event,
                Event::Replied(Reply::PromptClosed(Uuid::from_u128(9)))
            );
            closed += 1;
        }
        assert_eq!(closed, 2, "one message per connection");
        server.await.expect("the server");

        let (results, received) = mpsc::channel(8);
        drop(received);
        let gone = Session {
            server: Url::parse("http://127.0.0.1:9").expect("url"),
            ..session
        };
        tokio::time::timeout(
            Duration::from_secs(5),
            listen(gone, results, Duration::from_millis(10)),
        )
        .await
        .expect("ended without an interface");
    }
}
