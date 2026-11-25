//! The commands of `transferly-tui` outside the interface: `login`, `status` and `logout`, for scripts and for the first sign-in.

use std::io::{BufRead, Write};
use std::path::PathBuf;
use std::process::ExitCode;
use std::time::Duration;

use url::Url;

use crate::api::{Api, ApiError};
use crate::config::{self, Config};
use crate::login::{self, Answer, Listener, Pkce};
use crate::secrets::TokenStore;

/// How long a sign-in waits for the browser.
pub const BROWSER_WAIT: Duration = Duration::from_mins(10);

/// What the commands reach outside themselves, so tests can stand in for each part.
pub struct Context<'a> {
    /// The configuration file.
    pub config_path: PathBuf,
    /// Where the tokens are kept.
    pub tokens: Box<dyn TokenStore>,
    /// Where messages go.
    pub out: &'a mut dyn Write,
    /// Where a pasted code comes from.
    pub input: &'a mut dyn BufRead,
    /// Opens an address in the browser and tells whether it did.
    pub open_browser: &'a dyn Fn(&str) -> bool,
    /// Whether this computer can show a browser at all.
    pub browser_possible: bool,
    /// The name of this computer.
    pub host: String,
    /// How long to wait for the browser.
    pub wait: Duration,
    /// Address the listener of the callback takes, [`login::LOOPBACK`] but in tests.
    pub listen_on: String,
}

/// The origin of a server as the key of its profile and token, such as `https://files.example.com`.
fn key(server: &Url) -> String {
    server.origin().ascii_serialization()
}

/// Writes a line of output; a closed output is not worth failing a sign-in for.
macro_rules! say {
    ($ctx:expr, $($arg:tt)*) => {
        let _ = writeln!($ctx.out, $($arg)*);
    };
}

/// What a refusal of the exchange means for the person.
fn explain(error: &ApiError) -> String {
    match error.code() {
        Some("invalid_grant") => "The code is not valid any more: it works once, for 5 minutes. Sign in again.".to_owned(),
        Some("cli_login_disabled") => "This server has signing in from the terminal turned off; ask its administrator.".to_owned(),
        Some("http_404") => "This server is too old to sign in from the terminal: update it to the version of transferly-tui.".to_owned(),
        _ => format!("The sign-in failed: {error}."),
    }
}

/// Signs this terminal in to a server through the browser, or by pasting the code, and keeps the token.
pub async fn login(ctx: &mut Context<'_>, server: &str, paste: bool) -> ExitCode {
    let origin = match config::server_origin(server) {
        Ok(origin) => origin,
        Err(error) => {
            say!(ctx, "{error}.");
            return ExitCode::from(2);
        }
    };
    let api = Api::new(origin.clone());
    if let Err(error) = api.probe().await {
        say!(
            ctx,
            "{} is not a Transferly server, or it cannot be reached: {error}.",
            key(&origin)
        );
        return ExitCode::FAILURE;
    }
    let pkce = Pkce::new();
    let (code, redirect) = if !paste && ctx.browser_possible {
        match approve_in_browser(ctx, &origin, &pkce).await {
            Ok(answer) => answer,
            Err(code) => return code,
        }
    } else {
        let url = login::authorize_url(&origin, &pkce, None, &ctx.host);
        say!(
            ctx,
            "Open this address on any device, sign in and approve:\n\n  {url}\n\nThen paste the code the page shows:"
        );
        let mut line = String::new();
        let _ = ctx.input.read_line(&mut line);
        (line.trim().to_owned(), None)
    };
    let token = match api
        .exchange(&code, &pkce.verifier, redirect.as_deref())
        .await
    {
        Ok(token) => token,
        Err(error) => {
            say!(ctx, "{}", explain(&error));
            return ExitCode::FAILURE;
        }
    };
    let server = key(&origin);
    if let Err(error) = ctx.tokens.set(&server, &token.token) {
        say!(ctx, "Signed in, but the token could not be kept: {error}.");
        return ExitCode::FAILURE;
    }
    let mut config = Config::load(&ctx.config_path).unwrap_or_default();
    config.signed_in(&server, &token.user.email);
    if let Err(error) = config.save(&ctx.config_path) {
        say!(
            ctx,
            "Signed in, but the configuration could not be saved: {error}."
        );
        return ExitCode::FAILURE;
    }
    say!(ctx, "Signed in to {server} as {}.", token.user.email);
    ExitCode::SUCCESS
}

/// Opens the approval page with a redirect to the listener and waits for the answer; returns the code and the redirect.
async fn approve_in_browser(
    ctx: &mut Context<'_>,
    origin: &Url,
    pkce: &Pkce,
) -> Result<(String, Option<String>), ExitCode> {
    let listener = match Listener::bind(&ctx.listen_on).await {
        Ok(listener) => listener,
        Err(error) => {
            say!(
                ctx,
                "Could not listen on 127.0.0.1 ({error}); run login again with --paste."
            );
            return Err(ExitCode::FAILURE);
        }
    };
    let redirect = listener.redirect_uri().to_owned();
    let url = login::authorize_url(origin, pkce, Some(&redirect), &ctx.host);
    say!(ctx, "Approve this sign-in in your browser:\n\n  {url}\n");
    if !(ctx.open_browser)(url.as_str()) {
        say!(ctx, "The browser did not open; open the address yourself.");
    }
    say!(ctx, "Waiting for the approval…");
    match listener.wait(&pkce.state, ctx.wait).await {
        Ok(Answer::Approved(code)) => Ok((code, Some(redirect))),
        Ok(Answer::Denied) => {
            say!(ctx, "The sign-in was denied in the browser.");
            Err(ExitCode::FAILURE)
        }
        Err(error) => {
            say!(ctx, "The sign-in failed: {error}.");
            Err(ExitCode::FAILURE)
        }
    }
}

/// The server signed in to and its token, or the message that says there is none.
fn current(ctx: &Context<'_>) -> Result<(String, String), String> {
    let config = Config::load(&ctx.config_path).map_err(|error| format!("{error}."))?;
    let server = config
        .current
        .ok_or_else(|| "Not signed in. Run: transferly-tui login <server>".to_owned())?;
    match ctx.tokens.get(&server) {
        Ok(Some(token)) => Ok((server, token)),
        Ok(None) => Err(format!(
            "Not signed in to {server}. Run: transferly-tui login {server}"
        )),
        Err(error) => Err(format!("The token of {server} could not be read: {error}.")),
    }
}

/// Says which server and account this terminal is signed in to, checking the session with the server.
///
/// # Panics
///
/// A stored server is not an address, which the configuration never holds since it keeps only checked origins.
pub async fn status(ctx: &mut Context<'_>) -> ExitCode {
    let (server, token) = match current(ctx) {
        Ok(found) => found,
        Err(message) => {
            say!(ctx, "{message}");
            return ExitCode::FAILURE;
        }
    };
    let api = Api::new(Url::parse(&server).expect("a stored origin")).with_token(&token);
    match api.me().await {
        Ok(session) => {
            say!(
                ctx,
                "Signed in to {server} as {}.\nThe token is kept in {}.",
                session.user.email,
                ctx.tokens.describe()
            );
            ExitCode::SUCCESS
        }
        Err(error) if error.code() == Some("unauthenticated") => {
            say!(
                ctx,
                "The session on {server} ended. Run: transferly-tui login {server}"
            );
            ExitCode::FAILURE
        }
        Err(error) => {
            say!(ctx, "Could not check the session on {server}: {error}.");
            ExitCode::FAILURE
        }
    }
}

/// Ends the session of this terminal on the server and forgets its token.
///
/// # Panics
///
/// A stored server is not an address, which the configuration never holds since it keeps only checked origins.
pub async fn logout(ctx: &mut Context<'_>) -> ExitCode {
    let (server, token) = match current(ctx) {
        Ok(found) => found,
        Err(message) => {
            say!(ctx, "{message}");
            return ExitCode::FAILURE;
        }
    };
    let api = Api::new(Url::parse(&server).expect("a stored origin")).with_token(&token);
    match api.logout().await {
        Ok(()) => {}
        Err(error) if error.code() == Some("unauthenticated") => {}
        Err(error) => {
            say!(
                ctx,
                "Could not end the session on {server}: {error}. The token is kept; try again."
            );
            return ExitCode::FAILURE;
        }
    }
    if let Err(error) = ctx.tokens.delete(&server) {
        say!(
            ctx,
            "Signed out, but the token could not be forgotten: {error}."
        );
        return ExitCode::FAILURE;
    }
    say!(ctx, "Signed out of {server}.");
    ExitCode::SUCCESS
}

#[cfg(test)]
mod tests {
    use std::io::Cursor;
    use std::sync::Arc;

    use wiremock::matchers::{body_partial_json, method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    use super::*;
    use crate::secrets;

    /// A user as the server describes it.
    fn user() -> serde_json::Value {
        serde_json::json!({"id": "01a20f66-e85c-71b3-9c42-2987ff8c4adb", "email": "ana@example.com", "role": "user", "locale": "es",
            "timezone": "UTC", "theme": "nord", "fileDoubleClick": "download"})
    }

    /// A mock Transferly server that exchanges the code "good" and knows the token "t".
    async fn transferly() -> MockServer {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v1/setup/status"))
            .respond_with(
                ResponseTemplate::new(200).set_body_json(serde_json::json!({"required": false})),
            )
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/cli/token"))
            .and(body_partial_json(serde_json::json!({"code": "good"})))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"token": "t", "expiresAt": "2026-12-25T00:00:00Z", "user": user()})))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/cli/token"))
            .respond_with(problem(400, "invalid_grant"))
            .mount(&server)
            .await;
        server
    }

    /// A problem document.
    fn problem(status: u16, code: &str) -> ResponseTemplate {
        ResponseTemplate::new(status).set_body_json(serde_json::json!({"type": "about:blank", "title": "x", "status": status, "code": code}))
    }

    /// The parts of a context the tests set.
    struct Harness {
        dir: tempfile::TempDir,
        out: Vec<u8>,
        input: Cursor<Vec<u8>>,
        mock: Arc<keyring_core::CredentialStore>,
    }

    impl Harness {
        /// A fresh configuration folder, empty output, the given pasted input and an empty credential store.
        fn new(input: &str) -> Self {
            Self {
                dir: tempfile::tempdir().expect("folder"),
                out: Vec::new(),
                input: Cursor::new(input.as_bytes().to_vec()),
                mock: keyring_core::mock::Store::new().expect("mock"),
            }
        }

        /// A context over these parts; the browser, when it opens, is played by `browser`.
        fn context<'a>(
            &'a mut self,
            browser: &'a dyn Fn(&str) -> bool,
            browser_possible: bool,
        ) -> Context<'a> {
            let mock = self.mock.clone();
            Context {
                config_path: self.dir.path().join(config::FILE_NAME),
                tokens: secrets::open(self.dir.path(), Ok(mock)),
                out: &mut self.out,
                input: &mut self.input,
                open_browser: browser,
                browser_possible,
                host: "laptop".to_owned(),
                wait: Duration::from_secs(5),
                listen_on: login::LOOPBACK.to_owned(),
            }
        }

        /// What the commands wrote.
        fn output(&self) -> String {
            String::from_utf8_lossy(&self.out).into_owned()
        }
    }

    /// Plays a browser that answers the approval page with the given query, keeping its state.
    fn browser(answer: &'static str) -> impl Fn(&str) -> bool {
        move |address: &str| {
            let url = Url::parse(address).expect("approval address");
            let pairs: std::collections::HashMap<_, _> = url.query_pairs().into_owned().collect();
            assert_eq!(
                (pairs["client_name"].as_str(), pairs["client_host"].as_str()),
                ("transferly-tui", "laptop")
            );
            let callback = if answer.contains("state=") {
                format!("{}?{answer}", pairs["redirect_uri"])
            } else {
                format!(
                    "{}?{answer}&state={}",
                    pairs["redirect_uri"], pairs["state"]
                )
            };
            tokio::spawn(async move { reqwest::get(callback).await.expect("callback") });
            true
        }
    }

    /// Signing in through the browser keeps the token and the profile; status then checks it, and logout forgets it.
    #[tokio::test]
    async fn signs_in_through_the_browser() {
        let server = transferly().await;
        Mock::given(method("GET"))
            .and(path("/api/v1/me"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(serde_json::json!({"user": user(), "csrfToken": "c"})),
            )
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/auth/logout"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        let mut h = Harness::new("");
        let open = browser("code=good");
        assert_eq!(
            login(&mut h.context(&open, true), &server.uri(), false).await,
            ExitCode::SUCCESS
        );
        assert!(
            h.output().contains("Approve this sign-in")
                && h.output().contains(&format!(
                    "Signed in to {} as ana@example.com.",
                    server.uri()
                ))
        );
        let config = Config::load(&h.dir.path().join(config::FILE_NAME)).expect("config");
        assert_eq!(config.current.as_deref(), Some(server.uri().as_str()));

        assert_eq!(status(&mut h.context(&open, true)).await, ExitCode::SUCCESS);
        assert!(
            h.output()
                .contains("The token is kept in the credential store of the system.")
        );
        assert_eq!(logout(&mut h.context(&open, true)).await, ExitCode::SUCCESS);
        assert!(h.output().contains("Signed out of"));
        assert_eq!(status(&mut h.context(&open, true)).await, ExitCode::FAILURE);
        assert!(h.output().contains("Not signed in to"));
    }

    /// Without a browser, or with --paste, the person pastes the code; a browser that does not open is said so.
    #[tokio::test]
    async fn signs_in_with_a_pasted_code() {
        let server = transferly().await;
        let opened = std::cell::Cell::new(false);
        let never = |_: &str| -> bool {
            opened.set(true);
            false
        };
        assert!(!never("reset"));
        opened.set(false);
        let mut h = Harness::new("  good \n");
        assert_eq!(
            login(&mut h.context(&never, false), &server.uri(), false).await,
            ExitCode::SUCCESS
        );
        assert!(h.output().contains("paste the code"));
        let mut h = Harness::new("good\n");
        assert_eq!(
            login(&mut h.context(&never, true), &server.uri(), true).await,
            ExitCode::SUCCESS
        );
        assert!(!opened.get(), "no browser opens when the code is pasted");

        let mut h = Harness::new("");
        let mut ctx = h.context(&never, true);
        ctx.listen_on = "256.0.0.1:0".to_owned();
        assert_eq!(
            login(&mut ctx, &server.uri(), false).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("run login again with --paste"));

        let mut h = Harness::new("");
        let closed = |address: &str| -> bool {
            let _ = browser("code=good")(address);
            false
        };
        assert_eq!(
            login(&mut h.context(&closed, true), &server.uri(), false).await,
            ExitCode::SUCCESS
        );
        assert!(h.output().contains("did not open"));
    }

    /// Wrong addresses, servers that are not Transferly, refused codes, denials and forged answers fail with a message.
    #[tokio::test]
    async fn explains_failed_sign_ins() {
        let server = transferly().await;
        let none = |_: &str| false;
        let cases: [(&str, &str, bool, &str, ExitCode); 3] = [
            (
                "not a url at all",
                "",
                true,
                "is not the address of a server",
                ExitCode::from(2),
            ),
            (
                "http://127.0.0.1:9",
                "",
                true,
                "is not a Transferly server",
                ExitCode::FAILURE,
            ),
            (
                &server.uri(),
                "bad\n",
                true,
                "works once, for 5 minutes",
                ExitCode::FAILURE,
            ),
        ];
        for (address, input, paste, message, code) in cases {
            let mut h = Harness::new(input);
            assert_eq!(
                login(&mut h.context(&none, false), address, paste).await,
                code,
                "{address}"
            );
            assert!(h.output().contains(message), "{}", h.output());
        }
        for (answer, message) in [
            ("error=access_denied", "denied in the browser"),
            ("code=good&state=forged", "not for this sign-in"),
        ] {
            let mut h = Harness::new("");
            let open = browser(answer);
            assert_eq!(
                login(&mut h.context(&open, true), &server.uri(), false).await,
                ExitCode::FAILURE
            );
            assert!(h.output().contains(message), "{}", h.output());
        }
    }

    /// The other refusals of the exchange each say what to do.
    #[test]
    fn explains_refusals() {
        let refused = |code: &str| ApiError::Refused {
            status: 400,
            code: code.to_owned(),
        };
        assert!(explain(&refused("cli_login_disabled")).contains("turned off"));
        assert!(explain(&refused("http_404")).contains("too old"));
        assert!(explain(&refused("rate_limited")).contains("rate_limited"));
    }

    /// A kept token or configuration that cannot be written fails the sign-in with a message.
    #[tokio::test]
    async fn reports_what_could_not_be_kept() {
        let server = transferly().await;
        let none = |_: &str| false;
        let mut h = Harness::new("good\n");
        let mut ctx = h.context(&none, false);
        ctx.tokens = secrets::open(
            &ctx.config_path.join("blocked"),
            Err(keyring_core::Error::NoDefaultStore),
        );
        std::fs::write(&ctx.config_path, "").expect("a file where the folder of tokens would go");
        assert_eq!(
            login(&mut ctx, &server.uri(), true).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("the token could not be kept"));

        let mut h = Harness::new("good\n");
        let mut ctx = h.context(&none, false);
        let blocker = ctx.config_path.with_file_name("not-a-folder");
        std::fs::write(&blocker, "").expect("a file where the folder would go");
        ctx.config_path = blocker.join(config::FILE_NAME);
        assert_eq!(
            login(&mut ctx, &server.uri(), true).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("the configuration could not be saved"));
    }

    /// Status and logout without a sign-in, with an ended session, with an unreadable configuration or token, or with a server down.
    #[tokio::test]
    async fn reports_status_and_logout_problems() {
        let none = |_: &str| false;
        let mut h = Harness::new("");
        assert_eq!(
            status(&mut h.context(&none, false)).await,
            ExitCode::FAILURE
        );
        assert_eq!(
            logout(&mut h.context(&none, false)).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("Run: transferly-tui login <server>"));

        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v1/me"))
            .respond_with(problem(401, "unauthenticated"))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/auth/logout"))
            .respond_with(problem(401, "unauthenticated"))
            .mount(&server)
            .await;
        let mut h = Harness::new("");
        let mut config = Config::default();
        config.signed_in(&server.uri(), "ana@example.com");
        config
            .save(&h.dir.path().join(config::FILE_NAME))
            .expect("save");
        h.context(&none, false)
            .tokens
            .set(&server.uri(), "stale")
            .expect("token");
        assert_eq!(
            status(&mut h.context(&none, false)).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("ended"));
        assert_eq!(
            logout(&mut h.context(&none, false)).await,
            ExitCode::SUCCESS
        );

        let mut h = Harness::new("");
        let mut config = Config::default();
        config.signed_in("http://127.0.0.1:9", "ana@example.com");
        config
            .save(&h.dir.path().join(config::FILE_NAME))
            .expect("save");
        h.context(&none, false)
            .tokens
            .set("http://127.0.0.1:9", "t")
            .expect("token");
        assert_eq!(
            status(&mut h.context(&none, false)).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("Could not check the session"));
        assert_eq!(
            logout(&mut h.context(&none, false)).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("The token is kept"));

        let mut h = Harness::new("");
        std::fs::write(h.dir.path().join(config::FILE_NAME), "current = [").expect("broken");
        assert_eq!(
            status(&mut h.context(&none, false)).await,
            ExitCode::FAILURE
        );
        assert!(h.output().contains("not valid"));
    }

    /// A token that cannot be read or forgotten is reported.
    #[tokio::test]
    async fn reports_token_store_failures() {
        let none = |_: &str| false;
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/v1/auth/logout"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        let mut h = Harness::new("");
        let mut config = Config::default();
        config.signed_in(&server.uri(), "ana@example.com");
        config
            .save(&h.dir.path().join(config::FILE_NAME))
            .expect("save");
        let mut ctx = h.context(&none, false);
        let tokens = ctx.config_path.parent().expect("folder").join("tokens");
        std::fs::create_dir_all(tokens.join(secrets::FILE_NAME))
            .expect("a folder where the file of tokens goes");
        ctx.tokens = secrets::open(&tokens, Err(keyring_core::Error::NoDefaultStore));
        assert_eq!(status(&mut ctx).await, ExitCode::FAILURE);
        assert!(h.output().contains("could not be read"));

        let mut h = Harness::new("");
        config
            .save(&h.dir.path().join(config::FILE_NAME))
            .expect("save");
        let mut ctx = h.context(&none, false);
        let tokens = ctx.config_path.parent().expect("folder").join("tokens");
        std::fs::create_dir_all(&tokens).expect("folder");
        std::fs::write(
            tokens.join(secrets::FILE_NAME),
            format!("\"{}\" = \"t\"\n", server.uri()),
        )
        .expect("tokens");
        ctx.tokens = secrets::open(&tokens, Err(keyring_core::Error::NoDefaultStore));
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&tokens, std::fs::Permissions::from_mode(0o500))
                .expect("read-only folder");
        }
        assert_eq!(logout(&mut ctx).await, ExitCode::FAILURE);
        assert!(h.output().contains("could not be forgotten"));
    }
}
