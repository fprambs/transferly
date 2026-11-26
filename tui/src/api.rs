//! The API of a Transferly server, as the terminal client uses it (docs/guides/reqwest.md): one HTTP client, the bearer token on every
//! request, no redirects, timeouts that never cut a long transfer, and the stable code of every refusal.

use std::collections::HashMap;
use std::time::Duration;

use reqwest::{Client, Method, RequestBuilder, StatusCode};
use serde::de::DeserializeOwned;
use transferly_client::models::host_key_input::Mode;
use transferly_client::models::{
    CliLink, CliLinkInput, CliToken, CliTokenRequest, Collection, CopyInput, HomeFolder,
    HostKeyInput, MasterPasswordRequest, PathInput, Preferences, Problem, PromptAnswers,
    RemoteEntry, RenameInput, SessionInfo, SetupStatus, Site, SiteCredentialRequest, User,
    VaultStatus, Version, Workspace,
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
        /// What the problem says about itself, such as the fingerprint of a key, by name.
        params: HashMap<String, serde_json::Value>,
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

    /// What a refusal says about itself, by name; empty for anything else.
    #[must_use]
    pub fn params(&self) -> HashMap<String, serde_json::Value> {
        match self {
            Self::Refused { params, .. } => params.clone(),
            _ => HashMap::new(),
        }
    }
}

/// How a key joins the keys already trusted for a server.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TrustMode {
    /// Next to the others, for several servers behind one name.
    Add,
    /// Instead of the others, for a server whose key changed.
    Replace,
}

/// The key of a server a person saw and confirmed.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HostKey {
    /// The host of the server.
    pub host: String,
    /// Its port.
    pub port: u16,
    /// The type of the key, such as `ssh-ed25519`.
    pub key_type: String,
    /// Its fingerprint.
    pub fingerprint: String,
    /// How it joins the keys already trusted, when one of its type is.
    pub mode: Option<TrustMode>,
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

    /// Reads the catalog used to build site forms.
    ///
    /// # Errors
    ///
    /// The session ended or the server cannot be reached.
    pub async fn connectors(&self) -> Result<Vec<transferly_client::models::Connector>, ApiError> {
        Self::send(self.request(Method::GET, "/api/v1/connectors")).await
    }

    /// Runs a site operation in a workspace.
    ///
    /// # Errors
    ///
    /// The server refuses the operation or cannot be reached.
    pub async fn site_action(
        &self,
        workspace: uuid::Uuid,
        action: &SiteAction,
    ) -> Result<(), ApiError> {
        let (method, suffix, body) = match action {
            SiteAction::Create(body) => (Method::POST, String::new(), Some(body)),
            SiteAction::Edit(site, body) => (Method::PUT, format!("/{site}"), Some(body)),
            SiteAction::Delete(site) => (Method::DELETE, format!("/{site}"), None),
            SiteAction::Move(site, body) => (Method::POST, format!("/{site}/move"), Some(body)),
            SiteAction::Test(body) => (Method::POST, "/test".to_owned(), Some(body)),
        };
        let mut request = self.request(
            method,
            &format!("/api/v1/workspaces/{workspace}/sites{suffix}"),
        );
        if let Some(body) = body {
            request = request.json(body);
        }
        Self::empty(request).await
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

    /// Registers a sign-in request under a short link to the approval page, /cli/a/<code>, which fits on one line.
    ///
    /// # Errors
    ///
    /// The server has no short links (`http_404`), refuses the request, or cannot be reached.
    pub async fn link(
        &self,
        challenge: &str,
        state: &str,
        redirect: Option<&str>,
        name: &str,
        host: &str,
    ) -> Result<CliLink, ApiError> {
        let mut body = CliLinkInput::new(challenge.to_owned(), name.to_owned());
        body.state = Some(state.to_owned());
        body.redirect_uri = redirect.map(str::to_owned);
        body.client_host = Some(host.to_owned());
        Self::send(self.request(Method::POST, "/api/v1/cli/links").json(&body)).await
    }

    /// The path of the files of a site.
    fn files(workspace: &str, site: &str, rest: &str) -> String {
        format!("/api/v1/workspaces/{workspace}/sites/{site}/{rest}")
    }

    /// The entries of a folder of a site, connecting to it if needed.
    ///
    /// # Errors
    ///
    /// The folder does not exist, the vault is locked (`vault_locked`), the server key is not trusted (`host_key_unknown`), the server
    /// cannot be reached, or the session ended.
    pub async fn list(
        &self,
        workspace: &str,
        site: &str,
        path: &str,
    ) -> Result<Vec<RemoteEntry>, ApiError> {
        Self::send(
            self.request(Method::GET, &Self::files(workspace, site, "files"))
                .query(&[("path", path)]),
        )
        .await
    }

    /// The folder a connection to a site starts in.
    ///
    /// # Errors
    ///
    /// As listing a folder.
    pub async fn home(&self, workspace: &str, site: &str) -> Result<HomeFolder, ApiError> {
        Self::send(self.request(Method::GET, &Self::files(workspace, site, "files/home"))).await
    }

    /// Creates a folder on a site.
    ///
    /// # Errors
    ///
    /// It exists, the parent does not, the account may not write, or as listing a folder.
    pub async fn mkdir(
        &self,
        workspace: &str,
        site: &str,
        path: &str,
    ) -> Result<RemoteEntry, ApiError> {
        let body = PathInput::new(path.to_owned());
        Self::send(
            self.request(
                Method::POST,
                &Self::files(workspace, site, "files/directories"),
            )
            .json(&body),
        )
        .await
    }

    /// Renames or moves an entry of a site; an existing destination is never replaced.
    ///
    /// # Errors
    ///
    /// The destination exists, the source does not, the account may not write, or as listing a folder.
    pub async fn rename(
        &self,
        workspace: &str,
        site: &str,
        from: &str,
        to: &str,
    ) -> Result<RemoteEntry, ApiError> {
        let body = RenameInput::new(from.to_owned(), to.to_owned());
        Self::send(
            self.request(Method::POST, &Self::files(workspace, site, "files/rename"))
                .json(&body),
        )
        .await
    }

    /// Deletes an entry of a site, a folder with everything in it when `recursive`.
    ///
    /// # Errors
    ///
    /// It does not exist, a folder is not empty without `recursive`, the account may not write, or as listing a folder.
    pub async fn delete(
        &self,
        workspace: &str,
        site: &str,
        path: &str,
        recursive: bool,
    ) -> Result<(), ApiError> {
        let query = [
            ("path", path),
            ("recursive", if recursive { "true" } else { "false" }),
        ];
        Self::empty(
            self.request(Method::DELETE, &Self::files(workspace, site, "files"))
                .query(&query),
        )
        .await
    }

    /// Trusts the key of a server in a workspace, adding it next to or replacing a different key of the same type when `mode` says so.
    ///
    /// # Errors
    ///
    /// A different key is trusted and no mode was given (`host_key_changed`), the role may not trust keys, or the server cannot be reached.
    pub async fn trust_key(&self, workspace: &str, key: &HostKey) -> Result<(), ApiError> {
        let mut body = HostKeyInput::new(
            key.host.clone(),
            i32::from(key.port),
            key.key_type.clone(),
            key.fingerprint.clone(),
        );
        body.mode = key.mode.map(|mode| {
            if mode == TrustMode::Add {
                Mode::Add
            } else {
                Mode::Replace
            }
        });
        Self::empty(
            self.request(
                Method::POST,
                &format!("/api/v1/workspaces/{workspace}/known-hosts"),
            )
            .json(&body),
        )
        .await
    }

    /// Gives the password of a site that asks for it when connecting, held by the server in memory for this session only.
    ///
    /// # Errors
    ///
    /// The site does not ask for a password, or the server cannot be reached.
    pub async fn set_credential(
        &self,
        workspace: &str,
        site: &str,
        password: &str,
    ) -> Result<(), ApiError> {
        let body = SiteCredentialRequest::new(password.to_owned(), false);
        Self::empty(
            self.request(Method::PUT, &Self::files(workspace, site, "credential"))
                .json(&body),
        )
        .await
    }

    /// Hands the answers to the questions of a server, one per question, to the connection that waits.
    ///
    /// # Errors
    ///
    /// The prompt no longer waits (`prompt_not_found`), the number of answers differs (`invalid_answers`), or the server cannot be reached.
    pub async fn answer_prompt(&self, id: &str, answers: Vec<String>) -> Result<(), ApiError> {
        let body = PromptAnswers::new(answers);
        Self::empty(
            self.request(Method::POST, &format!("/api/v1/prompts/{id}"))
                .json(&body),
        )
        .await
    }

    /// Refuses to answer the questions of a server; the connection that waits fails.
    ///
    /// # Errors
    ///
    /// The prompt no longer waits, or the server cannot be reached.
    pub async fn cancel_prompt(&self, id: &str) -> Result<(), ApiError> {
        Self::empty(self.request(Method::DELETE, &format!("/api/v1/prompts/{id}"))).await
    }

    /// Uploads a file from the disk as it is read, from `offset` when resuming, under a transfer the client names to follow and
    /// resume it. `mode` is `create`, `overwrite` or `resume`.
    ///
    /// # Errors
    ///
    /// The file exists and `mode` is `create`, the account may not write, the connection drops, or as listing a folder.
    #[allow(
        clippy::too_many_arguments,
        reason = "the parameters of the upload, as the API names them"
    )]
    pub async fn upload(
        &self,
        workspace: &str,
        site: &str,
        path: &str,
        file: tokio::fs::File,
        size: u64,
        mode: &str,
        offset: u64,
        transfer: &str,
    ) -> Result<RemoteEntry, ApiError> {
        let query = [
            ("path", path.to_owned()),
            ("mode", mode.to_owned()),
            ("offset", offset.to_string()),
            ("transferId", transfer.to_owned()),
            ("size", size.to_string()),
        ];
        let request = self
            .request(Method::PUT, &Self::files(workspace, site, "files/content"))
            .query(&query)
            .header("Content-Type", "application/octet-stream");
        Self::send(request.body(reqwest::Body::from(file))).await
    }

    /// Starts downloading a file, from byte `from` when resuming; the answer carries the bytes to read as they arrive.
    ///
    /// # Errors
    ///
    /// The file does not exist, the range is past its end, or as listing a folder.
    pub async fn download(
        &self,
        workspace: &str,
        site: &str,
        path: &str,
        transfer: &str,
        from: u64,
    ) -> Result<reqwest::Response, ApiError> {
        let mut request = self
            .request(Method::GET, &Self::files(workspace, site, "files/content"))
            .query(&[("path", path), ("transferId", transfer)]);
        if from > 0 {
            request = request.header("Range", format!("bytes={from}-"));
        }
        let response = request.send().await?;
        let status = response.status();
        if status.is_success() {
            return Ok(response);
        }
        Err(refusal(status, &response.bytes().await?))
    }

    /// Copies a file or folder from one site to another on the server, with a policy for what exists.
    ///
    /// # Errors
    ///
    /// A site is not of the workspace, the account may not write, or the server cannot be reached.
    pub async fn copy(
        &self,
        workspace: &str,
        body: &CopyInput,
    ) -> Result<crate::ui::transfers::Row, ApiError> {
        Self::send(
            self.request(
                Method::POST,
                &format!("/api/v1/workspaces/{workspace}/copies"),
            )
            .json(body),
        )
        .await
    }

    /// Pauses, resumes or cancels a transfer: `action` is `pause`, `resume` or `cancel`.
    ///
    /// # Errors
    ///
    /// The transfer is not of the account or cannot do that now, or the server cannot be reached.
    pub async fn transfer_action(&self, transfer: &str, action: &str) -> Result<(), ApiError> {
        Self::empty(self.request(
            Method::POST,
            &format!("/api/v1/transfers/{transfer}/{action}"),
        ))
        .await
    }

    /// Changes the place of a waiting transfer in the queue, from -1 (low) to 1 (high).
    ///
    /// # Errors
    ///
    /// The transfer is not of the account or no longer waits, or the server cannot be reached.
    pub async fn transfer_priority(&self, transfer: &str, priority: i32) -> Result<(), ApiError> {
        let body = serde_json::json!({"priority": priority});
        Self::empty(
            self.request(Method::PATCH, &format!("/api/v1/transfers/{transfer}"))
                .json(&body),
        )
        .await
    }

    /// Closes the connections of this session to a site.
    ///
    /// # Errors
    ///
    /// The session ended or the server cannot be reached.
    pub async fn disconnect(&self, workspace: &str, site: &str) -> Result<(), ApiError> {
        Self::empty(self.request(Method::DELETE, &Self::files(workspace, site, "connection"))).await
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

/// Changes a saved site or tests a draft without saving it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SiteAction {
    /// Creates a site from its input.
    Create(serde_json::Value),
    /// Updates the identified site with its input.
    Edit(uuid::Uuid, serde_json::Value),
    /// Deletes the identified site.
    Delete(uuid::Uuid),
    /// Changes the collection of the identified site.
    Move(uuid::Uuid, serde_json::Value),
    /// Tests the draft and its supplied or stored secrets.
    Test(serde_json::Value),
}

/// The refusal of a status and its body: the stable code of a problem document, or `http_<status>` without one.
fn refusal(status: StatusCode, body: &[u8]) -> ApiError {
    let (code, params) = serde_json::from_slice::<Problem>(body).map_or_else(
        |_| (format!("http_{}", status.as_u16()), HashMap::new()),
        |problem| (problem.code, problem.params.unwrap_or_default()),
    );
    ApiError::Refused {
        status: status.as_u16(),
        code,
        params,
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

    /// Site requests send the bearer token, the contract's paths and the write-only inputs.
    #[tokio::test]
    async fn manages_a_workspace_site() {
        use serde_json::json;
        use uuid::Uuid;

        let server = MockServer::start().await;
        let workspace = Uuid::from_u128(1);
        let site = Uuid::from_u128(2);
        let base = format!("/api/v1/workspaces/{workspace}/sites");
        let item = format!("{base}/{site}");
        let draft =
            json!({"name": "files", "connector": "sftp", "config": {"host": "files.example.net"}});
        let copy =
            json!({"name": "files copy", "connector": "sftp", "config": {}, "secretsFrom": site});
        let tested = json!({"siteId": site, "site": draft});
        let moved = json!({"collectionId": Uuid::from_u128(3)});
        Mock::given(method("GET"))
            .and(path("/api/v1/connectors"))
            .and(header("Authorization", "Bearer token"))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!([{
                "type": "sftp", "name": "SFTP", "icon": "sftp", "maturity": "stable",
                "capabilities": {"folders": true, "permissions": true, "setTimes": true,
                    "resume": true, "rename": true, "serverCopy": true},
                "fields": [], "enabledByDefault": true, "enabled": true
            }])))
            .expect(1)
            .mount(&server)
            .await;
        for (verb, url, body, status) in [
            ("POST", base, copy.clone(), 201),
            ("PUT", item.clone(), draft.clone(), 200),
            ("POST", format!("{item}/move"), moved.clone(), 200),
            (
                "POST",
                format!("/api/v1/workspaces/{workspace}/sites/test"),
                tested.clone(),
                422,
            ),
        ] {
            let response = ResponseTemplate::new(status).set_body_json(json!({
                "type": "about:blank", "title": "Invalid field", "status": 422,
                "code": "invalid_config", "params": {"field": "host", "reason": "host"}
            }));
            Mock::given(method(verb))
                .and(path(url))
                .and(header("Authorization", "Bearer token"))
                .and(body_json(body))
                .respond_with(response)
                .expect(1)
                .mount(&server)
                .await;
        }
        Mock::given(method("DELETE"))
            .and(path(item))
            .and(header("Authorization", "Bearer token"))
            .respond_with(ResponseTemplate::new(204))
            .expect(1)
            .mount(&server)
            .await;
        let api = api(&server).with_token("token");
        let connectors = api.connectors().await.expect("catalog");
        assert_eq!(connectors[0].r#type, "sftp");
        for action in [
            SiteAction::Create(copy),
            SiteAction::Edit(site, draft),
            SiteAction::Move(site, moved),
            SiteAction::Delete(site),
        ] {
            api.site_action(workspace, &action)
                .await
                .expect("site action");
        }
        let error = api
            .site_action(workspace, &SiteAction::Test(tested))
            .await
            .expect_err("test refusal");
        assert_eq!(error.code(), Some("invalid_config"));
        assert_eq!(error.params()["field"], "host");
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
            matches!(api(&missing).probe().await, Err(ApiError::Refused { status: 404, ref code, .. }) if code == "http_404")
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

    /// The calls of the explorer: list a folder, find the home folder, create, rename and delete, and disconnect.
    #[allow(clippy::too_many_lines, reason = "one call of each operation")]
    #[tokio::test]
    async fn works_with_files() {
        use wiremock::matchers::query_param;
        let server = MockServer::start().await;
        let base = "/api/v1/workspaces/w/sites/s";
        let entry = serde_json::json!({"name": "a", "path": "/a", "type": "directory", "size": 0, "mode": "drwxr-xr-x", "modifiedAt": "2026-01-01T00:00:00Z", "modifiedPrecision": "second"});
        Mock::given(method("GET"))
            .and(path(format!("{base}/files")))
            .and(query_param("path", "/"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([entry])))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path(format!("{base}/files/home")))
            .respond_with(
                ResponseTemplate::new(200).set_body_json(serde_json::json!({"path": "/home/ana"})),
            )
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path(format!("{base}/files/directories")))
            .and(body_json(serde_json::json!({"path": "/a"})))
            .respond_with(ResponseTemplate::new(201).set_body_json(&entry))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path(format!("{base}/files/rename")))
            .and(body_json(serde_json::json!({"from": "/a", "to": "/b"})))
            .respond_with(ResponseTemplate::new(200).set_body_json(&entry))
            .mount(&server)
            .await;
        Mock::given(method("DELETE"))
            .and(path(format!("{base}/files")))
            .and(query_param("recursive", "true"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        Mock::given(method("DELETE")).and(path(format!("{base}/files"))).and(query_param("recursive", "false")).respond_with(ResponseTemplate::new(404).set_body_json(serde_json::json!({"type": "about:blank", "title": "x", "status": 404, "code": "not_found"}))).mount(&server).await;
        Mock::given(method("DELETE"))
            .and(path(format!("{base}/connection")))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        let api = api(&server).with_token("t");
        assert_eq!(api.list("w", "s", "/").await.expect("list")[0].name, "a");
        assert_eq!(api.home("w", "s").await.expect("home").path, "/home/ana");
        assert_eq!(api.mkdir("w", "s", "/a").await.expect("mkdir").path, "/a");
        assert_eq!(
            api.rename("w", "s", "/a", "/b").await.expect("rename").name,
            "a"
        );
        api.delete("w", "s", "/a", true).await.expect("deleted");
        assert_eq!(
            api.delete("w", "s", "/a", false)
                .await
                .expect_err("refused")
                .code(),
            Some("not_found")
        );
        api.disconnect("w", "s").await.expect("disconnected");
        Mock::given(method("POST")).and(path("/api/v1/workspaces/w/known-hosts")).and(body_json(serde_json::json!({"host": "h", "port": 22, "keyType": "t", "fingerprint": "f", "mode": "add"}))).respond_with(ResponseTemplate::new(201)).mount(&server).await;
        Mock::given(method("POST")).and(path("/api/v1/workspaces/w/known-hosts")).and(body_json(serde_json::json!({"host": "h", "port": 22, "keyType": "t", "fingerprint": "f", "mode": "replace"}))).respond_with(ResponseTemplate::new(201)).mount(&server).await;
        Mock::given(method("POST")).and(path("/api/v1/workspaces/w/known-hosts")).respond_with(ResponseTemplate::new(409).set_body_json(serde_json::json!({"type": "about:blank", "title": "x", "status": 409, "code": "host_key_changed", "params": {"trusted": ["old"]}}))).mount(&server).await;
        let key = |mode| HostKey {
            host: "h".to_owned(),
            port: 22,
            key_type: "t".to_owned(),
            fingerprint: "f".to_owned(),
            mode,
        };
        api.trust_key("w", &key(Some(TrustMode::Add)))
            .await
            .expect("added");
        api.trust_key("w", &key(Some(TrustMode::Replace)))
            .await
            .expect("replaced");
        Mock::given(method("PUT"))
            .and(path(format!("{base}/credential")))
            .and(body_json(
                serde_json::json!({"password": "p", "remember": false}),
            ))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        api.set_credential("w", "s", "p").await.expect("given");
        Mock::given(method("POST"))
            .and(path("/api/v1/prompts/p"))
            .and(body_json(serde_json::json!({"answers": ["a"]})))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        Mock::given(method("DELETE"))
            .and(path("/api/v1/prompts/p"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        api.answer_prompt("p", vec!["a".to_owned()])
            .await
            .expect("answered");
        api.cancel_prompt("p").await.expect("refused");
        let refused = api.trust_key("w", &key(None)).await.expect_err("changed");
        assert_eq!(refused.params()["trusted"], serde_json::json!(["old"]));
        assert!(ApiError::Unexpected("x".to_owned()).params().is_empty());
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
