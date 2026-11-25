//! Signing the terminal client in through the browser (ADR-0029): the PKCE verifier, the address of the approval page, and the listener
//! on 127.0.0.1 that receives the code; without a browser, the person pastes the code instead.

use std::collections::HashMap;
use std::io;
use std::time::Duration;

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use sha2::{Digest, Sha256};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use url::Url;

/// Name the client gives itself on the approval page.
pub const CLIENT_NAME: &str = "transferly-tui";

/// Address the listener takes: a free port of the loopback.
pub const LOOPBACK: &str = "127.0.0.1:0";

/// Largest request head the listener reads.
const MAX_REQUEST: usize = 8 * 1024;

/// Page the browser shows once it handed the answer to the terminal.
const DONE_PAGE: &str = "<!DOCTYPE html><meta charset=utf-8><title>transferly-tui</title>\
<body style=\"font-family:system-ui;background:#121212;color:#e0f2f1;display:grid;place-items:center;height:90vh\">\
<p>Back to your terminal: you can close this tab.</p>";

/// The secrets of one sign-in: the PKCE verifier, its S256 challenge, and the state the answer must carry back.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Pkce {
    /// The verifier, sent only with the exchange.
    pub verifier: String,
    /// The S256 challenge of the verifier, sent to the approval page.
    pub challenge: String,
    /// A random value the callback must return unchanged.
    pub state: String,
}

impl Pkce {
    /// Fresh secrets: 32 random bytes of verifier and 16 of state, in base64url.
    #[must_use]
    pub fn new() -> Self {
        let verifier = URL_SAFE_NO_PAD.encode(rand::random::<[u8; 32]>());
        let challenge = URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes()));
        Self {
            verifier,
            challenge,
            state: URL_SAFE_NO_PAD.encode(rand::random::<[u8; 16]>()),
        }
    }
}

impl Default for Pkce {
    fn default() -> Self {
        Self::new()
    }
}

/// The address of the approval page of a server, for a redirect to the listener or, without one, for a code to paste.
///
/// # Panics
///
/// The server is not a base address, which [`crate::config::server_origin`] never returns.
#[must_use]
pub fn authorize_url(server: &Url, pkce: &Pkce, redirect_uri: Option<&str>, host: &str) -> Url {
    let mut url = server.join("/cli/authorize").expect("the approval page");
    {
        let mut query = url.query_pairs_mut();
        query
            .append_pair("code_challenge", &pkce.challenge)
            .append_pair("code_challenge_method", "S256")
            .append_pair("state", &pkce.state);
        query
            .append_pair("client_name", CLIENT_NAME)
            .append_pair("client_host", host);
        if let Some(redirect) = redirect_uri {
            query.append_pair("redirect_uri", redirect);
        }
    }
    url
}

/// What the browser handed back.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Answer {
    /// The person approved: the code to exchange.
    Approved(String),
    /// The person denied the sign-in.
    Denied,
}

/// Why waiting for the browser failed.
#[derive(Debug, thiserror::Error)]
pub enum LoginError {
    /// The listener failed.
    #[error("the listener on 127.0.0.1: {0}")]
    Io(#[from] io::Error),
    /// The answer carried another state: it did not come from this sign-in.
    #[error("the answer of the browser was not for this sign-in")]
    StateMismatch,
    /// No answer came in time.
    #[error("no answer came from the browser in time")]
    TimedOut,
}

/// The listener on 127.0.0.1 that waits for the callback of the approval page.
pub struct Listener {
    listener: TcpListener,
    redirect_uri: String,
}

impl Listener {
    /// Listens on an address, `127.0.0.1:0` for a free port of the loopback; the redirect always names 127.0.0.1 and the port.
    ///
    /// # Errors
    ///
    /// No port can be opened.
    ///
    /// # Panics
    ///
    /// Never in practice: a bound listener always has an address.
    pub async fn bind(address: &str) -> io::Result<Self> {
        let listener = TcpListener::bind(address).await?;
        let redirect_uri = format!(
            "http://127.0.0.1:{}/callback",
            listener
                .local_addr()
                .expect("a bound listener has an address")
                .port()
        );
        Ok(Self {
            listener,
            redirect_uri,
        })
    }

    /// The address to send the answer to.
    #[must_use]
    pub fn redirect_uri(&self) -> &str {
        &self.redirect_uri
    }

    /// Waits for the answer of the browser, ignoring requests for other paths, and checks its state.
    ///
    /// # Errors
    ///
    /// The listener fails, the state does not match, or nothing comes before the time runs out.
    pub async fn wait(self, state: &str, within: Duration) -> Result<Answer, LoginError> {
        tokio::time::timeout(within, self.accept(state))
            .await
            .unwrap_or(Err(LoginError::TimedOut))
    }

    /// Accepts connections until one is the callback.
    async fn accept(&self, state: &str) -> Result<Answer, LoginError> {
        loop {
            let (mut stream, _) = self.listener.accept().await?;
            let Some(query) = read_callback(&mut stream).await else {
                let _ = stream
                    .write_all(
                        b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
                    )
                    .await;
                continue;
            };
            let page = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{DONE_PAGE}",
                DONE_PAGE.len()
            );
            let _ = stream.write_all(page.as_bytes()).await;
            if query.get("state").map(String::as_str) != Some(state) {
                return Err(LoginError::StateMismatch);
            }
            return Ok(match (query.get("code"), query.get("error")) {
                (Some(code), _) => Answer::Approved(code.clone()),
                _ => Answer::Denied,
            });
        }
    }
}

/// Reads the head of a request and returns the query of `GET /callback`, or `None` for anything else.
async fn read_callback(stream: &mut tokio::net::TcpStream) -> Option<HashMap<String, String>> {
    let mut head = Vec::new();
    let mut chunk = [0u8; 1024];
    while !head.windows(4).any(|window| window == b"\r\n\r\n") && head.len() < MAX_REQUEST {
        let read = stream.read(&mut chunk).await.unwrap_or(0);
        if read == 0 {
            break;
        }
        head.extend_from_slice(&chunk[..read]);
    }
    let line = String::from_utf8_lossy(&head);
    let target = line
        .lines()
        .next()?
        .strip_prefix("GET ")?
        .split(' ')
        .next()
        .expect("a split yields at least one piece");
    let url = Url::parse("http://127.0.0.1")
        .expect("the loopback")
        .join(target)
        .ok()?;
    (url.path() == "/callback").then(|| url.query_pairs().into_owned().collect())
}

/// Whether a browser can open on this computer: not over SSH, and on Linux and BSD only with a graphical session.
#[must_use]
pub fn can_open_browser(env: impl Fn(&str) -> Option<String>) -> bool {
    let over_ssh = env("SSH_CONNECTION").is_some() || env("SSH_TTY").is_some();
    let graphical = !cfg!(all(unix, not(target_os = "macos")))
        || env("DISPLAY").is_some()
        || env("WAYLAND_DISPLAY").is_some();
    !over_ssh && graphical
}

/// The name of this computer, as the approval page shows it.
#[must_use]
pub fn host_name() -> String {
    gethostname::gethostname().to_string_lossy().into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The verifier is 43 characters of base64url and the challenge is its SHA-256, so the server can check it; each sign-in differs.
    #[test]
    fn makes_pkce_secrets() {
        let pkce = Pkce::default();
        assert_eq!(pkce.verifier.len(), 43);
        assert_eq!(
            pkce.challenge,
            URL_SAFE_NO_PAD.encode(Sha256::digest(pkce.verifier.as_bytes()))
        );
        assert_eq!(pkce.state.len(), 22);
        assert_ne!(Pkce::new(), pkce);
    }

    /// The approval address carries the challenge, the state, the name and host of the client, and the redirect when there is one.
    #[test]
    fn builds_the_approval_address() {
        let pkce = Pkce {
            verifier: "v".into(),
            challenge: "c".into(),
            state: "s".into(),
        };
        let server = Url::parse("https://files.example.com").expect("server");
        let with = authorize_url(
            &server,
            &pkce,
            Some("http://127.0.0.1:5/callback"),
            "my laptop",
        );
        assert_eq!(
            with.as_str(),
            "https://files.example.com/cli/authorize?code_challenge=c&code_challenge_method=S256&state=s&client_name=transferly-tui&client_host=my+laptop&redirect_uri=http%3A%2F%2F127.0.0.1%3A5%2Fcallback"
        );
        assert!(
            !authorize_url(&server, &pkce, None, "h")
                .as_str()
                .contains("redirect_uri")
        );
    }

    /// Sends a GET to the listener and returns the answer of the browser's side.
    async fn get(redirect: &str, target: &str) -> reqwest::Response {
        let base = Url::parse(redirect).expect("redirect");
        reqwest::get(base.join(target).expect("target"))
            .await
            .expect("request")
    }

    /// The listener ignores other paths, hands over the code with the right state, and shows the page that sends back to the terminal.
    #[tokio::test]
    async fn receives_the_code() {
        let listener = Listener::bind(LOOPBACK).await.expect("bind");
        let redirect = listener.redirect_uri().to_owned();
        assert!(redirect.starts_with("http://127.0.0.1:") && redirect.ends_with("/callback"));
        let waiting = tokio::spawn(listener.wait("s1", Duration::from_secs(5)));
        assert_eq!(get(&redirect, "/favicon.ico").await.status(), 404);
        let page = get(&redirect, "/callback?code=abc&state=s1").await;
        assert!(page.text().await.expect("page").contains("close this tab"));
        assert_eq!(
            waiting.await.expect("task").expect("answer"),
            Answer::Approved("abc".into())
        );
    }

    /// A denial is reported as such, an answer with another state is refused, and silence times out.
    #[tokio::test]
    async fn reports_denials_forgeries_and_silence() {
        let listener = Listener::bind(LOOPBACK).await.expect("bind");
        let redirect = listener.redirect_uri().to_owned();
        let waiting = tokio::spawn(listener.wait("s1", Duration::from_secs(5)));
        get(&redirect, "/callback?error=access_denied&state=s1").await;
        assert_eq!(
            waiting.await.expect("task").expect("answer"),
            Answer::Denied
        );

        let listener = Listener::bind(LOOPBACK).await.expect("bind");
        let redirect = listener.redirect_uri().to_owned();
        let waiting = tokio::spawn(listener.wait("s1", Duration::from_secs(5)));
        get(&redirect, "/callback?code=abc&state=other").await;
        assert!(matches!(
            waiting.await.expect("task"),
            Err(LoginError::StateMismatch)
        ));

        let silent = Listener::bind(LOOPBACK)
            .await
            .expect("bind")
            .wait("s1", Duration::from_millis(50))
            .await;
        assert!(matches!(silent, Err(LoginError::TimedOut)));
        assert!(LoginError::TimedOut.to_string().contains("in time"));
    }

    /// Requests that are not a GET of the callback, cut or oversized, are answered 404 and the listener keeps waiting.
    #[tokio::test]
    async fn ignores_other_requests() {
        let listener = Listener::bind(LOOPBACK).await.expect("bind");
        let redirect = listener.redirect_uri().to_owned();
        let address = Url::parse(&redirect)
            .expect("redirect")
            .socket_addrs(|| None)
            .expect("address")[0];
        let waiting = tokio::spawn(listener.wait("s1", Duration::from_secs(5)));
        for raw in [
            &b"POST /callback?code=x&state=s1 HTTP/1.1\r\n\r\n"[..],
            b"GET http://[ HTTP/1.1\r\n\r\n",
            b"",
            &vec![b'a'; MAX_REQUEST + 10],
        ] {
            let mut stream = tokio::net::TcpStream::connect(address)
                .await
                .expect("connect");
            stream.write_all(raw).await.expect("write");
            stream.shutdown().await.expect("shutdown");
            let mut answer = String::new();
            let _ = stream.read_to_string(&mut answer).await;
            assert!(
                answer.starts_with("HTTP/1.1 404") || answer.is_empty(),
                "{answer}"
            );
        }
        get(&redirect, "/callback?code=ok&state=s1").await;
        assert_eq!(
            waiting.await.expect("task").expect("answer"),
            Answer::Approved("ok".into())
        );
    }

    /// Over SSH there is no browser; on Linux one needs a graphical session.
    #[test]
    fn knows_when_a_browser_can_open() {
        let with = |pairs: &'static [(&'static str, &'static str)]| {
            move |name: &str| {
                pairs
                    .iter()
                    .find(|(key, _)| *key == name)
                    .map(|(_, value)| (*value).to_owned())
            }
        };
        assert!(!can_open_browser(with(&[
            ("SSH_CONNECTION", "1"),
            ("DISPLAY", ":0")
        ])));
        assert!(!can_open_browser(with(&[("SSH_TTY", "/dev/pts/1")])));
        assert!(can_open_browser(with(&[("DISPLAY", ":0")])));
        assert!(can_open_browser(with(&[("WAYLAND_DISPLAY", "wayland-0")])));
        assert_eq!(
            can_open_browser(with(&[])),
            !cfg!(all(unix, not(target_os = "macos")))
        );
        assert_ne!(host_name(), "");
    }
}
