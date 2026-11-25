//! The loop of the interface (docs/guides/ratatui.md, docs/guides/tokio.md): it draws, waits for the next event of the terminal, of a
//! finished effect or of the clock, lets the interface handle it, and runs the effects it returns as tasks that report back as events.
//! It never awaits the network itself, so drawing never stops.

use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use futures::{Stream, StreamExt};
use std::io;
use std::pin::Pin;
use tokio::sync::mpsc;
use tokio::task::JoinHandle;
use url::Url;

use super::app::{App, Effect, Event, Failure};
use crate::api::Api;
use crate::config::Config;
use crate::login::Listener;
use crate::secrets::TokenStore;

/// The events of the terminal, or of a test, the loop reads.
pub type Events<'a> = Pin<Box<dyn Stream<Item = Event> + 'a>>;

/// How many results of effects may wait to be handled.
const RESULTS: usize = 64;

/// What the effects reach outside the interface.
pub struct Ports {
    /// The configuration file.
    pub config_path: PathBuf,
    /// Where tokens are kept.
    pub tokens: Arc<dyn TokenStore>,
    /// Opens an address in the browser and tells whether it did.
    pub open_browser: Arc<dyn Fn(&str) -> bool + Send + Sync>,
    /// Copies text to the clipboard of the terminal.
    pub copy: Arc<dyn Fn(&str) + Send + Sync>,
    /// Address the listener of the callback takes.
    pub listen_on: String,
    /// How long to wait for the browser.
    pub wait: Duration,
    /// How often the clock ticks, for the spinner.
    pub tick: Duration,
}

/// Runs the interface until it quits or its input ends.
///
/// # Errors
///
/// Drawing on the terminal fails.
pub async fn run(
    app: &mut App,
    ports: &Ports,
    mut input: Events<'_>,
    draw: &mut dyn FnMut(&App) -> io::Result<()>,
) -> io::Result<()> {
    let (results, mut finished) = mpsc::channel(RESULTS);
    let mut listener: Option<JoinHandle<()>> = None;
    let mut clock = tokio::time::interval(ports.tick);
    loop {
        draw(app)?;
        let event = tokio::select! {
            next = input.next() => match next {
                Some(event) => event,
                None => break,
            },
            Some(event) = finished.recv() => event,
            _ = clock.tick() => Event::Tick,
        };
        for effect in app.handle(event) {
            match effect {
                Effect::Quit => {
                    stop(&mut listener);
                    return Ok(());
                }
                Effect::StopListening => stop(&mut listener),
                Effect::Listen { state } => {
                    stop(&mut listener);
                    listener = Some(tokio::spawn(listen(
                        ports.listen_on.clone(),
                        state,
                        ports.wait,
                        results.clone(),
                    )));
                }
                Effect::Probe(server) => {
                    let results = results.clone();
                    tokio::spawn(async move {
                        let outcome = Api::new(server)
                            .probe()
                            .await
                            .map_err(|error| error.to_string());
                        let _ = results.send(Event::Probed(outcome)).await;
                    });
                }
                Effect::OpenBrowser(url) => {
                    let (results, open) = (results.clone(), ports.open_browser.clone());
                    tokio::task::spawn_blocking(move || {
                        let _ = results.blocking_send(Event::BrowserOpened(open(&url)));
                    });
                }
                Effect::Copy(text) => (ports.copy)(&text),
                Effect::Exchange {
                    server,
                    code,
                    verifier,
                    redirect,
                } => {
                    let (results, tokens, config_path) = (
                        results.clone(),
                        ports.tokens.clone(),
                        ports.config_path.clone(),
                    );
                    tokio::spawn(async move {
                        let event = exchange(
                            server,
                            &code,
                            &verifier,
                            redirect.as_deref(),
                            tokens,
                            config_path,
                        )
                        .await;
                        let _ = results.send(event).await;
                    });
                }
            }
        }
    }
    stop(&mut listener);
    Ok(())
}

/// Stops the listener, if one is waiting.
fn stop(listener: &mut Option<JoinHandle<()>>) {
    if let Some(task) = listener.take() {
        task.abort();
    }
}

/// Starts the listener and reports its address, then the answer of the browser.
async fn listen(address: String, state: String, wait: Duration, results: mpsc::Sender<Event>) {
    match Listener::bind(&address).await {
        Ok(listener) => {
            let _ = results
                .send(Event::Listening(Ok(listener.redirect_uri().to_owned())))
                .await;
            let answer = listener
                .wait(&state, wait)
                .await
                .map_err(|error| error.to_string());
            let _ = results.send(Event::Answered(answer)).await;
        }
        Err(error) => {
            let _ = results.send(Event::Listening(Err(error.to_string()))).await;
        }
    }
}

/// Exchanges the code, keeps the token and records the sign-in; the event says how it went.
async fn exchange(
    server: Url,
    code: &str,
    verifier: &str,
    redirect: Option<&str>,
    tokens: Arc<dyn TokenStore>,
    config_path: PathBuf,
) -> Event {
    let token = match Api::new(server.clone())
        .exchange(code, verifier, redirect)
        .await
    {
        Ok(token) => token,
        Err(error) => {
            return Event::SignInFailed(error.code().map_or_else(
                || Failure::Unreachable(error.to_string()),
                |code| Failure::Refused(code.to_owned()),
            ));
        }
    };
    let origin = server.origin().ascii_serialization();
    let kept = {
        let (origin, secret) = (origin.clone(), token.token.clone());
        tokio::task::spawn_blocking(move || tokens.set(&origin, &secret))
            .await
            .expect("the task keeping the token")
    };
    if let Err(error) = kept {
        return Event::SignInFailed(Failure::NotKept(error.to_string()));
    }
    let mut config = Config::load(&config_path).unwrap_or_default();
    config.signed_in(&origin, &token.user.email);
    if let Err(error) = config.save(&config_path) {
        return Event::SignInFailed(Failure::NotKept(error.to_string()));
    }
    Event::SignedIn {
        server: origin,
        email: token.user.email,
        locale: token.user.locale,
        theme: token.user.theme,
    }
}

#[cfg(test)]
mod tests {
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    use super::*;
    use crate::secrets;

    /// A user as the server describes it.
    fn user() -> serde_json::Value {
        serde_json::json!({"id": "01a20f66-e85c-71b3-9c42-2987ff8c4adb", "email": "ana@example.com", "role": "user", "locale": "en",
            "timezone": "UTC", "theme": "nord", "fileDoubleClick": "download"})
    }

    /// A server that answers the exchange with a token.
    async fn accepting() -> MockServer {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/v1/cli/token"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"token": "t", "expiresAt": "2026-12-25T00:00:00Z", "user": user()})))
            .mount(&server)
            .await;
        server
    }

    /// An exchange reports the refusal by its code, an unreachable server by its reason, and a token or configuration that cannot be
    /// kept as not kept; a good one records the sign-in.
    #[tokio::test]
    async fn reports_how_the_exchange_went() {
        let dir = tempfile::tempdir().expect("folder");
        let tokens: Arc<dyn TokenStore> = Arc::from(secrets::open(
            dir.path(),
            Ok(keyring_core::mock::Store::new().expect("mock")),
        ));
        let config = dir.path().join(crate::config::FILE_NAME);

        let refusing = MockServer::start().await;
        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(400).set_body_json(serde_json::json!({"type": "about:blank", "title": "x", "status": 400, "code": "invalid_grant"})))
            .mount(&refusing)
            .await;
        let url = |server: &MockServer| Url::parse(&server.uri()).expect("url");
        assert_eq!(
            exchange(
                url(&refusing),
                "c",
                "v",
                None,
                tokens.clone(),
                config.clone()
            )
            .await,
            Event::SignInFailed(Failure::Refused("invalid_grant".into()))
        );
        let down = Url::parse("http://127.0.0.1:9").expect("url");
        assert!(matches!(
            exchange(down, "c", "v", None, tokens.clone(), config.clone()).await,
            Event::SignInFailed(Failure::Unreachable(_))
        ));

        let server = accepting().await;
        let good = exchange(
            url(&server),
            "c",
            "v",
            Some("http://127.0.0.1:5/callback"),
            tokens.clone(),
            config.clone(),
        )
        .await;
        assert!(
            matches!(good, Event::SignedIn { ref email, ref theme, .. } if email == "ana@example.com" && theme == "nord")
        );

        let blocked = secrets::open(
            &config.join("blocked"),
            Err(keyring_core::Error::NoDefaultStore),
        );
        std::fs::write(&config, "").expect("a file where the folder of tokens would go");
        assert!(matches!(
            exchange(
                url(&server),
                "c",
                "v",
                None,
                Arc::from(blocked),
                config.clone()
            )
            .await,
            Event::SignInFailed(Failure::NotKept(_))
        ));
        let unsaved = dir.path().join("not-a-folder");
        std::fs::write(&unsaved, "")
            .expect("a file where the folder of the configuration would go");
        assert!(matches!(
            exchange(
                url(&server),
                "c",
                "v",
                None,
                tokens,
                unsaved.join("config.toml")
            )
            .await,
            Event::SignInFailed(Failure::NotKept(_))
        ));
    }

    /// A listener that cannot start says so, and stopping when nothing listens does nothing.
    #[tokio::test]
    async fn reports_a_listener_that_cannot_start() {
        let (results, mut finished) = mpsc::channel(4);
        listen(
            "256.0.0.1:0".into(),
            "s".into(),
            Duration::from_secs(1),
            results,
        )
        .await;
        assert!(matches!(
            finished.recv().await,
            Some(Event::Listening(Err(_)))
        ));
        let mut nothing = None;
        stop(&mut nothing);
        assert!(nothing.is_none());
    }
}
