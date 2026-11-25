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
use super::explorer::{Entry, Kind, Op};
use super::home::{Call, CallError, Reply, Session, Side};
use crate::api::Api;
use crate::api::ApiError;
use crate::config::Config;
use crate::login::{self, Listener, Pkce};
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
#[allow(
    clippy::too_many_lines,
    reason = "one arm per effect, each a few lines"
)]
pub async fn run(
    app: &mut App,
    ports: &Ports,
    mut input: Events<'_>,
    draw: &mut dyn FnMut(&App) -> io::Result<()>,
) -> io::Result<()> {
    let (results, mut finished) = mpsc::channel(RESULTS);
    let mut listener: Option<JoinHandle<()>> = None;
    let mut clock = tokio::time::interval(ports.tick);
    let mut effects = app.start();
    loop {
        for effect in effects.drain(..) {
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
                        let outcome = Api::new(server).probe().await.map_err(said);
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
                Effect::Shorten {
                    server,
                    challenge,
                    state,
                    redirect,
                    host,
                } => {
                    let results = results.clone();
                    tokio::spawn(async move {
                        let pkce = Pkce {
                            verifier: String::new(),
                            challenge,
                            state: state.clone(),
                        };
                        let url = login::approval_url(&server, &pkce, redirect.as_deref(), &host)
                            .await
                            .to_string();
                        let _ = results
                            .send(Event::Shortened {
                                state,
                                redirect,
                                url,
                            })
                            .await;
                    });
                }
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
                Effect::Call(session, call) => {
                    let results = results.clone();
                    tokio::spawn(async move {
                        let reply = perform(&session, call).await;
                        let _ = results.send(Event::Replied(reply)).await;
                    });
                }
                Effect::Local { side, op } => {
                    let results = results.clone();
                    tokio::task::spawn_blocking(move || {
                        let _ = results.blocking_send(Event::Replied(local(side, op)));
                    });
                }
                Effect::Logout(session) => {
                    let (results, tokens, config_path) = (
                        results.clone(),
                        ports.tokens.clone(),
                        ports.config_path.clone(),
                    );
                    tokio::spawn(async move {
                        let outcome = logout(&session, tokens, config_path).await;
                        let _ = results.send(Event::LoggedOut(outcome)).await;
                    });
                }
                Effect::SaveSettings(settings) => {
                    let (results, config_path) = (results.clone(), ports.config_path.clone());
                    tokio::task::spawn_blocking(move || {
                        let _ = results
                            .blocking_send(Event::Saved(save_settings(&config_path, settings)));
                    });
                }
            }
        }
        draw(app)?;
        let event = tokio::select! {
            next = input.next() => match next {
                Some(event) => event,
                None => break,
            },
            Some(event) = finished.recv() => event,
            _ = clock.tick() => Event::Tick,
        };
        effects = app.handle(event);
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
            let answer = listener.wait(&state, wait).await.map_err(said);
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
        token: token.token,
    }
}

/// Keeps the settings of this computer in the configuration file.
fn save_settings(
    config_path: &std::path::Path,
    settings: crate::config::Settings,
) -> Result<(), String> {
    let mut config = Config::load(config_path).map_err(said)?;
    config.settings = settings;
    config.save(config_path).map_err(said)
}

/// What went wrong, in words.
fn said(error: impl std::fmt::Display) -> String {
    error.to_string()
}

/// Why a call failed, as the interface reads it.
#[allow(
    clippy::needless_pass_by_value,
    reason = "it is passed to map_err, which gives the error by value"
)]
fn failure(error: ApiError) -> CallError {
    CallError {
        code: error.code().map(str::to_owned),
        message: error.to_string(),
    }
}

/// Makes a call of the main screen in a session.
async fn perform(session: &Session, call: Call) -> Reply {
    let api = Api::new(session.server.clone()).with_token(&session.token);
    match call {
        Call::Me => Reply::Me(api.me().await.map(|me| me.user).map_err(failure)),
        Call::Version => Reply::Version(
            api.version()
                .await
                .map(|version| version.version)
                .map_err(failure),
        ),
        Call::Workspaces => Reply::Workspaces(api.workspaces().await.map_err(failure)),
        Call::Tree(workspace) => {
            let id = workspace.to_string();
            let outcome = match tokio::try_join!(api.collections(&id), api.sites(&id)) {
                Ok(tree) => Ok(tree),
                Err(error) => Err(failure(error)),
            };
            Reply::Tree { workspace, outcome }
        }
        Call::Vault => Reply::Vault(api.vault().await.map_err(failure)),
        Call::Unlock(password) => {
            Reply::Unlocked(api.unlock_vault(&password).await.map_err(failure))
        }
        Call::Preferences { locale, theme } => Reply::Preferences(
            api.update_preferences(locale.as_deref(), theme.as_deref())
                .await
                .map_err(failure),
        ),
        Call::Files {
            workspace,
            site,
            side,
            op,
        } => remote(&api, &workspace.to_string(), &site.to_string(), side, op).await,
    }
}

/// An entry of a site as the explorer shows it.
fn entry(remote: transferly_client::models::RemoteEntry) -> Entry {
    use transferly_client::models::remote_entry::Type;
    Entry {
        name: remote.name,
        kind: match remote.r#type {
            Type::Directory => Kind::Folder,
            Type::File => Kind::File,
            Type::Symlink => Kind::Link,
            Type::Other => Kind::Other,
        },
        size: u64::try_from(remote.size).unwrap_or(0),
        modified: remote.modified_at.parse().ok(),
    }
}

/// Works with the files of a site for a panel.
async fn remote(api: &Api, workspace: &str, site: &str, side: Side, op: Op) -> Reply {
    match op {
        Op::Home => Reply::Homed {
            side,
            outcome: api
                .home(workspace, site)
                .await
                .map(|home| home.path)
                .map_err(failure),
        },
        Op::List(path) => {
            let outcome = api
                .list(workspace, site, &path)
                .await
                .map(|entries| entries.into_iter().map(entry).collect())
                .map_err(failure);
            Reply::Listed {
                side,
                path,
                outcome,
            }
        }
        Op::Mkdir(path) => Reply::Changed {
            side,
            outcome: api
                .mkdir(workspace, site, &path)
                .await
                .map(|_| ())
                .map_err(failure),
        },
        Op::Rename { from, to } => Reply::Changed {
            side,
            outcome: api
                .rename(workspace, site, &from, &to)
                .await
                .map(|_| ())
                .map_err(failure),
        },
        Op::Delete(paths) => {
            for (path, folder) in paths {
                if let Err(error) = api.delete(workspace, site, &path, folder).await {
                    return Reply::Changed {
                        side,
                        outcome: Err(failure(error)),
                    };
                }
            }
            Reply::Changed {
                side,
                outcome: Ok(()),
            }
        }
        Op::Disconnect => Reply::Changed {
            side,
            outcome: api.disconnect(workspace, site).await.map_err(failure),
        },
    }
}

/// A failure of the local disk, which has no stable code.
fn disk(error: &std::io::Error) -> CallError {
    CallError {
        code: None,
        message: error.to_string(),
    }
}

/// The entries of a folder of the local disk; an entry whose details cannot be read is shown without them.
fn read_folder(path: &str) -> std::io::Result<Vec<Entry>> {
    let mut entries = Vec::new();
    for item in std::fs::read_dir(path)? {
        let item = item?;
        let meta = item.metadata().ok();
        let kind = match item.file_type() {
            Ok(kind) if kind.is_symlink() => Kind::Link,
            Ok(kind) if kind.is_dir() => Kind::Folder,
            Ok(kind) if kind.is_file() => Kind::File,
            _ => Kind::Other,
        };
        entries.push(Entry {
            name: item.file_name().to_string_lossy().into_owned(),
            kind,
            size: meta.as_ref().map_or(0, std::fs::Metadata::len),
            modified: meta
                .and_then(|meta| meta.modified().ok())
                .and_then(|at| jiff::Timestamp::try_from(at).ok()),
        });
    }
    Ok(entries)
}

/// Works with the files of the local disk for a panel; the home folder is the one of the user.
fn local(side: Side, op: Op) -> Reply {
    match op {
        Op::Home => Reply::Homed {
            side,
            outcome: directories::BaseDirs::new()
                .map(|dirs| dirs.home_dir().to_string_lossy().into_owned())
                .ok_or(CallError {
                    code: None,
                    message: "no home folder".to_owned(),
                }),
        },
        Op::List(path) => Reply::Listed {
            side,
            outcome: read_folder(&path).map_err(|error| disk(&error)),
            path,
        },
        Op::Mkdir(path) => Reply::Changed {
            side,
            outcome: std::fs::create_dir(&path).map_err(|error| disk(&error)),
        },
        Op::Rename { from, to } => {
            let outcome = if std::path::Path::new(&to).exists() {
                Err(CallError {
                    code: Some("already_exists".to_owned()),
                    message: format!("{to} exists"),
                })
            } else {
                std::fs::rename(&from, &to).map_err(|error| disk(&error))
            };
            Reply::Changed { side, outcome }
        }
        Op::Delete(paths) => {
            let outcome = paths.iter().try_for_each(|(path, folder)| {
                if *folder {
                    std::fs::remove_dir_all(path)
                } else {
                    std::fs::remove_file(path)
                }
            });
            Reply::Changed {
                side,
                outcome: outcome.map_err(|error| disk(&error)),
            }
        }
        Op::Disconnect => Reply::Changed {
            side,
            outcome: Ok(()),
        },
    }
}

/// Ends the session on its server, forgets its token and leaves the server current no more; a session the server already ended is
/// forgotten too.
async fn logout(
    session: &Session,
    tokens: Arc<dyn TokenStore>,
    config_path: PathBuf,
) -> Result<(), String> {
    let api = Api::new(session.server.clone()).with_token(&session.token);
    match api.logout().await {
        Ok(()) => {}
        Err(error) if error.code() == Some("unauthenticated") => {}
        Err(error) => return Err(error.to_string()),
    }
    let origin = session.server.origin().ascii_serialization();
    tokio::task::spawn_blocking(move || {
        tokens.delete(&origin).map_err(said)?;
        let mut config = Config::load(&config_path).map_err(said)?;
        if config.current.as_deref() == Some(origin.as_str()) {
            config.current = None;
        }
        config.save(&config_path).map_err(said)
    })
    .await
    .expect("the task forgetting the token")
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

    /// The calls of the main screen read the server, and a refusal comes back with its code; a tree fails when either half does.
    #[tokio::test]
    async fn performs_calls() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/v1/vault/unlock"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        let session = Session {
            server: Url::parse(&server.uri()).expect("url"),
            token: "t".to_owned(),
        };
        assert_eq!(
            perform(&session, Call::Unlock("m".to_owned())).await,
            Reply::Unlocked(Ok(()))
        );
        let workspace = uuid::Uuid::nil();
        let reply = perform(&session, Call::Tree(workspace)).await;
        assert!(
            matches!(reply, Reply::Tree { outcome: Err(CallError { code: Some(ref code), .. }), .. } if code == "http_404"),
            "a tree the server does not have"
        );
    }

    /// Signing out forgets the token even when the server already ended the session, keeps another current server, and reports a
    /// server that cannot be reached or a configuration that cannot be written; settings are kept or the reason is given.
    #[tokio::test]
    async fn signs_out_and_keeps_settings() {
        let dir = tempfile::tempdir().expect("folder");
        let tokens: Arc<dyn TokenStore> = Arc::from(secrets::open(
            dir.path(),
            Ok(keyring_core::mock::Store::new().expect("mock")),
        ));
        let config_path = dir.path().join(crate::config::FILE_NAME);
        let ended = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/v1/auth/logout"))
            .respond_with(ResponseTemplate::new(401).set_body_json(serde_json::json!({"type": "about:blank", "title": "x", "status": 401, "code": "unauthenticated"})))
            .mount(&ended)
            .await;
        let session = Session {
            server: Url::parse(&ended.uri()).expect("url"),
            token: "t".to_owned(),
        };
        let mut config = Config::default();
        config.signed_in("https://other.example.com", "ana@example.com");
        config.save(&config_path).expect("config");
        tokens.set(&ended.uri(), "t").expect("token");
        logout(&session, tokens.clone(), config_path.clone())
            .await
            .expect("signed out");
        assert_eq!(tokens.get(&ended.uri()).expect("tokens"), None);
        assert_eq!(
            Config::load(&config_path)
                .expect("config")
                .current
                .as_deref(),
            Some("https://other.example.com")
        );

        let down = Session {
            server: Url::parse("http://127.0.0.1:9").expect("url"),
            token: "t".to_owned(),
        };
        assert!(
            logout(&down, tokens.clone(), config_path.clone())
                .await
                .is_err()
        );
        std::fs::write(&config_path, "not = [toml").expect("a broken configuration");
        assert!(
            logout(&session, tokens.clone(), config_path.clone())
                .await
                .is_err()
        );

        assert!(save_settings(&config_path, crate::config::Settings::default()).is_err());
        std::fs::remove_file(&config_path).expect("removed");
        save_settings(&config_path, crate::config::Settings::default()).expect("kept");
        assert!(config_path.exists());
    }

    /// The files of a site: every operation reaches the server, entries come back as the explorer shows them, and the first failed
    /// deletion stops the others.
    #[allow(clippy::too_many_lines, reason = "one call of each operation")]
    #[tokio::test]
    async fn works_with_remote_files() {
        use wiremock::matchers::query_param;
        let server = MockServer::start().await;
        let base = "/api/v1/workspaces/w/sites/s";
        let entries = serde_json::json!([
            {"name": "d", "path": "/d", "type": "directory", "size": 0, "mode": "", "modifiedAt": "2026-01-01T00:00:00Z", "modifiedPrecision": "second"},
            {"name": "f", "path": "/f", "type": "file", "size": 5, "mode": "", "modifiedAt": "bad", "modifiedPrecision": "second"},
            {"name": "l", "path": "/l", "type": "symlink", "size": -1, "mode": "", "modifiedAt": "2026-01-01T00:00:00Z", "modifiedPrecision": "second"},
            {"name": "o", "path": "/o", "type": "other", "size": 0, "mode": "", "modifiedAt": "2026-01-01T00:00:00Z", "modifiedPrecision": "second"}
        ]);
        let entry = entries[0].clone();
        Mock::given(method("GET"))
            .and(path(format!("{base}/files")))
            .respond_with(ResponseTemplate::new(200).set_body_json(entries))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path(format!("{base}/files/home")))
            .respond_with(
                ResponseTemplate::new(200).set_body_json(serde_json::json!({"path": "/"})),
            )
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(201).set_body_json(entry))
            .mount(&server)
            .await;
        Mock::given(method("DELETE")).and(path(format!("{base}/files"))).and(query_param("path", "/bad")).respond_with(ResponseTemplate::new(404).set_body_json(serde_json::json!({"type": "about:blank", "title": "x", "status": 404, "code": "not_found"}))).mount(&server).await;
        Mock::given(method("DELETE"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        let api = Api::new(Url::parse(&server.uri()).expect("url")).with_token("t");
        let side = Side::Right;
        assert_eq!(
            remote(&api, "w", "s", side, Op::Home).await,
            Reply::Homed {
                side,
                outcome: Ok("/".to_owned())
            }
        );
        #[rustfmt::skip]
        let Reply::Listed { outcome: Ok(listed), .. } = remote(&api, "w", "s", side, Op::List("/".to_owned())).await else { panic!("a listing") };
        let kinds: Vec<(Kind, u64, bool)> = listed
            .iter()
            .map(|entry| (entry.kind, entry.size, entry.modified.is_some()))
            .collect();
        assert_eq!(
            kinds,
            [
                (Kind::Folder, 0, true),
                (Kind::File, 5, false),
                (Kind::Link, 0, true),
                (Kind::Other, 0, true)
            ]
        );
        let done = Reply::Changed {
            side,
            outcome: Ok(()),
        };
        assert_eq!(
            remote(&api, "w", "s", side, Op::Mkdir("/d".to_owned())).await,
            done
        );
        assert_eq!(
            remote(
                &api,
                "w",
                "s",
                side,
                Op::Rename {
                    from: "/d".to_owned(),
                    to: "/e".to_owned()
                }
            )
            .await,
            done
        );
        assert_eq!(
            remote(
                &api,
                "w",
                "s",
                side,
                Op::Delete(vec![("/d".to_owned(), true)])
            )
            .await,
            done
        );
        assert_eq!(remote(&api, "w", "s", side, Op::Disconnect).await, done);
        let failed = remote(
            &api,
            "w",
            "s",
            side,
            Op::Delete(vec![("/bad".to_owned(), false), ("/d".to_owned(), false)]),
        )
        .await;
        assert!(
            matches!(failed, Reply::Changed { outcome: Err(CallError { code: Some(ref code), .. }), .. } if code == "not_found")
        );
        let session = Session {
            server: Url::parse(&server.uri()).expect("url"),
            token: "t".to_owned(),
        };
        let call = Call::Files {
            workspace: uuid::Uuid::nil(),
            site: uuid::Uuid::nil(),
            side,
            op: Op::Disconnect,
        };
        assert!(matches!(
            perform(&session, call).await,
            Reply::Changed { .. }
        ));
    }

    /// The files of the local disk: the home folder, listing with the kind, size and date of each entry, creating, renaming without
    /// replacing, deleting files and folders with what they hold, and the failures of the disk.
    #[allow(clippy::too_many_lines, reason = "one call of each operation")]
    #[test]
    fn works_with_local_files() {
        let dir = tempfile::tempdir().expect("folder");
        let root = dir.path().to_string_lossy().into_owned();
        let at = |name: &str| dir.path().join(name).to_string_lossy().into_owned();
        let side = Side::Left;
        assert!(matches!(
            local(side, Op::Home),
            Reply::Homed { outcome: Ok(_), .. }
        ));
        let done = Reply::Changed {
            side,
            outcome: Ok(()),
        };
        assert_eq!(local(side, Op::Mkdir(at("d"))), done);
        std::fs::write(dir.path().join("d").join("inside"), "x").expect("a file");
        std::fs::write(dir.path().join("f"), "12345").expect("a file");
        #[cfg(unix)]
        {
            std::os::unix::fs::symlink(dir.path().join("f"), dir.path().join("l")).expect("a link");
            let _socket =
                std::os::unix::net::UnixListener::bind(dir.path().join("s")).expect("a socket");
        }
        #[rustfmt::skip]
        let Reply::Listed { outcome: Ok(mut listed), .. } = local(side, Op::List(root.clone())) else { panic!("a listing") };
        listed.sort_by(|a, b| a.name.cmp(&b.name));
        let found: Vec<(&str, Kind, u64)> = listed
            .iter()
            .map(|entry| {
                (
                    entry.name.as_str(),
                    entry.kind,
                    if entry.kind == Kind::File {
                        entry.size
                    } else {
                        0
                    },
                )
            })
            .collect();
        assert!(
            found.contains(&("d", Kind::Folder, 0)) && found.contains(&("f", Kind::File, 5)),
            "{found:?}"
        );
        #[cfg(unix)]
        assert!(
            found.contains(&("l", Kind::Link, 0)) && found.contains(&("s", Kind::Other, 0)),
            "{found:?}"
        );
        assert!(listed.iter().all(|entry| entry.modified.is_some()));
        assert!(matches!(
            local(side, Op::List(at("missing"))),
            Reply::Listed {
                outcome: Err(CallError { code: None, .. }),
                ..
            }
        ));
        assert!(matches!(
            local(
                side,
                Op::Rename {
                    from: at("f"),
                    to: at("d")
                }
            ),
            Reply::Changed {
                outcome: Err(CallError { code: Some(_), .. }),
                ..
            }
        ));
        assert_eq!(
            local(
                side,
                Op::Rename {
                    from: at("f"),
                    to: at("g")
                }
            ),
            done
        );
        assert!(matches!(
            local(
                side,
                Op::Rename {
                    from: at("f"),
                    to: at("h")
                }
            ),
            Reply::Changed {
                outcome: Err(_),
                ..
            }
        ));
        assert_eq!(
            local(side, Op::Delete(vec![(at("d"), true), (at("g"), false)])),
            done
        );
        assert!(matches!(
            local(side, Op::Delete(vec![(at("g"), false)])),
            Reply::Changed {
                outcome: Err(_),
                ..
            }
        ));
        assert!(matches!(
            local(side, Op::Mkdir(at("missing/deeper"))),
            Reply::Changed {
                outcome: Err(_),
                ..
            }
        ));
        assert_eq!(local(side, Op::Disconnect), done);
    }
}
