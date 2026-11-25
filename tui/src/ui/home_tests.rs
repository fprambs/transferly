//! Tests of the main screen: loading a session, the sites tree, the vault, the command line, the settings, the lists, the help and how
//! it is drawn, then the same through the interface and the loop against a mock server.

#![allow(
    clippy::too_many_lines,
    reason = "each test walks one part of the screen through its steps"
)]

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::Terminal;
use ratatui::backend::TestBackend;
use transferly_client::models::{Collection, Site, User, VaultStatus, Workspace};
use url::Url;
use uuid::Uuid;

use super::DEFAULT_THEME;
use super::explorer::Op;
use super::home::{
    Ask, Call, CallError, Choice, Focus, Home, Overlay, Reply, Session, Side, VERSION,
};
use super::look::Look;
use super::theme::ColorMode;
use crate::config::{Colors, EnterOnFile, Settings};
use crate::i18n::Language;

/// Asserts that nothing was asked or done.
fn nothing<T: PartialEq + std::fmt::Debug>(items: &[T], why: &str) {
    assert!(items.is_empty(), "{items:?}: {why}");
}

/// A pressed key.
fn key(code: KeyCode) -> KeyEvent {
    KeyEvent::new(code, KeyModifiers::NONE)
}

/// A key pressed with Ctrl.
fn ctrl(c: char) -> KeyEvent {
    KeyEvent::new(KeyCode::Char(c), KeyModifiers::CONTROL)
}

/// The look of an English terminal in truecolor with the default theme.
fn look() -> Look {
    Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::TrueColor,
        Settings::default(),
    )
}

/// The session of files.example.com.
fn session() -> Session {
    Session {
        server: Url::parse("https://files.example.com").expect("server"),
        token: "t".to_owned(),
    }
}

/// An identifier that reads as a number.
fn id(n: u8) -> Uuid {
    Uuid::from_u128(u128::from(n))
}

/// A workspace.
fn workspace(n: u8, name: &str, kind: &str) -> Workspace {
    serde_json::from_value(serde_json::json!({"id": id(n), "name": name, "kind": kind, "role": "owner", "createdAt": "2026-01-01T00:00:00Z"}))
        .expect("workspace")
}

/// A collection, inside another or at the top.
fn collection(n: u8, name: &str, parent: Option<u8>) -> Collection {
    Collection {
        id: id(n),
        name: name.to_owned(),
        parent_id: parent.map(id),
    }
}

/// A site in a collection, or outside any, with credentials in the vault or not.
fn site(n: u8, name: &str, collection: Option<u8>, secrets: bool) -> Site {
    serde_json::from_value(serde_json::json!({
        "id": id(n), "collectionId": collection.map(id), "name": name, "connector": "sftp", "config": {"host": format!("{name}.example.com")},
        "speedLimitBps": 0, "secrets": if secrets { vec!["password"] } else { vec![] }, "createdAt": "2026-01-01T00:00:00Z",
        "updatedAt": "2026-01-01T00:00:00Z"
    }))
    .expect("site")
}

/// The account, speaking a language and wearing a theme.
fn user(locale: &str, theme: &str) -> User {
    serde_json::from_value(serde_json::json!({"id": id(99), "email": "ana@example.com", "role": "user", "locale": locale,
        "timezone": "America/Santiago", "theme": theme, "fileDoubleClick": "download"}))
    .expect("user")
}

/// A refusal with a code.
fn refused(code: &str) -> CallError {
    CallError {
        code: Some(code.to_owned()),
        message: format!("refused: {code}"),
    }
}

/// A main screen loaded with two workspaces, the tree of the personal one and a vault in a state.
///
/// The tree: Clients (with Acme inside, with acme-sftp inside it, and beta-ftp), then home-nas outside any collection.
fn loaded(look: &mut Look, vault: Option<(bool, bool)>) -> Home {
    let (mut home, _) = Home::new(session(), "ana@example.com", false);
    home.reply(Reply::Me(Ok(user("en", DEFAULT_THEME))), look);
    home.reply(
        Reply::Workspaces(Ok(vec![
            workspace(2, "Team", "team"),
            workspace(1, "Personal", "personal"),
        ])),
        look,
    );
    home.reply(
        Reply::Tree {
            workspace: id(1),
            outcome: Ok((
                vec![
                    collection(10, "Clients", None),
                    collection(11, "Acme", Some(10)),
                ],
                vec![
                    site(20, "acme-sftp", Some(11), true),
                    site(21, "beta-ftp", Some(10), false),
                    site(22, "home-nas", None, false),
                ],
            )),
        },
        look,
    );
    if let Some((initialized, unlocked)) = vault {
        home.reply(
            Reply::Vault(Ok(VaultStatus {
                initialized,
                unlocked,
            })),
            look,
        );
    }
    home
}

/// Draws the main screen and returns its text.
fn draw(home: &Home, look: &Look, width: u16, height: u16) -> String {
    let mut terminal = Terminal::new(TestBackend::new(width, height)).expect("backend");
    terminal
        .draw(|frame| home.render(frame, look, "*"))
        .expect("draw");
    let buffer = terminal.backend().buffer();
    (0..height)
        .map(|y| {
            (0..width)
                .map(|x| buffer[(x, y)].symbol().to_owned())
                .collect::<String>()
                + "\n"
        })
        .collect()
}

/// Types text, one key per character.
fn type_text(home: &mut Home, look: &mut Look, text: &str) {
    for c in text.chars() {
        home.key(key(KeyCode::Char(c)), look);
    }
}

/// Runs a command line.
fn command(home: &mut Home, look: &mut Look, line: &str) -> Vec<Ask> {
    home.key(key(KeyCode::Char('/')), look);
    type_text(home, look, line);
    home.key(key(KeyCode::Enter), look)
}

/// A session loads the account, the version, the workspaces and the vault; the personal workspace opens; the account brings its
/// language and theme; a different minor version of the server is pointed out.
#[test]
fn loads_a_session() {
    let mut look = look();
    let (mut home, asks) = Home::new(session(), "ana@example.com", false);
    assert_eq!(
        asks,
        [Call::Me, Call::Version, Call::Workspaces, Call::Vault]
            .map(Ask::Call)
            .into_iter()
            .chain([Ask::Local {
                side: Side::Left,
                op: Op::Home
            }])
            .collect::<Vec<_>>()
    );
    assert_eq!(home.session(), &session());
    assert!(draw(&home, &look, 120, 30).contains("* Loading…"));

    home.reply(Reply::Me(Ok(user("es", "nord"))), &mut look);
    assert_eq!(
        (look.messages.language(), look.theme.as_str()),
        (Language::Spanish, "nord")
    );
    nothing(
        &home.reply(Reply::Version(Ok("dev".to_owned())), &mut look),
        "",
    );
    assert!(!draw(&home, &look, 120, 30).contains("dev,"));
    home.reply(Reply::Version(Ok("v99.1.0".to_owned())), &mut look);
    assert!(draw(&home, &look, 160, 30).contains(&format!("servidor v99.1.0, cliente {VERSION}")));

    let asks = home.reply(
        Reply::Workspaces(Ok(vec![
            workspace(2, "Team", "team"),
            workspace(1, "Personal", "personal"),
        ])),
        &mut look,
    );
    assert_eq!(asks, vec![Ask::Call(Call::Tree(id(1)))]);
    let stale = Reply::Tree {
        workspace: id(2),
        outcome: Ok((vec![], vec![site(20, "x", None, false)])),
    };
    nothing(&home.reply(stale, &mut look), "");
    home.reply(
        Reply::Tree {
            workspace: id(1),
            outcome: Ok((vec![], vec![])),
        },
        &mut look,
    );
    assert_eq!(home.workspace().map(|w| w.name.as_str()), Some("Personal"));
    assert!(draw(&home, &look, 120, 30).contains("Este workspace"));

    let kept = home.reply(
        Reply::Workspaces(Ok(vec![workspace(1, "Personal", "personal")])),
        &mut look,
    );
    assert_eq!(kept, vec![Ask::Call(Call::Tree(id(1)))]);
    let only_team = home.reply(
        Reply::Workspaces(Ok(vec![workspace(3, "Other", "team")])),
        &mut look,
    );
    assert_eq!(only_team, vec![Ask::Call(Call::Tree(id(3)))]);
    nothing(&home.reply(Reply::Workspaces(Ok(vec![])), &mut look), "");
    assert!(home.workspace().is_none());
}

/// Failed calls: an ended session goes back to signing in, an old server says so, a locked vault asks for its password, anything else is
/// said; a failed tree stops loading; a failed change of preferences goes back to what the account wears.
#[test]
fn explains_failed_calls() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    assert_eq!(
        home.reply(Reply::Me(Err(refused("unauthenticated"))), &mut look),
        vec![Ask::Leave(Some(
            "The session ended. Sign in again.".to_owned()
        ))]
    );
    home.reply(Reply::Version(Err(refused("http_404"))), &mut look);
    assert_eq!(
        home.notice().map(|n| n.text.as_str()),
        Some("This server is too old for that: update it.")
    );
    home.reply(Reply::Workspaces(Err(refused("internal"))), &mut look);
    assert_eq!(
        home.notice().map(|n| (n.text.as_str(), n.error)),
        Some(("refused: internal", true))
    );
    home.reply(Reply::Vault(Err(refused("vault_locked"))), &mut look);
    assert!(matches!(
        home.overlay(),
        Some(Overlay::Vault { site: None, .. })
    ));
    assert!(draw(&home, &look, 120, 30).contains("Your vault is locked."));
    home.reply(
        Reply::Tree {
            workspace: id(1),
            outcome: Err(refused("forbidden")),
        },
        &mut look,
    );
    assert!(!draw(&home, &look, 120, 30).contains("* Loading"));

    look.wear("nord");
    look.speak("es");
    home.reply(Reply::Preferences(Err(refused("invalid"))), &mut look);
    assert_eq!(
        (look.theme.as_str(), look.messages.language()),
        (DEFAULT_THEME, Language::English)
    );
    home.reply(Reply::Preferences(Ok(user("es", "dracula"))), &mut look);
    assert_eq!(look.theme, "dracula");
}

/// The tree moves with the arrows and vim keys, opens and closes collections, and opens a site in the explorer with a dot on it.
#[test]
fn browses_the_sites() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    let text = draw(&home, &look, 120, 30);
    for row in [
        "▾ Clients",
        "  ▾ Acme",
        "    · acme-sftp",
        "  · beta-ftp",
        "· home-nas",
        "Personal",
        "vault unlocked",
    ] {
        assert!(text.contains(row), "{row} in\n{text}");
    }
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(home.opened().map(|s| s.name.as_str()), Some("home-nas"));
    assert_eq!(home.focus(), Focus::Right);
    let text = draw(&home, &look, 120, 30);
    assert!(text.contains("● home-nas") && text.contains(" 1 home-nas "));

    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Char('g')), &mut look);
    home.key(key(KeyCode::Char('j')), &mut look);
    home.key(key(KeyCode::Left), &mut look);
    assert!(!draw(&home, &look, 120, 30).contains("acme-sftp"));
    home.key(key(KeyCode::Char('l')), &mut look);
    assert!(draw(&home, &look, 120, 30).contains("acme-sftp"));
    home.key(key(KeyCode::Enter), &mut look);
    assert!(!draw(&home, &look, 120, 30).contains("acme-sftp"));
    home.key(key(KeyCode::Enter), &mut look);
    home.key(key(KeyCode::Char('h')), &mut look);
    home.key(key(KeyCode::Right), &mut look);
    home.key(key(KeyCode::Char('k')), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::Home), &mut look);
    home.key(key(KeyCode::Char('j')), &mut look);
    home.key(key(KeyCode::Char('j')), &mut look);
    home.key(key(KeyCode::Right), &mut look);
    home.key(key(KeyCode::Char('x')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(home.opened().map(|s| s.name.as_str()), Some("acme-sftp"));

    home.key(key(KeyCode::BackTab), &mut look);
    assert_eq!(home.focus(), Focus::Left);
    home.key(key(KeyCode::BackTab), &mut look);
    assert_eq!(home.focus(), Focus::Sites);
    home.key(ctrl('b'), &mut look);
    assert_eq!(home.focus(), Focus::Left, "the last panel of the explorer");
    nothing(&home.key(key(KeyCode::Tab), &mut look), "");
    assert_eq!(home.focus(), Focus::Right);
    home.key(key(KeyCode::BackTab), &mut look);
    home.key(key(KeyCode::BackTab), &mut look);
    assert_eq!(
        home.focus(),
        Focus::Right,
        "without the sites, the two panels"
    );
    nothing(&home.key(key(KeyCode::Char('j')), &mut look), "");
    home.key(ctrl('b'), &mut look);
    assert_eq!(home.focus(), Focus::Sites);
    home.key(key(KeyCode::Char('w')), &mut look);
    assert!(matches!(
        home.overlay(),
        Some(Overlay::Choose {
            what: Choice::Workspace,
            ..
        })
    ));
}

/// The screen fits the window: a small one asks for more room, a narrow one shows the focused panel, a long tree scrolls, and without
/// colors the active tab is reversed.
#[test]
fn fits_the_window() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((false, false)));
    assert!(draw(&home, &look, 50, 20).contains("Make the window"));
    let narrow = draw(&home, &look, 80, 20);
    assert!(narrow.contains("Clients") && narrow.contains("no vault"));
    home.key(key(KeyCode::Tab), &mut look);
    let narrow = draw(&home, &look, 80, 20);
    assert!(!narrow.contains("Clients") && narrow.contains("Local disk"));
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Char('G')), &mut look);
    let short = draw(&home, &look, 100, 16);
    assert!(short.contains("home-nas"));

    let mut plain = Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::Plain,
        Settings::default(),
    );
    let home = loaded(&mut plain, Some((true, false)));
    assert!(draw(&home, &plain, 120, 30).contains("vault locked"));
}

/// A site whose credentials are in a locked vault asks for the master password first, then opens; a wrong password, too many attempts
/// and other failures are explained; Esc gives up.
#[test]
fn unlocks_the_vault() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, false)));
    nothing(
        &home.reply(Reply::Unlocked(Ok(())), &mut look),
        "an answer nobody waits for",
    );
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert!(matches!(
        home.overlay(),
        Some(Overlay::Vault { site: Some(_), .. })
    ));
    nothing(&home.key(key(KeyCode::Enter), &mut look), "nothing typed");
    type_text(&mut home, &mut look, "sec");
    home.paste("ret");
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("The credentials of acme-sftp")
            && text.contains("••••••")
            && !text.contains("secret")
    );
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![Ask::Call(Call::Unlock("secret".to_owned()))]
    );
    assert!(draw(&home, &look, 120, 30).contains("* Unlocking…"));
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    assert!(home.overlay().is_some(), "keys wait while unlocking");

    for (code, message) in [
        (
            "invalid_master_password",
            "That master password is not right.",
        ),
        ("rate_limited", "Too many attempts."),
        ("other", "refused: other"),
    ] {
        home.reply(Reply::Unlocked(Err(refused(code))), &mut look);
        assert!(draw(&home, &look, 120, 30).contains(message), "{code}");
        home.key(key(KeyCode::Enter), &mut look);
    }
    home.reply(Reply::Unlocked(Ok(())), &mut look);
    assert_eq!(home.opened().map(|s| s.name.as_str()), Some("acme-sftp"));
    assert_eq!(
        home.notice().map(|n| n.text.as_str()),
        Some("The vault is unlocked.")
    );

    let mut home = loaded(&mut look, Some((true, false)));
    home.reply(Reply::Vault(Err(refused("vault_locked"))), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    assert!(home.overlay().is_none());
    home.reply(Reply::Vault(Err(refused("vault_locked"))), &mut look);
    home.key(key(KeyCode::Char('x')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.reply(Reply::Unlocked(Ok(())), &mut look);
    assert!(home.opened().is_none());
}

/// `/` lists the commands that match, completes with Tab, runs with Enter, and takes the value of a command right away.
#[test]
fn runs_commands() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    home.key(key(KeyCode::Char('/')), &mut look);
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("/config")
            && text.contains("/quit")
            && text.contains("sign this terminal out")
    );
    home.paste("th");
    home.key(key(KeyCode::Tab), &mut look);
    type_text(&mut home, &mut look, "Nord");
    assert!(draw(&home, &look, 120, 30).contains("/theme Nord"));
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![Ask::Call(Call::Preferences {
            locale: None,
            theme: Some("nord".to_owned())
        })]
    );
    assert_eq!(look.theme, "nord");

    for (line, asks) in [
        (
            "theme dracula",
            vec![Ask::Call(Call::Preferences {
                locale: None,
                theme: Some("dracula".to_owned()),
            })],
        ),
        (
            "language es",
            vec![Ask::Call(Call::Preferences {
                locale: Some("es".to_owned()),
                theme: None,
            })],
        ),
        (
            "language english",
            vec![Ask::Call(Call::Preferences {
                locale: Some("en".to_owned()),
                theme: None,
            })],
        ),
        ("workspace team", vec![Ask::Call(Call::Tree(id(2)))]),
        ("server", vec![Ask::Leave(None)]),
        (
            "web",
            vec![Ask::Browse("https://files.example.com/".to_owned())],
        ),
        ("logout", vec![Ask::Logout]),
        ("quit", vec![Ask::Quit]),
        ("q", vec![Ask::Quit]),
    ] {
        assert_eq!(command(&mut home, &mut look, line), asks, "{line}");
        if line == "logout" {
            assert_eq!(home.notice().map(|n| n.text.as_str()), Some("Signing out…"));
        }
    }

    for (line, notice) in [
        ("theme bogus", "Nothing is called bogus."),
        ("language xx", "Nothing is called xx."),
        ("workspace nope", "Nothing is called nope."),
        ("zzz", "There is no command /zzz."),
    ] {
        nothing(&command(&mut home, &mut look, line), "");
        assert_eq!(
            home.notice().map(|n| (n.text.as_str(), n.error)),
            Some((notice, true)),
            "{line}"
        );
    }

    for (line, overlay) in [
        ("config", "Settings"),
        ("keys", "Help"),
        ("theme", "Theme"),
        ("language", "Language"),
        ("workspace", "Workspace"),
    ] {
        command(&mut home, &mut look, line);
        let shown = match home.overlay() {
            Some(Overlay::Settings { .. }) => "Settings",
            Some(Overlay::Help) => "Help",
            Some(Overlay::Choose {
                what: Choice::Theme,
                ..
            }) => "Theme",
            Some(Overlay::Choose {
                what: Choice::Language,
                ..
            }) => "Language",
            Some(Overlay::Choose {
                what: Choice::Workspace,
                ..
            }) => "Workspace",
            _ => "none",
        };
        assert_eq!(shown, overlay, "{line}");
        home.key(key(KeyCode::Esc), &mut look);
    }

    home.key(key(KeyCode::Char('/')), &mut look);
    home.key(key(KeyCode::F(5)), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Char('/')), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert!(
        matches!(
            home.overlay(),
            Some(Overlay::Choose {
                what: Choice::Theme,
                ..
            })
        ),
        "the chosen match runs"
    );
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Char('/')), &mut look);
    type_text(&mut home, &mut look, "zz");
    assert!(!draw(&home, &look, 120, 30).contains("/config"));
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    assert!(home.overlay().is_none());
    home.key(key(KeyCode::Char('/')), &mut look);
    home.key(key(KeyCode::Backspace), &mut look);
    assert!(home.overlay().is_none());
}

/// Lists preview themes while moving and go back on Esc, keep the choice on Enter, and change the language and the workspace.
#[test]
fn chooses_from_lists() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    command(&mut home, &mut look, "theme");
    for _ in 0..11 {
        home.key(key(KeyCode::Down), &mut look);
    }
    assert_ne!(look.theme, DEFAULT_THEME, "previewed");
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("✓ Transferly Dark") || !text.contains("Transferly Dark"),
        "scrolled to the last"
    );
    home.key(key(KeyCode::Esc), &mut look);
    assert_eq!(look.theme, DEFAULT_THEME, "back to what was worn");

    command(&mut home, &mut look, "theme");
    home.key(key(KeyCode::Char('j')), &mut look);
    home.key(key(KeyCode::Char('k')), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Char('x')), &mut look);
    let chosen = look.theme.clone();
    assert!(
        matches!(home.key(key(KeyCode::Enter), &mut look).as_slice(), [Ask::Call(Call::Preferences { theme: Some(theme), .. })] if *theme == chosen)
    );

    command(&mut home, &mut look, "language");
    assert!(draw(&home, &look, 120, 30).contains("✓ English"));
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(look.messages.language(), Language::Spanish);
    command(&mut home, &mut look, "language");
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(look.messages.language(), Language::English);

    command(&mut home, &mut look, "workspace");
    assert!(draw(&home, &look, 120, 30).contains("✓ Personal"));
    home.key(key(KeyCode::Up), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![Ask::Call(Call::Tree(id(2)))]
    );

    let (mut empty, _) = Home::new(session(), "ana@example.com", false);
    command(&mut empty, &mut look, "workspace");
    nothing(
        &empty.key(key(KeyCode::Enter), &mut look),
        "nothing to choose",
    );
}

/// `/config` lists every setting with its value and where it lives; ← → change it in place and Enter opens the list of the language
/// and the theme; the settings of this computer are kept and the colors apply at once.
#[test]
fn changes_settings() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    command(&mut home, &mut look, "config");
    let text = draw(&home, &look, 120, 30);
    for value in [
        "Language",
        "English",
        "Transferly Dark",
        "America/Santiago",
        "of the theme",
        "copy it",
        "hidden",
        "yes",
        "once a day",
        "this computer",
    ] {
        assert!(text.contains(value), "{value} in\n{text}");
    }
    home.key(key(KeyCode::Enter), &mut look);
    assert!(matches!(
        home.overlay(),
        Some(Overlay::Choose {
            what: Choice::Language,
            ..
        })
    ));
    home.key(key(KeyCode::Esc), &mut look);
    command(&mut home, &mut look, "config");
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert!(matches!(
        home.overlay(),
        Some(Overlay::Choose {
            what: Choice::Theme,
            ..
        })
    ));
    home.key(key(KeyCode::Esc), &mut look);

    command(&mut home, &mut look, "config");
    assert!(matches!(
        home.key(key(KeyCode::Right), &mut look).as_slice(),
        [Ask::Call(Call::Preferences {
            locale: Some(_),
            ..
        })]
    ));
    assert_eq!(look.messages.language(), Language::Spanish);
    assert!(draw(&home, &look, 120, 30).contains("Español"));
    home.key(key(KeyCode::Left), &mut look);
    assert_eq!(look.messages.language(), Language::English);
    home.key(key(KeyCode::Char('j')), &mut look);
    home.key(key(KeyCode::Right), &mut look);
    let forward = look.theme.clone();
    home.key(key(KeyCode::Char('h')), &mut look);
    home.key(key(KeyCode::Char('h')), &mut look);
    assert_ne!(look.theme, forward);
    home.key(key(KeyCode::Down), &mut look);
    nothing(&home.key(key(KeyCode::Right), &mut look), "");
    assert_eq!(
        home.notice().map(|n| n.text.as_str()),
        Some("The time zone is changed in the web, in Settings.")
    );

    for _ in 3..8 {
        home.key(key(KeyCode::Down), &mut look);
        assert_eq!(
            home.key(key(KeyCode::Char(' ')), &mut look),
            vec![Ask::Save]
        );
    }
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Char('k')), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Char('x')), &mut look);
    assert_eq!(
        look.settings,
        Settings {
            colors: Colors::Terminal,
            enter_on_file: EnterOnFile::Details,
            hidden_files: true,
            confirm_quit: false,
            check_updates: false
        }
    );
    assert_eq!(look.mode(), ColorMode::Terminal);
    let text = draw(&home, &look, 120, 30);
    for value in ["of the terminal", "show details", "shown", "no", "never"] {
        assert!(text.contains(value), "{value} in\n{text}");
    }
    for _ in 0..5 {
        home.key(key(KeyCode::Up), &mut look);
    }
    for _ in 0..3 {
        home.key(key(KeyCode::Down), &mut look);
    }
    for _ in 3..8 {
        home.key(key(KeyCode::Enter), &mut look);
        home.key(key(KeyCode::Down), &mut look);
    }
    assert_eq!(look.settings, Settings::default());
    home.key(key(KeyCode::Esc), &mut look);
    assert!(home.overlay().is_none());
}

/// `?` and F1 show every key and any key closes them; q and F10 quit.
#[test]
fn shows_help_and_quits() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    for open in [KeyCode::Char('?'), KeyCode::F(1)] {
        home.key(key(open), &mut look);
        let text = draw(&home, &look, 120, 30);
        assert!(
            text.contains("Everywhere")
                && text.contains("Ctrl+B")
                && text.contains("open the site")
        );
        nothing(&home.key(key(KeyCode::Char('q')), &mut look), "");
        assert!(home.overlay().is_none());
    }
    assert_eq!(
        home.key(key(KeyCode::Char('q')), &mut look),
        vec![Ask::Quit]
    );
    assert_eq!(home.key(key(KeyCode::F(10)), &mut look), vec![Ask::Quit]);
    home.say("kept".to_owned(), false);
    assert!(draw(&home, &look, 120, 30).contains("kept"));
    home.paste("nowhere");
}

/// The two panels of the explorer: a site opens on the right in its home folder, keys reach the focused panel, its operations go to
/// the server with the workspace and to the local disk, replies reach the panel that asked, and `o` chooses what a panel shows.
#[test]
fn works_with_the_explorer() {
    use super::explorer::Entry;
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('G')), &mut look);
    let opened = home.key(key(KeyCode::Enter), &mut look);
    let files = |op: Op| {
        Ask::Call(Call::Files {
            workspace: id(1),
            site: id(22),
            side: Side::Right,
            op,
        })
    };
    assert_eq!(opened, vec![files(Op::Home)]);
    assert_eq!(home.focus(), Focus::Right);
    let listed = home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Ok("/srv".to_owned()),
        },
        &mut look,
    );
    assert_eq!(listed, vec![files(Op::List("/srv".to_owned()))]);
    let entries = vec![Entry {
        name: "a.txt".to_owned(),
        kind: super::explorer::Kind::File,
        size: 1,
        modified: None,
    }];
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/srv".to_owned(),
            outcome: Ok(entries),
        },
        &mut look,
    );
    assert_eq!(home.panel(Side::Right).path(), "/srv");
    let text = draw(&home, &look, 140, 30);
    assert!(
        text.contains("● home-nas") && text.contains("a.txt") && text.contains("/srv"),
        "{text}"
    );
    home.key(key(KeyCode::Char('n')), &mut look);
    nothing(
        &home.key(key(KeyCode::Char('q')), &mut look),
        "the form takes the keys",
    );
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![files(Op::Mkdir("/srv/q".to_owned()))]
    );
    assert_eq!(
        home.reply(
            Reply::Changed {
                side: Side::Right,
                outcome: Ok(())
            },
            &mut look
        ),
        vec![files(Op::List("/srv".to_owned()))]
    );
    let missing = CallError {
        code: Some("not_found".to_owned()),
        message: "gone".to_owned(),
    };
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/srv".to_owned(),
            outcome: Err(missing),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 140, 30).contains("It does not exist any more."));
    let odd = CallError {
        code: Some("odd_code".to_owned()),
        message: "something odd".to_owned(),
    };
    home.key(key(KeyCode::Char('r')), &mut look);
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/srv".to_owned(),
            outcome: Err(odd),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 140, 30).contains("something odd"));
    home.reply(
        Reply::Changed {
            side: Side::Right,
            outcome: Err(refused("permission_denied")),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 140, 30).contains("does not let this account"));
    let ended = home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Err(refused("unauthenticated")),
        },
        &mut look,
    );
    assert_eq!(
        ended,
        vec![Ask::Leave(Some(
            "The session ended. Sign in again.".to_owned()
        ))]
    );

    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/srv".to_owned(),
            outcome: Err(refused("vault_locked")),
        },
        &mut look,
    );
    assert!(
        matches!(home.overlay(), Some(Overlay::Vault { site: Some(site), .. }) if *site == id(22))
    );
    home.key(key(KeyCode::Esc), &mut look);

    let left = home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/home/ana".to_owned()),
        },
        &mut look,
    );
    assert_eq!(
        left,
        vec![Ask::Local {
            side: Side::Left,
            op: Op::List("/home/ana".to_owned())
        }]
    );
    let disk = CallError {
        code: None,
        message: "permission denied (os error 13)".to_owned(),
    };
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home/ana".to_owned(),
            outcome: Err(disk),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 140, 30).contains("os error 13"));

    assert_eq!(home.key(ctrl('d'), &mut look), vec![files(Op::Disconnect)]);
    assert!(home.panel(Side::Right).source().is_none());
    home.key(key(KeyCode::Char('o')), &mut look);
    let text = draw(&home, &look, 140, 30);
    assert!(
        text.contains("Show in this panel")
            && text.contains("✓ Local disk")
            && text.contains("acme-sftp"),
        "{text}"
    );
    home.key(key(KeyCode::Down), &mut look);
    nothing(
        &home.key(key(KeyCode::Enter), &mut look),
        "a site with credentials in a locked vault asks first",
    );
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Char('o')), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![Ask::Local {
            side: Side::Right,
            op: Op::Home
        }]
    );
    nothing(
        &home.key(key(KeyCode::Char('j')), &mut look),
        "a panel listing its home folder",
    );
    home.key(key(KeyCode::BackTab), &mut look);
    assert_eq!(home.focus(), Focus::Left);
    home.key(key(KeyCode::Char('o')), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![Ask::Call(Call::Files {
            workspace: id(1),
            site: id(22),
            side: Side::Left,
            op: Op::Home
        })]
    );
    assert!(
        draw(&home, &look, 80, 20).contains("home-nas"),
        "a narrow window shows the focused panel"
    );
    home.key(key(KeyCode::BackTab), &mut look);
    assert!(draw(&home, &look, 80, 20).contains("Clients"));
    command(&mut home, &mut look, "workspace team");
    assert!(
        home.panel(Side::Left).source().is_none(),
        "the sites of another workspace close"
    );
    assert_eq!(
        home.panel(Side::Right).source(),
        Some(&super::explorer::Source::Local)
    );
}

/// The layout follows the focus and the sites panel: hidden on a wide window it leaves the two panels, a narrow window shows the
/// focused panel; `o` on a site marks it; a site that left the workspace while the vault was asked for does not open.
#[test]
fn lays_out_the_explorer() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, false)));
    home.key(key(KeyCode::Tab), &mut look);
    home.key(ctrl('b'), &mut look);
    assert_eq!(
        home.focus(),
        Focus::Left,
        "hiding the sites keeps the panel with the focus"
    );
    assert!(!draw(&home, &look, 140, 30).contains("Clients"));
    home.key(key(KeyCode::Tab), &mut look);
    assert!(draw(&home, &look, 80, 20).contains("Empty panel"));
    home.key(ctrl('b'), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.reply(Reply::Unlocked(Ok(())), &mut look);
    home.reply(
        Reply::Vault(Ok(VaultStatus {
            initialized: true,
            unlocked: false,
        })),
        &mut look,
    );
    assert_eq!(home.focus(), Focus::Right);
    home.key(key(KeyCode::Char('o')), &mut look);
    assert!(draw(&home, &look, 140, 30).contains("✓ acme-sftp"));
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert!(matches!(
        home.overlay(),
        Some(Overlay::Vault { site: Some(_), .. })
    ));
    home.reply(
        Reply::Tree {
            workspace: id(1),
            outcome: Ok((vec![], vec![])),
        },
        &mut look,
    );
    nothing(
        &home.reply(Reply::Unlocked(Ok(())), &mut look),
        "the site is gone",
    );
}

/// The interface around the main screen: a kept session starts on it, its replies reach it, and what it asks becomes effects; signing
/// out, keeping the settings and opening the browser report back on it.
mod interface {
    use std::sync::{Arc, Mutex};
    use std::time::Duration;

    use wiremock::matchers::{body_partial_json, method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    use super::*;
    use crate::config::{Config, Profile};
    use crate::secrets::{self, TokenStore};
    use crate::ui::app::{App, Effect, Event, Kept, Screen, Setup};
    use crate::ui::runtime;
    use crate::ui::tests::{draw_on, paced, ports};

    /// A setup with the session of a server kept.
    fn kept(server: &str) -> Setup {
        Setup {
            language: Language::English,
            mode: ColorMode::TrueColor,
            theme: DEFAULT_THEME.to_owned(),
            settings: Settings::default(),
            profiles: vec![Profile {
                server: server.to_owned(),
                account: Some("ana@example.com".to_owned()),
            }],
            host: "laptop".to_owned(),
            browser_possible: true,
            signed_in: Some(Kept {
                server: server.to_owned(),
                email: "ana@example.com".to_owned(),
                token: "t".to_owned(),
            }),
        }
    }

    /// A key as an event.
    fn press(code: KeyCode) -> Event {
        Event::Key(key(code))
    }

    /// Types a command line and runs it.
    fn run(app: &mut App, line: &str) -> Vec<Effect> {
        app.handle(press(KeyCode::Char('/')));
        app.handle(Event::Paste(line.to_owned()));
        app.handle(press(KeyCode::Enter))
    }

    /// The text of the screen.
    fn text(app: &App) -> String {
        let mut terminal = Terminal::new(TestBackend::new(140, 30)).expect("backend");
        terminal.draw(|frame| app.render(frame)).expect("draw");
        let buffer = terminal.backend().buffer();
        (0..30)
            .map(|y| {
                (0..140)
                    .map(|x| buffer[(x, y)].symbol().to_owned())
                    .collect::<String>()
                    + "\n"
            })
            .collect()
    }

    /// The main screen of the interface.
    fn home(app: &App) -> &Home {
        match app.screen() {
            Screen::Main(home) => home,
            _ => panic!("not the main screen"),
        }
    }

    /// A kept session opens the main screen and loads it; its replies change it and its asks become effects.
    #[test]
    fn drives_the_main_screen() {
        let mut app = App::new(kept("https://files.example.com"));
        let session = session();
        assert_eq!(
            app.start(),
            [Call::Me, Call::Version, Call::Workspaces, Call::Vault]
                .map(|call| Effect::Call(session.clone(), call))
                .into_iter()
                .chain([Effect::Local {
                    side: Side::Left,
                    op: Op::Home
                }])
                .collect::<Vec<_>>()
        );
        nothing(&app.start(), "");
        app.handle(Event::Replied(Reply::Me(Ok(user("es", "nord")))));
        assert_eq!(
            (app.look().messages.language(), app.look().theme.as_str()),
            (Language::Spanish, "nord")
        );
        app.handle(Event::Replied(Reply::Me(Ok(user("en", DEFAULT_THEME)))));

        assert_eq!(
            run(&mut app, "web"),
            vec![Effect::OpenBrowser("https://files.example.com/".to_owned())]
        );
        app.handle(Event::BrowserOpened(true));
        assert!(home(&app).notice().is_none());
        app.handle(Event::BrowserOpened(false));
        assert_eq!(
            home(&app).notice().map(|n| n.text.as_str()),
            Some("The browser did not open.")
        );
        assert_eq!(
            run(&mut app, "theme nord"),
            vec![Effect::Call(
                session.clone(),
                Call::Preferences {
                    locale: None,
                    theme: Some("nord".to_owned())
                }
            )]
        );
        assert_eq!(run(&mut app, "quit"), vec![Effect::Quit]);

        run(&mut app, "config");
        for _ in 0..3 {
            app.handle(press(KeyCode::Down));
        }
        let saved = app.handle(press(KeyCode::Right));
        assert!(
            matches!(saved.as_slice(), [Effect::SaveSettings(settings)] if settings.colors == Colors::Terminal)
        );
        app.handle(press(KeyCode::Esc));
        app.handle(Event::Saved(Ok(())));
        assert!(home(&app).notice().is_none());
        app.handle(Event::Saved(Err("disk full".to_owned())));
        assert_eq!(
            home(&app).notice().map(|n| n.text.as_str()),
            Some("The settings could not be kept: disk full")
        );

        assert_eq!(
            run(&mut app, "logout"),
            vec![Effect::Logout(session.clone())]
        );
        app.handle(Event::LoggedOut(Err("offline".to_owned())));
        assert_eq!(
            home(&app).notice().map(|n| n.text.as_str()),
            Some("Could not sign out: offline")
        );
        app.handle(Event::LoggedOut(Ok(())));
        assert!(matches!(app.screen(), Screen::Server { .. }));
        assert!(text(&app).contains("Signed out of https://files.example.com."));
        for ignored in [
            Event::LoggedOut(Err("x".to_owned())),
            Event::Saved(Err("x".to_owned())),
            Event::Replied(Reply::Vault(Ok(VaultStatus::default()))),
            Event::BrowserOpened(false),
        ] {
            nothing(&app.handle(ignored), "");
        }
        assert!(matches!(app.screen(), Screen::Server { .. }));

        let mut app = App::new(kept("https://files.example.com"));
        app.handle(Event::Replied(Reply::Workspaces(Err(refused(
            "unauthenticated",
        )))));
        assert!(text(&app).contains("The session ended. Sign in again."));
        let mut app = App::new(kept("https://files.example.com"));
        nothing(&run(&mut app, "server"), "");
        assert!(text(&app).contains("https://files.example.com"));
        assert!(matches!(
            App::new(kept("not an address")).screen(),
            Screen::Server { .. }
        ));

        let mut signed_out = kept("https://files.example.com");
        signed_out.signed_in = None;
        let mut app = App::new(signed_out);
        app.handle(press(KeyCode::Enter));
        app.handle(Event::Probed(Ok(())));
        app.handle(Event::Listening(Ok(
            "http://127.0.0.1:5000/callback".to_owned()
        )));
        nothing(&app.handle(Event::Paste("ignored".to_owned())), "");
        assert!(matches!(app.screen(), Screen::Approval { .. }));
    }

    /// The loop against a mock server: a kept session loads, a setting of this computer is kept, the language goes to the account,
    /// and signing out ends the session, forgets the token and goes back to the address of the server.
    #[tokio::test]
    async fn runs_the_main_screen() {
        let server = MockServer::start().await;
        let workspace_id = id(1).to_string();
        let answers = [
            (
                "GET",
                "/api/v1/me".to_owned(),
                serde_json::json!({"user": user("en", DEFAULT_THEME), "csrfToken": "c"}),
            ),
            (
                "GET",
                "/api/v1/version".to_owned(),
                serde_json::json!({"version": VERSION}),
            ),
            (
                "GET",
                "/api/v1/workspaces".to_owned(),
                serde_json::json!([workspace(1, "Personal", "personal")]),
            ),
            (
                "GET",
                format!("/api/v1/workspaces/{workspace_id}/collections"),
                serde_json::json!([]),
            ),
            (
                "GET",
                format!("/api/v1/workspaces/{workspace_id}/sites"),
                serde_json::json!([site(20, "acme", None, false)]),
            ),
            (
                "GET",
                "/api/v1/vault".to_owned(),
                serde_json::json!({"initialized": true, "unlocked": true}),
            ),
        ];
        for (verb, route, body) in answers {
            Mock::given(method(verb))
                .and(path(route))
                .respond_with(ResponseTemplate::new(200).set_body_json(body))
                .mount(&server)
                .await;
        }
        Mock::given(method("PATCH"))
            .and(path("/api/v1/me"))
            .and(body_partial_json(serde_json::json!({"locale": "es"})))
            .respond_with(ResponseTemplate::new(200).set_body_json(user("es", DEFAULT_THEME)))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/auth/logout"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;

        let dir = tempfile::tempdir().expect("folder");
        let tokens: Arc<dyn TokenStore> = Arc::from(secrets::open(
            dir.path(),
            Ok(keyring_core::mock::Store::new().expect("mock")),
        ));
        tokens.set(&server.uri(), "t").expect("token");
        let ports = ports(
            dir.path(),
            tokens.clone(),
            Arc::new(|_: &str| true),
            Arc::new(Mutex::new(Vec::new())),
        );
        let mut config = Config::default();
        config.signed_in(&server.uri(), "ana@example.com");
        config.save(&ports.config_path).expect("config");

        let mut events = vec![
            (300, press(KeyCode::Char('/'))),
            (0, Event::Paste("config".to_owned())),
            (0, press(KeyCode::Enter)),
        ];
        events.extend((0..3).map(|_| (0, press(KeyCode::Down))));
        events.extend([(0, press(KeyCode::Right)), (0, press(KeyCode::Esc))]);
        events.extend([
            (200, press(KeyCode::Char('/'))),
            (0, Event::Paste("language es".to_owned())),
            (0, press(KeyCode::Enter)),
        ]);
        events.extend([
            (300, press(KeyCode::Char('/'))),
            (0, Event::Paste("logout".to_owned())),
            (0, press(KeyCode::Enter)),
        ]);
        events.push((500, press(KeyCode::Esc)));
        let mut app = App::new(kept(&server.uri()));
        let mut terminal = Terminal::new(TestBackend::new(120, 30)).expect("backend");
        tokio::time::timeout(
            Duration::from_secs(10),
            runtime::run(&mut app, &ports, paced(events), &mut draw_on(&mut terminal)),
        )
        .await
        .expect("the loop ended")
        .expect("drawing");

        assert_eq!(app.look().messages.language(), Language::Spanish);
        assert!(matches!(app.screen(), Screen::Server { .. }));
        assert_eq!(tokens.get(&server.uri()).expect("tokens"), None);
        let config = Config::load(&ports.config_path).expect("config");
        assert_eq!(
            (config.settings.colors, config.current),
            (Colors::Terminal, None)
        );
    }
}
