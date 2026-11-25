//! Tests of the interface: its transitions, its screens drawn on a test backend, and the loop driven against a mock server.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use crossterm::event::{KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::Terminal;
use ratatui::backend::TestBackend;
use ratatui::style::Color;
use wiremock::matchers::{body_partial_json, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

use super::app::{App, Effect, Event, Failure, Kept, Screen, Setup};
use super::runtime::{self, Ports};
use super::theme::ColorMode;
use super::*;
use crate::config::{Profile, Settings};
use crate::i18n::Language;
use crate::login::Answer;
use crate::secrets;

/// A pressed key.
fn key(code: KeyCode) -> Event {
    Event::Key(KeyEvent::new(code, KeyModifiers::NONE))
}

/// Types text, one key per character.
fn type_text(app: &mut App, text: &str) {
    for c in text.chars() {
        app.handle(key(KeyCode::Char(c)));
    }
}

/// A setup with English, truecolor, the given profiles and browser, and nobody signed in.
fn setup_with(profiles: Vec<Profile>, browser_possible: bool) -> Setup {
    Setup {
        language: Language::English,
        mode: ColorMode::TrueColor,
        theme: DEFAULT_THEME.to_owned(),
        settings: Settings::default(),
        profiles,
        host: "laptop".to_owned(),
        browser_possible,
        signed_in: None,
    }
}

/// The session kept for files.example.com, with the token `t`.
fn kept(email: &str) -> Kept {
    Kept {
        server: "https://files.example.com".to_owned(),
        email: email.to_owned(),
        token: "t".to_owned(),
    }
}

/// A profile of a server.
fn profile(server: &str) -> Profile {
    Profile {
        server: server.to_owned(),
        account: Some("ana@example.com".to_owned()),
    }
}

/// The text of every row of the screen.
fn screen_text(app: &App) -> String {
    let mut terminal = Terminal::new(TestBackend::new(90, 26)).expect("backend");
    terminal.draw(|frame| app.render(frame)).expect("draw");
    let buffer = terminal.backend().buffer();
    (0..26)
        .map(|y| {
            (0..90)
                .map(|x| buffer[(x, y)].symbol().to_owned())
                .collect::<String>()
                .trim_end()
                .to_owned()
                + "\n"
        })
        .collect()
}

/// Signing in through the browser: the address is checked, the listener starts, the browser opens, the answer is exchanged, and
/// the account brings its language and theme.
#[test]
fn signs_in_through_the_browser() {
    let mut app = App::new(setup_with(vec![], true));
    assert!(screen_text(&app).contains("Server address"));
    type_text(&mut app, "files.example.com");
    let effects = app.handle(key(KeyCode::Enter));
    assert_eq!(
        effects,
        vec![Effect::Probe(
            url::Url::parse("https://files.example.com").expect("url")
        )]
    );
    assert!(screen_text(&app).contains("Checking https://files.example.com"));
    assert!(
        app.handle(key(KeyCode::Char('x'))).is_empty(),
        "keys wait while checking"
    );

    let effects = app.handle(Event::Probed(Ok(())));
    let Effect::Listen { state } = &effects[0] else {
        panic!("{effects:?}")
    };
    let effects = app.handle(Event::Listening(Ok(
        "http://127.0.0.1:5000/callback".to_owned()
    )));
    let Effect::OpenBrowser(address) = &effects[0] else {
        panic!("{effects:?}")
    };
    assert!(
        address.contains(&format!("state={state}"))
            && address.contains("redirect_uri=")
            && address.contains("client_host=laptop")
    );
    assert!(screen_text(&app).contains("Open the address below in your browser"));
    app.handle(Event::BrowserOpened(true));
    app.handle(Event::Tick);
    let text = screen_text(&app);
    assert!(
        text.contains("Your browser opened the address below")
            && text.contains("Waiting for the approval")
    );

    assert_eq!(
        app.handle(key(KeyCode::Char('o'))),
        vec![Effect::OpenBrowser(address.clone())]
    );
    assert_eq!(
        app.handle(key(KeyCode::Char('c'))),
        vec![Effect::Copy(address.clone())]
    );
    assert!(screen_text(&app).contains("The address was copied."));
    assert_eq!(app.handle(key(KeyCode::Char('z'))), Vec::<Effect>::new());

    let effects = app.handle(Event::Answered(Ok(Answer::Approved("code-1".to_owned()))));
    let Effect::Exchange { code, redirect, .. } = &effects[0] else {
        panic!("{effects:?}")
    };
    assert_eq!(
        (code.as_str(), redirect.as_deref()),
        ("code-1", Some("http://127.0.0.1:5000/callback"))
    );
    assert!(
        app.handle(key(KeyCode::Char('o'))).is_empty(),
        "keys wait while exchanging"
    );
    app.handle(Event::SignedIn {
        server: "https://files.example.com".into(),
        email: "ana@example.com".into(),
        locale: "es".into(),
        theme: "nord".into(),
        token: "t".into(),
    });
    assert!(matches!(app.screen(), Screen::Main(home) if home.session().token == "t"));
    assert_eq!(app.messages().language(), Language::Spanish);
    assert_eq!(app.palette().accent, Color::Rgb(0x88, 0xc0, 0xd0));
    assert!(screen_text(&app).contains("Disco local"));
    assert_eq!(app.handle(key(KeyCode::Char('x'))), Vec::<Effect>::new());
    assert_eq!(app.handle(key(KeyCode::Char('q'))), vec![Effect::Quit]);
}

/// Without a browser, or when the listener cannot start or the person asks, the code is pasted; a refused code stays to fix.
#[test]
fn signs_in_with_a_pasted_code() {
    let mut app = App::new(setup_with(vec![], false));
    type_text(&mut app, "files.example.com");
    app.handle(key(KeyCode::Enter));
    assert_eq!(app.handle(Event::Probed(Ok(()))), Vec::<Effect>::new());
    let Screen::Paste { url, .. } = app.screen() else {
        panic!("{:?}", app.screen())
    };
    assert!(!url.contains("redirect_uri"));
    assert!(screen_text(&app).contains("Then paste the code the page shows."));
    assert!(
        app.handle(key(KeyCode::Enter)).is_empty(),
        "an empty code is not sent"
    );
    app.handle(Event::Paste("  the-code\n".to_owned()));
    app.handle(key(KeyCode::Char('x')));
    app.handle(key(KeyCode::Backspace));
    let effects = app.handle(key(KeyCode::Enter));
    let Effect::Exchange { code, redirect, .. } = &effects[0] else {
        panic!("{effects:?}")
    };
    assert_eq!((code.as_str(), redirect), ("the-code", &None));
    assert!(
        app.handle(key(KeyCode::Char('y'))).is_empty(),
        "keys wait while exchanging"
    );
    app.handle(Event::Tick);
    assert!(screen_text(&app).contains('⠙'));
    app.handle(Event::SignInFailed(Failure::Refused(
        "invalid_grant".to_owned(),
    )));
    assert!(screen_text(&app).contains("The code is not valid any more"));
    assert_eq!(app.handle(key(KeyCode::Esc)), vec![]);
    assert!(matches!(app.screen(), Screen::Server { error: None, .. }));

    let mut app = App::new(setup_with(vec![], true));
    type_text(&mut app, "files.example.com");
    app.handle(key(KeyCode::Enter));
    app.handle(Event::Probed(Ok(())));
    app.handle(Event::Listening(Err("no ports".to_owned())));
    assert!(matches!(app.screen(), Screen::Paste { .. }));

    let mut app = App::new(setup_with(vec![], true));
    type_text(&mut app, "files.example.com");
    app.handle(key(KeyCode::Enter));
    app.handle(Event::Probed(Ok(())));
    app.handle(Event::Listening(Ok(
        "http://127.0.0.1:5000/callback".to_owned()
    )));
    assert_eq!(
        app.handle(key(KeyCode::Char('p'))),
        vec![Effect::StopListening]
    );
    assert!(matches!(app.screen(), Screen::Paste { .. }));
    app.handle(Event::BrowserOpened(true));
    assert!(
        matches!(app.screen(), Screen::Paste { .. }),
        "a late browser does not leave the paste screen"
    );
}
/// The address is checked before anything is sent, profiles are chosen with the arrows, and an unreachable server goes back to the
/// address with the reason.
#[test]
fn explains_what_went_wrong() {
    let mut app = App::new(setup_with(
        vec![
            profile("https://a.example.com"),
            profile("https://b.example.com"),
        ],
        true,
    ));
    let text = screen_text(&app);
    assert!(text.contains("Used before") && text.contains("https://b.example.com"));
    app.handle(key(KeyCode::Char('u')));
    app.handle(Event::Key(KeyEvent::new(
        KeyCode::Char('u'),
        KeyModifiers::CONTROL,
    )));
    type_text(&mut app, "not a url");
    assert_eq!(app.handle(key(KeyCode::Enter)), Vec::<Effect>::new());
    assert!(screen_text(&app).contains("That is not the address of a server"));

    app.handle(key(KeyCode::Down));
    app.handle(key(KeyCode::Down));
    app.handle(key(KeyCode::Down));
    app.handle(key(KeyCode::Up));
    app.handle(key(KeyCode::Up));
    app.handle(key(KeyCode::Up));
    app.handle(key(KeyCode::Down));
    app.handle(key(KeyCode::Down));
    assert!(matches!(
        app.screen(),
        Screen::Server {
            selected: Some(1),
            ..
        }
    ));
    screen_text(&app);
    assert_eq!(
        app.handle(key(KeyCode::Enter)),
        vec![Effect::Probe(
            url::Url::parse("https://b.example.com").expect("url")
        )]
    );
    app.handle(Event::Probed(Err("refused".to_owned())));
    assert!(screen_text(&app).contains("https://b.example.com is not a Transferly server"));

    app.handle(key(KeyCode::Down));
    app.handle(Event::Paste("x".to_owned()));
    assert!(matches!(
        app.screen(),
        Screen::Server { selected: None, .. }
    ));
    app.handle(key(KeyCode::Down));
    app.handle(key(KeyCode::Char('z')));
    assert!(matches!(
        app.screen(),
        Screen::Server { selected: None, .. }
    ));
    app.handle(key(KeyCode::F(5)));
}

/// Denials, failed waits and failed exchanges go back to the address with the reason; Esc on the approval stops the listener.
#[test]
fn explains_failed_sign_ins() {
    for (event, message) in [
        (
            Event::Answered(Ok(Answer::Denied)),
            "The sign-in was denied in the browser.",
        ),
        (
            Event::Answered(Err("timed out".to_owned())),
            "The sign-in failed: timed out",
        ),
        (
            Event::SignInFailed(Failure::Refused("cli_login_disabled".to_owned())),
            "turned off",
        ),
        (
            Event::SignInFailed(Failure::Refused("http_404".to_owned())),
            "too old",
        ),
        (
            Event::SignInFailed(Failure::Refused("rate_limited".to_owned())),
            "The sign-in failed: rate_limited",
        ),
        (
            Event::SignInFailed(Failure::Unreachable("reset".to_owned())),
            "The sign-in failed: reset",
        ),
        (
            Event::SignInFailed(Failure::NotKept("locked".to_owned())),
            "the token could not be kept: locked",
        ),
    ] {
        let mut app = App::new(setup_with(vec![], true));
        type_text(&mut app, "files.example.com");
        app.handle(key(KeyCode::Enter));
        app.handle(Event::Probed(Ok(())));
        app.handle(Event::Listening(Ok(
            "http://127.0.0.1:5000/callback".to_owned()
        )));
        app.handle(event);
        let text = screen_text(&app);
        assert!(text.contains(message), "{message}: {text}");
        assert!(matches!(app.screen(), Screen::Server { .. }));
    }

    let mut app = App::new(setup_with(vec![], true));
    type_text(&mut app, "files.example.com");
    app.handle(key(KeyCode::Enter));
    app.handle(Event::Probed(Ok(())));
    app.handle(Event::Listening(Ok(
        "http://127.0.0.1:5000/callback".to_owned()
    )));
    assert_eq!(app.handle(key(KeyCode::Esc)), vec![Effect::StopListening]);
    assert!(matches!(app.screen(), Screen::Server { .. }));
    app.handle(Event::Paste("ignored".to_owned()));
}

/// Ctrl+C quits from anywhere; Esc quits from the address; released keys are ignored; signed in, the screen says where.
#[test]
fn quits_and_ignores_releases() {
    let mut app = App::new(setup_with(vec![], true));
    let mut release = KeyEvent::new(KeyCode::Esc, KeyModifiers::NONE);
    release.kind = KeyEventKind::Release;
    assert_eq!(app.handle(Event::Key(release)), Vec::<Effect>::new());
    assert_eq!(
        app.handle(Event::Key(KeyEvent::new(
            KeyCode::Char('c'),
            KeyModifiers::CONTROL
        ))),
        vec![Effect::StopListening, Effect::Quit]
    );
    assert_eq!(app.handle(key(KeyCode::Esc)), vec![Effect::Quit]);

    let mut signed = setup_with(vec![], true);
    signed.signed_in = Some(kept("ana@example.com"));
    signed.mode = ColorMode::Plain;
    let mut app = App::new(signed);
    assert!(screen_text(&app).contains("files.example.com · ana@example.com"));
    app.handle(Event::Paste("ignored".to_owned()));

    let mut plain = setup_with(vec![profile("https://a.example.com")], true);
    plain.mode = ColorMode::Plain;
    let mut app = App::new(plain);
    app.handle(key(KeyCode::Down));
    assert!(screen_text(&app).contains("https://a.example.com"));
}

/// Terminal events become keys, pasted text and redraws; the rest is ignored.
#[test]
fn reads_terminal_events() {
    let pressed = KeyEvent::new(KeyCode::Enter, KeyModifiers::NONE);
    assert_eq!(
        from_terminal(TerminalEvent::Key(pressed)),
        Some(Event::Key(pressed))
    );
    assert_eq!(
        from_terminal(TerminalEvent::Paste("x".into())),
        Some(Event::Paste("x".into()))
    );
    assert_eq!(
        from_terminal(TerminalEvent::Resize(80, 24)),
        Some(Event::Tick)
    );
    assert_eq!(from_terminal(TerminalEvent::FocusGained), None);
}

/// The setup takes the language and colors of the environment, and the server already signed in to only when its token is kept.
#[test]
fn sets_up_from_this_computer() {
    let dir = tempfile::tempdir().expect("folder");
    let tokens = secrets::open(dir.path(), Err(keyring_core::Error::NoDefaultStore));
    let mut config = Config::default();
    config.signed_in("https://files.example.com", "ana@example.com");
    let env = |name: &str| (name == "LANG").then(|| "es_CL.UTF-8".to_owned());
    let without_token = setup(&config, tokens.as_ref(), env, "laptop".to_owned());
    assert_eq!(
        (
            without_token.language,
            without_token.mode,
            without_token.signed_in
        ),
        (Language::Spanish, ColorMode::Indexed, None)
    );
    tokens.set("https://files.example.com", "t").expect("token");
    let with_token = setup(&config, tokens.as_ref(), env, "laptop".to_owned());
    assert_eq!(with_token.signed_in, Some(kept("ana@example.com")));
    config.profiles.clear();
    assert_eq!(
        setup(&config, tokens.as_ref(), env, "laptop".to_owned()).signed_in,
        Some(kept(""))
    );
}

/// A user as the server describes it.
fn user() -> serde_json::Value {
    serde_json::json!({"id": "01a20f66-e85c-71b3-9c42-2987ff8c4adb", "email": "ana@example.com", "role": "user", "locale": "es",
        "timezone": "UTC", "theme": "dracula", "fileDoubleClick": "download"})
}

/// Ports over a folder, the given tokens and browser, recording what was copied.
pub(super) fn ports(
    dir: &std::path::Path,
    tokens: Arc<dyn secrets::TokenStore>,
    browser: Arc<dyn Fn(&str) -> bool + Send + Sync>,
    copied: Arc<Mutex<Vec<String>>>,
) -> Ports {
    Ports {
        config_path: dir.join(crate::config::FILE_NAME),
        tokens,
        open_browser: browser,
        copy: Arc::new(move |text: &str| copied.lock().expect("copied").push(text.to_owned())),
        listen_on: crate::login::LOOPBACK.to_owned(),
        wait: Duration::from_secs(5),
        tick: Duration::from_millis(10),
    }
}

/// The whole loop against a mock server: the browser approves, the token is kept and the signed-in screen shows; q then quits, and the
/// loop also ends when its input does.
#[tokio::test]
async fn runs_the_sign_in_end_to_end() {
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
        .respond_with(ResponseTemplate::new(200).set_body_json(
            serde_json::json!({"token": "t", "expiresAt": "2026-12-25T00:00:00Z", "user": user()}),
        ))
        .mount(&server)
        .await;
    let dir = tempfile::tempdir().expect("folder");
    let tokens: Arc<dyn secrets::TokenStore> = Arc::from(secrets::open(
        dir.path(),
        Ok(keyring_core::mock::Store::new().expect("mock")),
    ));
    let browser: Arc<dyn Fn(&str) -> bool + Send + Sync> = Arc::new(|address: &str| {
        let url = url::Url::parse(address).expect("approval address");
        let pairs: std::collections::HashMap<_, _> = url.query_pairs().into_owned().collect();
        let callback = format!(
            "{}?code=good&state={}",
            pairs["redirect_uri"], pairs["state"]
        );
        std::thread::spawn(move || {
            let runtime = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .expect("runtime");
            runtime.block_on(async { reqwest::get(callback).await.expect("callback") });
        });
        true
    });
    let copied = Arc::new(Mutex::new(Vec::new()));
    let ports = ports(dir.path(), tokens.clone(), browser, copied.clone());
    (ports.copy)("copied");
    assert_eq!(copied.lock().expect("copied").as_slice(), ["copied"]);

    let (keys, mut receiver) = tokio::sync::mpsc::unbounded_channel::<Event>();
    for c in server.uri().chars() {
        keys.send(key(KeyCode::Char(c))).expect("key");
    }
    keys.send(key(KeyCode::Enter)).expect("enter");
    let quitter = tokio::spawn(async move {
        while keys.send(key(KeyCode::Char('q'))).is_ok() {
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
    });
    let input = Box::pin(futures::stream::poll_fn(move |context| {
        receiver.poll_recv(context)
    }));
    let mut app = App::new(setup_with(vec![], true));
    let mut terminal = Terminal::new(TestBackend::new(90, 26)).expect("backend");
    tokio::time::timeout(
        Duration::from_secs(10),
        runtime::run(&mut app, &ports, input, &mut draw_on(&mut terminal)),
    )
    .await
    .expect("the sign-in finished")
    .expect("drawing");
    assert!(matches!(app.screen(), Screen::Main(home) if home.session().token == "t"));
    assert_eq!(
        tokens.get(&server.uri()).expect("token").as_deref(),
        Some("t")
    );
    quitter.await.expect("quitter");

    let mut app = App::new(setup_with(vec![], true));
    let ended = Box::pin(futures::stream::iter(vec![key(KeyCode::Char('a'))]));
    runtime::run(&mut app, &ports, ended, &mut draw_on(&mut terminal))
        .await
        .expect("drawing");
    let mut broken = |_: &App| Err(std::io::Error::other("gone"));
    let ended = runtime::run(
        &mut app,
        &ports,
        Box::pin(futures::stream::pending()),
        &mut broken,
    )
    .await;
    assert!(
        ended.is_err(),
        "a terminal that cannot be drawn on ends the loop"
    );
}

/// Events with a pause before each, as a stream.
pub(super) fn paced(events: Vec<(u64, Event)>) -> runtime::Events<'static> {
    Box::pin(futures::stream::unfold(
        events.into_iter(),
        |mut events| async move {
            let (pause, event) = events.next()?;
            tokio::time::sleep(Duration::from_millis(pause)).await;
            Some((event, events))
        },
    ))
}

/// The other effects of the loop: an unreachable server, copying the address, a wait for the browser that runs out, and leaving the
/// approval, which stops the listener.
#[tokio::test]
async fn runs_the_other_effects() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v1/setup/status"))
        .respond_with(
            ResponseTemplate::new(200).set_body_json(serde_json::json!({"required": false})),
        )
        .mount(&server)
        .await;
    let dir = tempfile::tempdir().expect("folder");
    let tokens: Arc<dyn secrets::TokenStore> = Arc::from(secrets::open(
        dir.path(),
        Ok(keyring_core::mock::Store::new().expect("mock")),
    ));
    let copied = Arc::new(Mutex::new(Vec::new()));
    let mut ports = ports(
        dir.path(),
        tokens,
        Arc::new(|_: &str| false),
        copied.clone(),
    );
    ports.wait = Duration::from_millis(600);

    let mut events: Vec<(u64, Event)> = "http://127.0.0.1:9"
        .chars()
        .map(|c| (0, key(KeyCode::Char(c))))
        .collect();
    events.push((0, key(KeyCode::Enter)));
    events.push((
        500,
        Event::Key(KeyEvent::new(KeyCode::Char('u'), KeyModifiers::CONTROL)),
    ));
    events.extend(server.uri().chars().map(|c| (0, key(KeyCode::Char(c)))));
    events.push((0, key(KeyCode::Enter)));
    events.push((150, key(KeyCode::Char('c'))));
    events.push((800, key(KeyCode::Enter)));
    events.push((500, key(KeyCode::Esc)));
    events.push((200, key(KeyCode::Esc)));
    let mut app = App::new(setup_with(vec![], true));
    let mut terminal = Terminal::new(TestBackend::new(90, 26)).expect("backend");
    tokio::time::timeout(
        Duration::from_secs(10),
        runtime::run(&mut app, &ports, paced(events), &mut draw_on(&mut terminal)),
    )
    .await
    .expect("the loop ended")
    .expect("drawing");
    let copied = copied.lock().expect("copied");
    assert_eq!(copied.len(), 1);
    assert!(copied[0].starts_with(&format!("{}/cli/authorize?", server.uri())));
}

/// An approval that arrives once the person moved on to pasting is still exchanged.
#[test]
fn exchanges_a_late_approval() {
    let mut app = App::new(setup_with(vec![], true));
    type_text(&mut app, "files.example.com");
    app.handle(key(KeyCode::Enter));
    app.handle(Event::Probed(Ok(())));
    app.handle(Event::Listening(Ok(
        "http://127.0.0.1:5000/callback".to_owned()
    )));
    app.handle(key(KeyCode::Char('p')));
    let effects = app.handle(Event::Answered(Ok(Answer::Approved("late".to_owned()))));
    assert!(matches!(&effects[0], Effect::Exchange { code, redirect: None, .. } if code == "late"));
}

/// Draws the interface on a test backend, which never fails.
pub(super) fn draw_on(
    terminal: &mut Terminal<TestBackend>,
) -> impl FnMut(&App) -> std::io::Result<()> + '_ {
    move |app: &App| {
        terminal
            .draw(|frame| app.render(frame))
            .expect("a test backend");
        Ok(())
    }
}

/// The address to approve is drawn across the bottom of the screen from its first column, with nothing around it, so selecting it
/// with the mouse copies the address and line breaks only; the screen to paste a code shows it the same way.
#[test]
fn draws_the_address_to_copy() {
    let mut app = App::new(setup_with(vec![], true));
    type_text(&mut app, "files.example.com");
    app.handle(key(KeyCode::Enter));
    app.handle(Event::Probed(Ok(())));
    app.handle(Event::Listening(Ok(
        "http://127.0.0.1:5000/callback".to_owned()
    )));
    let mut url = String::new();
    if let Screen::Approval { url: shown, .. } = app.screen() {
        url.clone_from(shown);
    }
    for (screen, label) in [(screen_text(&app), "The address: c copies it whole"), {
        app.handle(key(KeyCode::Char('p')));
        (screen_text(&app), "The address, to select with the mouse")
    }] {
        let rows: Vec<&str> = screen.lines().collect();
        let at = rows
            .iter()
            .position(|row| row.starts_with(label))
            .expect("the label of the address");
        let copied: String = rows[at + 1..].concat();
        assert!(
            copied.starts_with("https://files.example.com/cli/authorize?code_challenge="),
            "{copied}"
        );
        assert!(
            copied.contains("client_name=transferly-tui") && !copied.contains([' ', '│']),
            "{copied}"
        );
    }
    assert!(
        screen_text(&app)
            .lines()
            .any(|row| url.starts_with(row) && row.len() > 80)
    );
    assert!(url.contains("redirect_uri="));
}
