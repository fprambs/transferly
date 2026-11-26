//! The terminal client against a real server and a real SFTP server: the isolated stack of `compose.test.yaml`, run by
//! `make tui-it`. It signs the terminal in through the same approval the browser gives, then uploads, lists, downloads, resumes and
//! copies between sites through the API wrapper of the client. It is ignored by `make tui-test`, which has no stack.

use std::collections::HashMap;
use std::time::Duration;

use base64::Engine;
use sha2::{Digest, Sha256};
use transferly_tui::api::{Api, HostKey};
use url::Url;

/// The server of the stack, from `TRANSFERLY_IT_URL`.
fn server() -> Url {
    Url::parse(&std::env::var("TRANSFERLY_IT_URL").expect("TRANSFERLY_IT_URL")).expect("an address")
}

/// The account of the test.
const EMAIL: &str = "tui@example.com";
/// Its password.
const PASSWORD: &str = "a long enough password for the tui";

/// Signs a browser in, creating the account on a fresh stack, and returns a client that keeps its cookie with its anti-CSRF token.
async fn browser() -> (reqwest::Client, String) {
    let client = reqwest::Client::builder()
        .cookie_store(true)
        .build()
        .expect("a client");
    let base = server();
    let account = serde_json::json!({"email": EMAIL, "password": PASSWORD});
    let mut answer = client
        .post(base.join("/api/v1/setup").expect("url"))
        .json(&account)
        .send()
        .await
        .expect("setup");
    if !answer.status().is_success() {
        answer = client
            .post(base.join("/api/v1/auth/login").expect("url"))
            .json(&account)
            .send()
            .await
            .expect("login");
    }
    let session: serde_json::Value = answer.json().await.expect("a session");
    (
        client,
        session["csrfToken"].as_str().expect("a token").to_owned(),
    )
}

/// Signs the terminal in as a person would: the browser approves a request with the password, and the code is exchanged with the
/// verifier for a token.
async fn sign_in(browser: &reqwest::Client, csrf: &str) -> String {
    let verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
    let challenge =
        base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(Sha256::digest(verifier));
    let approval = serde_json::json!({"codeChallenge": challenge, "clientName": "transferly-tui", "clientHost": "testenv", "password": PASSWORD});
    let approved: serde_json::Value = browser
        .post(server().join("/api/v1/cli/authorizations").expect("url"))
        .header("X-CSRF-Token", csrf)
        .json(&approval)
        .send()
        .await
        .expect("approved")
        .json()
        .await
        .expect("a code");
    let token = Api::new(server())
        .exchange(approved["code"].as_str().expect("a code"), verifier, None)
        .await
        .expect("a token");
    token.token
}

/// Sends a request as the terminal and returns its JSON answer.
async fn call(
    token: &str,
    method: reqwest::Method,
    path: &str,
    body: serde_json::Value,
) -> serde_json::Value {
    let answer = reqwest::Client::new()
        .request(method, server().join(path).expect("url"))
        .bearer_auth(token)
        .json(&body)
        .send()
        .await
        .expect("an answer");
    let status = answer.status();
    let text = answer.text().await.expect("a body");
    assert!(status.is_success(), "{path}: {status} {text}");
    serde_json::from_str(&text).unwrap_or(serde_json::Value::Null)
}

/// The whole round trip against SFTP: trust the key, upload, list, download, resume a download, refuse what exists, and copy between
/// two sites on the server until the copy is done.
#[tokio::test]
#[ignore = "needs the isolated stack: make tui-it"]
#[allow(clippy::too_many_lines, reason = "one walk through every way of moving bytes, in order")]
async fn moves_files_through_a_real_server() {
    let (browser, csrf) = browser().await;
    let token = sign_in(&browser, &csrf).await;
    let api = Api::new(server()).with_token(&token);
    assert_eq!(api.me().await.expect("me").user.email, EMAIL);

    let vault = api.vault().await.expect("the vault");
    if vault.initialized {
        api.unlock_vault(PASSWORD).await.expect("unlocked");
    } else {
        call(
            &token,
            reqwest::Method::POST,
            "/api/v1/vault/setup",
            serde_json::json!({"masterPassword": PASSWORD}),
        )
        .await;
    }
    let workspace = api.workspaces().await.expect("workspaces")[0]
        .id
        .to_string();
    let suffix = uuid::Uuid::new_v4().simple().to_string();
    let site = |name: &str, dir: &str| {
        serde_json::json!({"name": format!("{name}-{suffix}"), "connector": "sftp", "remoteDir": dir,
            "config": {"host": "sftp", "port": "22", "auth": "password", "username": "transferly"}, "secrets": {"password": "transferly"}})
    };
    let sites = format!("/api/v1/workspaces/{workspace}/sites");
    let first = call(
        &token,
        reqwest::Method::POST,
        &sites,
        site("tui-a", "/upload"),
    )
    .await["id"]
        .as_str()
        .expect("an id")
        .to_owned();
    let second = call(
        &token,
        reqwest::Method::POST,
        &sites,
        site("tui-b", "/upload"),
    )
    .await["id"]
        .as_str()
        .expect("an id")
        .to_owned();

    if let Err(error) = api.list(&workspace, &first, "/upload").await {
        assert!(matches!(error.code(), Some("host_key_unknown")), "{error}");
        let params: HashMap<String, serde_json::Value> = error.params();
        let text = |name: &str| params[name].as_str().expect("a param").to_owned();
        let key = HostKey {
            host: "sftp".to_owned(),
            port: 22,
            key_type: text("keyType"),
            fingerprint: text("fingerprint"),
            mode: None,
        };
        api.trust_key(&workspace, &key).await.expect("trusted");
    }
    let folder = format!("/upload/tui-{suffix}");
    api.mkdir(&workspace, &first, &folder)
        .await
        .expect("a folder");

    let dir = tempfile::tempdir().expect("a folder");
    let local = dir.path().join("payload.bin");
    let payload: Vec<u8> = (0..300_000_u32).map(|n| (n % 251) as u8).collect();
    std::fs::write(&local, &payload).expect("a file");
    let remote = format!("{folder}/payload.bin");
    let id = uuid::Uuid::new_v4().to_string();
    let file = tokio::fs::File::open(&local).await.expect("the file");
    let size = u64::try_from(payload.len()).expect("a size");
    api.upload(&workspace, &first, &remote, file, size, "create", 0, &id)
        .await
        .expect("uploaded");
    let listed = api.list(&workspace, &first, &folder).await.expect("listed");
    assert!(
        listed.iter().any(|entry| entry.name == "payload.bin"
            && entry.size == i64::try_from(size).expect("a size"))
    );
    let file = tokio::fs::File::open(&local).await.expect("the file");
    let again = api
        .upload(
            &workspace,
            &first,
            &remote,
            file,
            size,
            "create",
            0,
            &uuid::Uuid::new_v4().to_string(),
        )
        .await
        .expect_err("it exists");
    assert!(again.code().is_some(), "{again}");

    let whole = api
        .download(
            &workspace,
            &first,
            &remote,
            &uuid::Uuid::new_v4().to_string(),
            0,
        )
        .await
        .expect("downloading")
        .bytes()
        .await
        .expect("the bytes");
    assert_eq!(whole.as_ref(), payload.as_slice());
    let tail = api
        .download(
            &workspace,
            &first,
            &remote,
            &uuid::Uuid::new_v4().to_string(),
            100_000,
        )
        .await
        .expect("resuming");
    assert_eq!(tail.status(), reqwest::StatusCode::PARTIAL_CONTENT);
    assert_eq!(
        tail.bytes().await.expect("the bytes").as_ref(),
        &payload[100_000..]
    );

    let mut body = transferly_client::models::CopyInput::new(
        first.parse().expect("an id"),
        remote.clone(),
        second.parse().expect("an id"),
        format!("{folder}/copied.bin"),
    );
    body.policy = Some(transferly_client::models::copy_input::Policy::Rename);
    let copy = api.copy(&workspace, &body).await.expect("a copy");
    let mut status = String::new();
    for _ in 0..60 {
        let transfer = call(
            &token,
            reqwest::Method::GET,
            &format!("/api/v1/transfers/{}", copy.id),
            serde_json::Value::Null,
        )
        .await;
        status = transfer["status"].as_str().unwrap_or_default().to_owned();
        if status == "done" || status == "failed" {
            break;
        }
        tokio::time::sleep(Duration::from_millis(500)).await;
    }
    assert_eq!(status, "done");
    let copied = api
        .list(&workspace, &second, &folder)
        .await
        .expect("listed");
    assert!(
        copied.iter().any(|entry| entry.name == "copied.bin"),
        "{copied:?}"
    );
    api.delete(&workspace, &first, &folder, true)
        .await
        .expect("cleaned up");
}
