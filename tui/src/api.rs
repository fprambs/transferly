//! The API of a Transferly server, as the terminal client uses it (docs/guides/reqwest.md): one HTTP client, the bearer token on every
//! request, no redirects, timeouts that never cut a long transfer, and the stable code of every refusal.

use std::time::Duration;

use reqwest::{Client, Method, RequestBuilder, StatusCode};
use serde::de::DeserializeOwned;
use transferly_client::models::{
    CliToken, CliTokenRequest, Collection, MasterPasswordRequest, Preferences, Problem,
    SessionInfo, SetupStatus, Site, User, VaultStatus, Version, Workspace,
};
use url::Url;

/// How long connecting to the server may take.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(10);
/// How long the server may stay silent in the middle of an answer.
const READ_TIMEOUT: Duration = Duration::from_secs(60);

/// Why a call to the server failed.
#[derive(Debug, thiserror::Error)]
pub enum ApiError {
    /// The server could not be reached, or the answer was cut.
    #[error("the server could not be reached: {0}")]
    Network(#[from] reqwest::Error),
    /// The server refused the call with a stable code, such as `invalid_grant`.
    #[error("the server answered {status}: {code}")]
    Refused {
        /// HTTP status of the answer.
        status: u16,
        /// Stable code of the problem, or `http_<status>` when the answer had none.
        code: String,
    },
    /// The server answered something that is not what Transferly answers.
    #[error("the server does not answer as a Transferly server: {0}")]
    Unexpected(String),
}

impl ApiError {
    /// The stable code of a refusal, if this is one.
    #[must_use]
    pub fn code(&self) -> Option<&str> {
        match self {
            Self::Refused { code, .. } => Some(code),
            _ => None,
        }
    }
}

/// A Transferly server, signed in to or not.
#[derive(Clone)]
pub struct Api {
    base: Url,
    http: Client,
    token: Option<String>,
}

impl Api {
    /// The server at an origin, signed in to nobody yet.
    ///
    /// # Panics
    ///
    /// The TLS backend cannot start, which happens only on a broken build.
    #[must_use]
    pub fn new(base: Url) -> Self {
        let http = Client::builder()
            .user_agent(concat!("transferly-tui/", env!("CARGO_PKG_VERSION")))
            .connect_timeout(CONNECT_TIMEOUT)
            .read_timeout(READ_TIMEOUT)
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .expect("the HTTP client of the terminal");
        Self {
            base,
            http,
            token: None,
        }
    }

    /// The same server, signed in with a bearer token.
    #[must_use]
    pub fn with_token(mut self, token: &str) -> Self {
        self.token = Some(token.to_owned());
        self
    }

    /// The origin of the server.
    #[must_use]
    pub fn base(&self) -> &Url {
        &self.base
    }

    /// A request to a path of the API, with the token when there is one.
    fn request(&self, method: Method, path: &str) -> RequestBuilder {
        let url = self.base.join(path).expect("a path of the API");
        let request = self.http.request(method, url);
        match &self.token {
            Some(token) => request.bearer_auth(token),
            None => request,
        }
    }

    /// Sends a request and decodes its answer, turning a refusal into its stable code.
    async fn send<T: DeserializeOwned>(request: RequestBuilder) -> Result<T, ApiError> {
        let response = request.send().await?;
        let status = response.status();
        let body = response.bytes().await?;
        if !status.is_success() {
            return Err(refusal(status, &body));
        }
        serde_json::from_slice(&body).map_err(|error| ApiError::Unexpected(error.to_string()))
    }

    /// Checks that the server is a Transferly server, through the only call every version answers without a session.
    ///
    /// # Errors
    ///
    /// The server cannot be reached or is not a Transferly server.
    pub async fn probe(&self) -> Result<(), ApiError> {
        Self::send::<SetupStatus>(self.request(Method::GET, "/api/v1/setup/status"))
            .await
            .map(|_| ())
    }

    /// Exchanges an approved code for a session of the terminal client. A server without the sign-in of the terminal answers 404.
    ///
    /// # Errors
    ///
    /// The server refuses the code (`invalid_grant`), has the sign-in turned off, or cannot be reached.
    pub async fn exchange(
        &self,
        code: &str,
        verifier: &str,
        redirect_uri: Option<&str>,
    ) -> Result<CliToken, ApiError> {
        let mut body = CliTokenRequest::new(code.to_owned(), verifier.to_owned());
        body.redirect_uri = redirect_uri.map(str::to_owned);
        Self::send(self.request(Method::POST, "/api/v1/cli/token").json(&body)).await
    }

    /// The signed-in account and its session.
    ///
    /// # Errors
    ///
    /// The token is not valid any more (`unauthenticated`), or the server cannot be reached.
    pub async fn me(&self) -> Result<SessionInfo, ApiError> {
        Self::send(self.request(Method::GET, "/api/v1/me")).await
    }

    /// The version of the server.
    ///
    /// # Errors
    ///
    /// The server cannot be reached or answers something else.
    pub async fn version(&self) -> Result<Version, ApiError> {
        Self::send(self.request(Method::GET, "/api/v1/version")).await
    }

    /// The workspaces of the account: the personal one and its teams.
    ///
    /// # Errors
    ///
    /// The session ended or the server cannot be reached.
    pub async fn workspaces(&self) -> Result<Vec<Workspace>, ApiError> {
        Self::send(self.request(Method::GET, "/api/v1/workspaces")).await
    }

    /// The collections of a workspace.
    ///
    /// # Errors
    ///
    /// The workspace is not the account's, the session ended or the server cannot be reached.
    pub async fn collections(&self, workspace: &str) -> Result<Vec<Collection>, ApiError> {
        Self::send(self.request(
            Method::GET,
            &format!("/api/v1/workspaces/{workspace}/collections"),
        ))
        .await
    }

    /// The sites of a workspace.
    ///
    /// # Errors
    ///
    /// The workspace is not the account's, the session ended or the server cannot be reached.
    pub async fn sites(&self, workspace: &str) -> Result<Vec<Site>, ApiError> {
        Self::send(self.request(
            Method::GET,
            &format!("/api/v1/workspaces/{workspace}/sites"),
        ))
        .await
    }

    /// Whether the vault of the account exists and is unlocked in this session.
    ///
    /// # Errors
    ///
    /// The session ended or the server cannot be reached.
    pub async fn vault(&self) -> Result<VaultStatus, ApiError> {
        Self::send(self.request(Method::GET, "/api/v1/vault")).await
    }

    /// Unlocks the vault of the account for this session.
    ///
    /// # Errors
    ///
    /// The master password is wrong (`invalid_master_password`), too many attempts (`rate_limited`), or the server cannot be reached.
    pub async fn unlock_vault(&self, master_password: &str) -> Result<(), ApiError> {
        let body = MasterPasswordRequest::new(master_password.to_owned());
        Self::empty(
            self.request(Method::POST, "/api/v1/vault/unlock")
                .json(&body),
        )
        .await
    }

    /// Changes the language or the theme of the account, which the web shares, and returns the account as it stands.
    ///
    /// # Errors
    ///
    /// A value is not valid, the session ended or the server cannot be reached.
    pub async fn update_preferences(
        &self,
        locale: Option<&str>,
        theme: Option<&str>,
    ) -> Result<User, ApiError> {
        let mut body = Preferences::new();
        body.locale = locale.map(str::to_owned);
        body.theme = theme.map(str::to_owned);
        Self::send(self.request(Method::PATCH, "/api/v1/me").json(&body)).await
    }

    /// Sends a request whose answer has no body.
    async fn empty(request: RequestBuilder) -> Result<(), ApiError> {
        let response = request.send().await?;
        let status = response.status();
        if status.is_success() {
            return Ok(());
        }
        Err(refusal(status, &response.bytes().await?))
    }

    /// Ends the session of the token.
    ///
    /// # Errors
    ///
    /// The server refuses or cannot be reached.
    pub async fn logout(&self) -> Result<(), ApiError> {
        Self::empty(self.request(Method::POST, "/api/v1/auth/logout")).await
    }
}

/// The refusal of a status and its body: the stable code of a problem document, or `http_<status>` without one.
fn refusal(status: StatusCode, body: &[u8]) -> ApiError {
    let code = serde_json::from_slice::<Problem>(body).map_or_else(
        |_| format!("http_{}", status.as_u16()),
        |problem| problem.code,
    );
    ApiError::Refused {
        status: status.as_u16(),
        code,
    }
}

#[cfg(test)]
mod tests {
    use wiremock::matchers::{body_json, header, header_exists, method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    use super::*;

    /// A user as the server describes it.
    fn user() -> serde_json::Value {
        serde_json::json!({"id": "01a20f66-e85c-71b3-9c42-2987ff8c4adb", "email": "ana@example.com", "role": "user", "locale": "es",
            "timezone": "America/Santiago", "theme": "nord", "fileDoubleClick": "download"})
    }

    /// The API of a mock server.
    fn api(server: &MockServer) -> Api {
        Api::new(Url::parse(&server.uri()).expect("mock address"))
    }

    /// Probing accepts a Transferly server and refuses one that answers otherwise.
    #[tokio::test]
    async fn probes_the_server() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v1/setup/status"))
            .respond_with(
                ResponseTemplate::new(200).set_body_json(serde_json::json!({"required": false})),
            )
            .mount(&server)
            .await;
        api(&server).probe().await.expect("a Transferly server");

        let other = MockServer::start().await;
        Mock::given(method("GET"))
            .respond_with(ResponseTemplate::new(200).set_body_string("<html>"))
            .mount(&other)
            .await;
        assert!(matches!(
            api(&other).probe().await,
            Err(ApiError::Unexpected(_))
        ));
        let missing = MockServer::start().await;
        assert!(
            matches!(api(&missing).probe().await, Err(ApiError::Refused { status: 404, ref code }) if code == "http_404")
        );
    }

    /// The exchange sends the code, the verifier and the redirect, and reads the token or the stable code of the refusal.
    #[tokio::test]
    async fn exchanges_codes() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/v1/cli/token"))
            .and(body_json(serde_json::json!({"code": "good", "codeVerifier": "v", "redirectUri": "http://127.0.0.1:9/callback"})))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"token": "t", "expiresAt": "2026-12-25T00:00:00Z", "user": user()})))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/cli/token"))
            .and(body_json(serde_json::json!({"code": "bad", "codeVerifier": "v"})))
            .respond_with(ResponseTemplate::new(400).set_body_json(serde_json::json!({"type": "about:blank", "title": "Bad Request", "status": 400, "code": "invalid_grant"})))
            .mount(&server)
            .await;
        let token = api(&server)
            .exchange("good", "v", Some("http://127.0.0.1:9/callback"))
            .await
            .expect("a token");
        assert_eq!(
            (token.token.as_str(), token.user.email.as_str()),
            ("t", "ana@example.com")
        );
        let refused = api(&server)
            .exchange("bad", "v", None)
            .await
            .expect_err("refused");
        assert_eq!(refused.code(), Some("invalid_grant"));
        assert!(refused.to_string().contains("invalid_grant"));
    }

    /// Signed-in calls carry the bearer token; signing out reads the refusal like any call.
    #[tokio::test]
    async fn sends_the_token() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v1/me"))
            .and(header("Authorization", "Bearer secret"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(serde_json::json!({"user": user(), "csrfToken": "c"})),
            )
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/auth/logout"))
            .and(header("Authorization", "Bearer secret"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        Mock::given(method("POST")).and(path("/api/v1/auth/logout")).and(header_exists("Authorization")).respond_with(ResponseTemplate::new(401).set_body_json(serde_json::json!({"type": "about:blank", "title": "Unauthorized", "status": 401, "code": "unauthenticated"}))).mount(&server).await;

        let signed_in = api(&server).with_token("secret");
        assert_eq!(
            signed_in.me().await.expect("me").user.email,
            "ana@example.com"
        );
        signed_in.logout().await.expect("logout");
        let stale = api(&server).with_token("stale");
        assert_eq!(
            stale.logout().await.expect_err("refused").code(),
            Some("unauthenticated")
        );
        assert_eq!(stale.base().as_str(), format!("{}/", server.uri()));
    }

    /// The calls of the main screen: workspaces, collections and sites, the vault, unlocking it, and the preferences of the account.
    #[tokio::test]
    async fn reads_the_workspace() {
        let server = MockServer::start().await;
        let workspace = "01a20f66-e85c-71b3-9c42-2987ff8c4ad0";
        let answers = [
            (
                "GET",
                "/api/v1/workspaces".to_owned(),
                200,
                serde_json::json!([{"id": workspace, "name": "Personal", "kind": "personal", "role": "owner", "createdAt": "2026-01-01T00:00:00Z"}]),
            ),
            (
                "GET",
                format!("/api/v1/workspaces/{workspace}/collections"),
                200,
                serde_json::json!([{"id": "01a20f66-e85c-71b3-9c42-2987ff8c4ad1", "name": "Clients"}]),
            ),
            (
                "GET",
                format!("/api/v1/workspaces/{workspace}/sites"),
                200,
                serde_json::json!([{"id": "01a20f66-e85c-71b3-9c42-2987ff8c4ad2", "name": "acme", "connector": "sftp", "config": {"host": "sftp.acme.com"}, "speedLimitBps": 0, "secrets": ["password"], "createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-01T00:00:00Z"}]),
            ),
            (
                "GET",
                "/api/v1/version".to_owned(),
                200,
                serde_json::json!({"version": "1.4.0"}),
            ),
            (
                "GET",
                "/api/v1/vault".to_owned(),
                200,
                serde_json::json!({"initialized": true, "unlocked": false}),
            ),
            ("PATCH", "/api/v1/me".to_owned(), 200, user()),
        ];
        for (verb, route, status, body) in answers {
            Mock::given(method(verb))
                .and(path(route))
                .respond_with(ResponseTemplate::new(status).set_body_json(body))
                .mount(&server)
                .await;
        }
        Mock::given(method("POST"))
            .and(path("/api/v1/vault/unlock"))
            .and(body_json(serde_json::json!({"masterPassword": "right"})))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        Mock::given(method("POST")).and(path("/api/v1/vault/unlock")).respond_with(ResponseTemplate::new(400).set_body_json(serde_json::json!({"type": "about:blank", "title": "Bad Request", "status": 400, "code": "invalid_master_password"}))).mount(&server).await;

        let api = api(&server).with_token("t");
        let workspaces = api.workspaces().await.expect("workspaces");
        assert_eq!(workspaces[0].name, "Personal");
        assert_eq!(
            api.collections(workspace).await.expect("collections")[0].name,
            "Clients"
        );
        assert_eq!(
            api.sites(workspace).await.expect("sites")[0].config["host"],
            "sftp.acme.com"
        );
        assert!(!api.vault().await.expect("vault").unlocked);
        assert_eq!(api.version().await.expect("version").version, "1.4.0");
        api.unlock_vault("right").await.expect("unlocked");
        assert_eq!(
            api.unlock_vault("wrong").await.expect_err("refused").code(),
            Some("invalid_master_password")
        );
        assert_eq!(
            api.update_preferences(Some("es"), None)
                .await
                .expect("updated")
                .email,
            "ana@example.com"
        );
    }

    /// An answer cut in the middle of its body is a network error, when decoding a call and when signing out.
    #[tokio::test]
    async fn reports_cut_answers() {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0")
            .await
            .expect("bind");
        let address = listener.local_addr().expect("address");
        tokio::spawn(async move {
            for _ in 0..2 {
                let (mut stream, _) = listener.accept().await.expect("accept");
                let mut request = [0u8; 4096];
                let _ = tokio::io::AsyncReadExt::read(&mut stream, &mut request).await;
                let _ = tokio::io::AsyncWriteExt::write_all(
                    &mut stream,
                    b"HTTP/1.1 401 Unauthorized\r\nContent-Length: 100\r\n\r\n{",
                )
                .await;
            }
        });
        let api = Api::new(Url::parse(&format!("http://{address}")).expect("address"));
        assert!(matches!(api.probe().await, Err(ApiError::Network(_))));
        assert!(matches!(api.logout().await, Err(ApiError::Network(_))));
    }

    /// A server that cannot be reached is a network error, with no code.
    #[tokio::test]
    async fn reports_unreachable_servers() {
        let closed = Api::new(Url::parse("http://127.0.0.1:9").expect("address"));
        let error = closed.probe().await.expect_err("nothing listens on port 9");
        assert!(matches!(error, ApiError::Network(_)));
        assert_eq!(error.code(), None);
        assert!(matches!(closed.logout().await, Err(ApiError::Network(_))));
    }
}
