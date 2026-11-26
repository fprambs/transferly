//! The screens of `transferly-tui` itself, drawn by its interface (`transferly_tui::ui::app::App`) after the events that lead to them,
//! not mockups: the steps of signing in, for the screenshots of make tui-mockups.

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::Terminal;
use ratatui::backend::TestBackend;
use ratatui::buffer::Buffer;
use ratatui::style::Color;
use transferly_tui::config::Profile;
use transferly_tui::i18n::Language;
use transferly_tui::login::Answer;
use transferly_tui::config::Settings;
use transferly_tui::ui::app::{App, Effect, Event, Kept, Setup};
use transferly_tui::ui::explorer::{Entry, Kind};
use transferly_tui::ui::home::{Reply, Side};
use transferly_tui::ui::theme::ColorMode;

use crate::html;

/// A screen of the application: its file name, what it shows, and the buffer it draws.
pub struct Shot {
    /// Name of the page and the image.
    pub slug: &'static str,
    /// What it shows.
    pub about: &'static str,
    /// What the interface drew.
    pub buffer: Buffer,
    /// The colors of cells without their own.
    pub defaults: html::Defaults,
}

/// A pressed key.
fn key(code: KeyCode) -> Event {
    Event::Key(KeyEvent::new(code, KeyModifiers::NONE))
}

/// The interface of a computer with two servers used before, in a language, with or without a browser.
fn app(language: Language, browser: bool) -> App {
    let profile = |server: &str, account: &str| Profile { server: server.to_owned(), account: Some(account.to_owned()) };
    App::new(Setup {
        language,
        mode: ColorMode::TrueColor,
        theme: "transferly-dark".to_owned(),
        settings: Settings::default(),
        profiles: vec![profile("https://files.acme.com", "ana@acme.com"), profile("https://transferly.lan", "admin@home.lan")],
        host: "laptop".to_owned(),
        browser_possible: browser,
        signed_in: None,
    })
}

/// The replies that load the main screen: the account, two workspaces, the tree of the personal one and a locked vault.
fn load(app: &mut App) {
    let id = |n: u8| format!("00000000-0000-0000-0000-0000000000{n:02}");
    let site = |n: u8, name: &str, collection: Option<u8>, connector: &str, host: &str| {
        serde_json::json!({"id": id(n), "collectionId": collection.map(id), "name": name, "connector": connector, "config": {"host": host},
            "speedLimitBps": 0, "secrets": ["password"], "createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-01T00:00:00Z"})
    };
    let workspaces = serde_json::json!([
        {"id": id(1), "name": "Personal", "kind": "personal", "role": "owner", "createdAt": "2026-01-01T00:00:00Z"},
        {"id": id(2), "name": "Acme team", "kind": "team", "role": "editor", "createdAt": "2026-01-01T00:00:00Z"}
    ]);
    let collections = serde_json::json!([
        {"id": id(10), "name": "Clients"}, {"id": id(11), "name": "Acme", "parentId": id(10)}, {"id": id(12), "name": "Home"}
    ]);
    let sites = serde_json::json!([
        site(20, "acme-prod", Some(11), "sftp", "sftp.acme.com"), site(21, "acme-staging", Some(11), "sftp", "staging.acme.com"),
        site(22, "globex-ftp", Some(10), "ftps", "ftp.globex.com"), site(23, "nas", Some(12), "sftp", "nas.home.lan"),
        site(24, "backups", None, "sftp", "backup.example.net")
    ]);
    let user = serde_json::json!({"id": id(99), "email": "ana@acme.com", "role": "user", "locale": "en", "timezone": "America/Santiago",
        "theme": "transferly-dark", "fileDoubleClick": "download"});
    app.handle(Event::Replied(Reply::Me(Ok(parse(user)))));
    app.handle(Event::Replied(Reply::Version(Ok(transferly_tui::ui::home::VERSION.to_owned()))));
    app.handle(Event::Replied(Reply::Workspaces(Ok(parse(workspaces)))));
    app.handle(Event::Replied(Reply::Vault(Ok(parse(serde_json::json!({"initialized": true, "unlocked": true}))))));
    let workspace = uuid(id(1));
    app.handle(Event::Replied(Reply::Tree { workspace, outcome: Ok((parse(collections), parse(sites))) }));
}

/// An identifier from its text.
fn uuid(text: String) -> uuid::Uuid {
    text.parse().expect("an identifier")
}

/// The short link the server answers for the request the interface asked about.
fn shortened(effects: &[Effect], code: &str) -> Event {
    let [Effect::Shorten { state, redirect, .. }] = effects else {
        panic!("the interface asks for the address of the approval page: {effects:?}")
    };
    Event::Shortened { state: state.clone(), redirect: redirect.clone(), url: format!("https://files.acme.com/cli/a/{code}") }
}

/// Draws the interface as it stands.
fn shot(slug: &'static str, about: &'static str, app: &App) -> Shot {
    shot_at(slug, about, app, 100, 28)
}

/// Draws the interface at the size of the corresponding design.
fn shot_at(slug: &'static str, about: &'static str, app: &App, width: u16, height: u16) -> Shot {
    let mut terminal = Terminal::new(TestBackend::new(width, height)).expect("backend");
    terminal.draw(|frame| app.render(frame)).expect("draw");
    let palette = app.palette();
    let defaults = html::Defaults { fg: palette.text, bg: palette.bg, muted: if palette.muted == Color::Reset { Color::Reset } else { palette.muted } };
    Shot { slug, about, buffer: terminal.backend().buffer().clone(), defaults }
}

/// The steps of signing in: the server, the approval in the browser, the code pasted without a browser, and signed in.
pub fn shots() -> Vec<Shot> {
    let mut shots = Vec::new();
    let mut english = app(Language::English, true);
    english.handle(key(KeyCode::Down));
    shots.push(shot("app-server", "The first screen: the address of the server, with the profiles used before; ↓ chose one.", &english));

    english.handle(key(KeyCode::Enter));
    english.handle(Event::Probed(Ok(())));
    let effects = english.handle(Event::Listening(Ok("http://127.0.0.1:49152/callback".to_owned())));
    english.handle(shortened(&effects, "K7P2-9QXM"));
    english.handle(Event::BrowserOpened(true));
    for _ in 0..3 {
        english.handle(Event::Tick);
    }
    shots.push(shot("app-approval", "The browser opened the approval page; the terminal waits on 127.0.0.1.", &english));

    let mut spanish = app(Language::Spanish, false);
    spanish.handle(key(KeyCode::Enter));
    let effects = spanish.handle(Event::Probed(Ok(())));
    spanish.handle(shortened(&effects, "W4HN-3RTD"));
    spanish.handle(Event::Paste("rsA7h63aXQetjzVD7pKsfA1yzQggA868KGK49ScbGr8".to_owned()));
    shots.push(shot("app-paste", "Over SSH, without a browser: the code shown by the approval page is pasted, in Spanish.", &spanish));

    english.handle(Event::Answered(Ok(Answer::Approved("code".to_owned()))));
    english.handle(Event::SignedIn {
        server: "https://files.acme.com".to_owned(),
        email: "ana@acme.com".to_owned(),
        locale: "en".to_owned(),
        theme: "transferly-dark".to_owned(),
        token: "t".to_owned(),
    });
    load(&mut english);
    for code in [KeyCode::Down, KeyCode::Down, KeyCode::Enter] {
        english.handle(key(code));
    }
    shots.push(shot("app-main", "The main screen: the sites of the workspace as a tree, the site opened on the right, and session status at the top.", &english));

    for (side, path) in [(Side::Left, "/home/ana"), (Side::Right, "/var/www")] {
        english.handle(Event::Replied(Reply::Homed {
            side,
            outcome: Ok(path.to_owned()),
        }));
        english.handle(Event::Replied(Reply::Listed {
            side,
            path: path.to_owned(),
            outcome: Ok(Vec::new()),
        }));
    }
    english.handle(key(KeyCode::Char('s')));
    english.handle(key(KeyCode::Char('s')));
    english.handle(key(KeyCode::Char('S')));
    shots.push(shot_at(
        "app-bars",
        "Settings on the explorer borders, shortcuts at the bottom, and session status at the top.",
        &english,
        132,
        34,
    ));

    let mut with_menu = App::new(Setup {
        language: Language::English,
        mode: ColorMode::TrueColor,
        theme: "transferly-dark".to_owned(),
        settings: Settings::default(),
        profiles: Vec::new(),
        host: "laptop".to_owned(),
        browser_possible: true,
        signed_in: Some(Kept {
            server: "https://files.acme.com".to_owned(),
            email: "ana@acme.com".to_owned(),
            token: "t".to_owned(),
        }),
    });
    load(&mut with_menu);
    for code in [KeyCode::Down, KeyCode::Down, KeyCode::Enter] {
        with_menu.handle(key(code));
    }
    let remote_path = "/var/www";
    with_menu.handle(Event::Replied(Reply::Homed {
        side: Side::Right,
        outcome: Ok(remote_path.to_owned()),
    }));
    with_menu.handle(Event::Replied(Reply::Listed {
        side: Side::Right,
        path: remote_path.to_owned(),
        outcome: Ok(vec![Entry {
            name: "release.tar.gz".to_owned(),
            kind: Kind::File,
            size: 50_331_648,
            modified: None,
        }]),
    }));
    with_menu.handle(key(KeyCode::End));
    with_menu.handle(key(KeyCode::F(9)));
    with_menu.handle(key(KeyCode::Down));
    shots.push(shot_at(
        "app-context-menu",
        "The explorer context menu beside an entry, with each action and its shortcut.",
        &with_menu,
        132,
        34,
    ));

    let mut site_manager = app(Language::English, true);
    site_manager.handle(Event::SignedIn {
        server: "https://files.acme.com".to_owned(),
        email: "ana@acme.com".to_owned(),
        locale: "en".to_owned(),
        theme: "transferly-dark".to_owned(),
        token: "t".to_owned(),
    });
    load(&mut site_manager);
    site_manager.handle(key(KeyCode::Char('S')));
    shots.push(shot_at(
        "app-sites",
        "The site table with collection paths and search.",
        &site_manager,
        132,
        34,
    ));
    site_manager.handle(key(KeyCode::Char('N')));
    let catalog = serde_json::from_str(include_str!("../../../frontend/src/test/connectors.json"))
        .expect("connector catalog");
    site_manager.handle(Event::Replied(Reply::Connectors(Ok(catalog))));
    shots.push(shot_at(
        "app-new-site",
        "The new site form drawn from the connector schema.",
        &site_manager,
        132,
        40,
    ));

    english.handle(key(KeyCode::Char('/')));
    shots.push(shot("app-commands", "/ opens the command line with the commands that match, as in Claude Code.", &english));
    english.handle(Event::Paste("config".to_owned()));
    english.handle(key(KeyCode::Enter));
    shots.push(shot("app-config", "/config: the settings of the account and of this computer, changed in place with ← →.", &english));
    english.handle(key(KeyCode::Down));
    english.handle(key(KeyCode::Enter));
    for _ in 0..4 {
        english.handle(key(KeyCode::Down));
    }
    shots.push(shot("app-theme", "Choosing a theme wears it while moving: here Nord; Enter keeps it in the account.", &english));
    english.handle(key(KeyCode::Esc));

    let locked = serde_json::json!({"initialized": true, "unlocked": false});
    english.handle(Event::Replied(Reply::Vault(Ok(parse(locked)))));
    english.handle(key(KeyCode::BackTab));
    english.handle(key(KeyCode::Down));
    english.handle(key(KeyCode::Enter));
    for _ in 0..6 {
        english.handle(key(KeyCode::Char('x')));
    }
    shots.push(shot("app-vault", "A site whose credentials are in the locked vault asks for the master password first.", &english));
    english.handle(key(KeyCode::Esc));
    english.handle(key(KeyCode::Char('?')));
    shots.push(shot("app-help", "? shows every key of the screen.", &english));
    english.handle(key(KeyCode::Esc));
    let row = |n: u8, direction: &str, name: &str, status: &str, done: u64, total: u64| {
        serde_json::json!({"id": format!("00000000-0000-0000-0000-0000000000{n:02}"), "direction": direction, "name": name, "siteName": "acme-prod", "status": status, "bytesDone": done, "totalBytes": total, "errorCode": if status == "failed" { Some("permission_denied") } else { None }})
    };
    let rows = vec![row(1, "upload", "release-2026-11.tar.gz", "running", 0, 734_003_200), row(2, "download", "backup-db.sql.gz", "running", 0, 52_428_800), row(3, "copy", "reports/", "queued", 0, 0), row(4, "upload", "notes.txt", "done", 2048, 2048), row(5, "download", "secret.key", "failed", 0, 512)];
    english.handle(Event::Replied(Reply::Snapshot(parse(serde_json::Value::Array(rows)))));
    let start = std::time::Instant::now();
    for (n, done, seconds) in [(1, 100_000_000_u64, 0), (1, 285_000_000, 10), (2, 10_000_000, 0), (2, 39_000_000, 10)] {
        let id = uuid(format!("00000000-0000-0000-0000-0000000000{n:02}"));
        english.handle(Event::Replied(Reply::Progress { id, done, total: None, at: start + std::time::Duration::from_secs(seconds) }));
    }
    english.handle(Event::Key(KeyEvent::new(KeyCode::Char('e'), KeyModifiers::CONTROL)));
    shots.push(shot("app-transfers", "Ctrl+E shows the queue: each transfer with its gauge, share, speed and time left; failures in red with their code.", &english));
    shots
}

/// A model of the API from its JSON.
fn parse<T: serde::de::DeserializeOwned>(value: serde_json::Value) -> T {
    serde_json::from_value(value).expect("a model")
}
