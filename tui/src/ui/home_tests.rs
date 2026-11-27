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
        ..Default::default()
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

/// Locates the last occurrence of text in a rendered screen in terminal cells.
fn pointer(text: &str, label: &str) -> ratatui::layout::Position {
    let byte = text.rfind(label).expect("visible label");
    let prefix = &text[..byte];
    let line = prefix.rsplit('\n').next().expect("current line");
    let row = prefix.split('\n').count() - 1;
    let column = ratatui::text::Line::from(line).width();
    ratatui::layout::Position::new(u16::try_from(column).unwrap(), u16::try_from(row).unwrap())
}

/// Clicks text visible in the main screen at the chosen terminal size.
fn click(home: &mut Home, look: &mut Look, size: (u16, u16), label: &str) -> Vec<Ask> {
    use super::mouse::MouseKind;
    use ratatui::layout::Rect;

    let point = pointer(&draw(home, look, size.0, size.1), label);
    home.mouse(
        Rect::new(0, 0, size.0, size.1),
        point,
        MouseKind::Left,
        false,
        look,
    )
    .0
}

/// Settings, command matches and chooser rows respond to clicks, and help explains text selection.
#[test]
fn clicks_list_popups() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    home.key(key(KeyCode::Char('/')), &mut look);
    nothing(&click(&mut home, &mut look, (120, 30), "/config"), "");
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.lines()
            .any(|line| line.contains("Mouse") && line.contains("yes"))
    );
    assert_eq!(
        click(&mut home, &mut look, (120, 30), "Mouse"),
        vec![Ask::Mouse(false), Ask::Save]
    );
    look.speak("es");
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.lines()
            .any(|line| line.contains("Mouse") && line.contains("no"))
    );
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::F(1)), &mut look);
    assert!(draw(&home, &look, 120, 30).contains("Shift + arrastrar para seleccionar texto"));
    home.key(key(KeyCode::Esc), &mut look);
    look.speak("en");
    home.key(key(KeyCode::F(1)), &mut look);
    assert!(draw(&home, &look, 120, 30).contains("Shift + drag to select text"));
    home.key(key(KeyCode::Esc), &mut look);
    command(&mut home, &mut look, "workspace");
    assert_eq!(
        click(&mut home, &mut look, (120, 30), "Team"),
        vec![Ask::Call(Call::Tree(id(2)))]
    );
}

/// The wheel and clicks follow visible panels at wide and narrow terminal sizes.
#[test]
fn targets_visible_panels() {
    use ratatui::layout::{Position, Rect};

    use super::mouse::MouseKind;
    use super::transfers::{Direction, Row};

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let wide = Rect::new(0, 0, 140, 30);
    let narrow = Rect::new(0, 0, 80, 20);
    let point = pointer(&draw(&home, &look, 80, 20), "home-nas");
    assert!(
        home.mouse(narrow, point, MouseKind::Left, false, &mut look)
            .1
    );
    assert_eq!(home.focus(), Focus::Sites);
    assert!(
        !home
            .mouse(narrow, point, MouseKind::Right, false, &mut look)
            .1
    );
    assert!(
        !home
            .mouse(
                Rect::new(0, 0, 40, 12),
                point,
                MouseKind::Left,
                false,
                &mut look
            )
            .1
    );
    nothing(&click(&mut home, &mut look, (140, 30), "?  help"), "");
    assert!(matches!(home.overlay(), Some(Overlay::Help { .. })));
    home.mouse(wide, point, MouseKind::Down, false, &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    home.mouse(
        wide,
        Position::new(30, 3),
        MouseKind::Left,
        false,
        &mut look,
    );
    assert_eq!(home.focus(), Focus::Left);

    let point = pointer(&draw(&home, &look, 140, 30), "home-nas");
    assert!(matches!(
        home.mouse(wide, point, MouseKind::Left, true, &mut look)
            .0
            .as_slice(),
        [Ask::Call(Call::Files { site, side: Side::Right, .. })] if *site == id(22)
    ));
    assert_eq!(home.focus(), Focus::Right);

    for (number, name) in [(70, "first-transfer"), (71, "second-transfer")] {
        home.queue_mut().start(
            Row::started(id(number), Direction::Copy, name, "nas", None),
            None,
        );
    }
    home.key(key(KeyCode::Char('T')), &mut look);
    click(&mut home, &mut look, (140, 30), "second-transfer");
    assert_eq!(
        home.queue().current().expect("transfer").name,
        "second-transfer"
    );
    home.mouse(wide, point, MouseKind::Down, false, &mut look);
    home.key(ctrl('e'), &mut look);
    click(&mut home, &mut look, (140, 30), "first-transfer");
    assert_eq!(
        home.queue().current().expect("transfer").name,
        "first-transfer"
    );
    assert_eq!(home.focus(), Focus::Bottom);
}

/// The bottom panel shortcut switches between its queue and log tabs.
#[test]
fn clicks_queue_shortcuts() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (180, 30);
    home.key(key(KeyCode::Tab), &mut look);
    home.key(ctrl('e'), &mut look);
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Tab), &mut look);
    assert_eq!(home.focus(), Focus::Bottom);
    assert!(draw(&home, &look, size.0, size.1).contains("[Transfers] Log"));
    nothing(&click(&mut home, &mut look, size, "[ ]  tabs"), "");
    assert!(draw(&home, &look, size.0, size.1).contains("Transfers [Log]"));
    nothing(&click(&mut home, &mut look, size, "Nothing yet:"), "");
    assert!(home.queue().current().is_none());
    nothing(&click(&mut home, &mut look, size, "[ ]  tabs"), "");
    assert!(draw(&home, &look, size.0, size.1).contains("[Transfers] Log"));
    nothing(&click(&mut home, &mut look, size, "No transfers yet"), "");
    assert!(home.queue().current().is_none());
}

/// Each explorer shortcut dispatches the same action as its key.
#[test]
fn clicks_explorer_shortcuts() {
    use super::explorer::{Entry, Kind};

    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Tab), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/work".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/work".to_owned(),
            outcome: Ok(vec![Entry {
                name: "notes.txt".to_owned(),
                kind: Kind::File,
                size: 1,
                modified: None,
            }]),
        },
        &mut look,
    );
    home.key(key(KeyCode::End), &mut look);
    for (label, code) in [
        ("F1  Help", KeyCode::F(1)),
        ("Tab  Panel", KeyCode::Tab),
        ("Enter  Open", KeyCode::Enter),
        ("F3  View", KeyCode::F(3)),
        ("F4  Edit", KeyCode::F(4)),
        ("F5  Copy", KeyCode::F(5)),
        ("F6  Rename", KeyCode::F(6)),
        ("F7  Folder", KeyCode::F(7)),
        ("F8  Delete", KeyCode::F(8)),
        ("F9  Menu", KeyCode::F(9)),
        ("F10  Quit", KeyCode::F(10)),
    ] {
        let mut clicked = home.clone();
        let mut pressed = home.clone();
        assert_eq!(
            click(&mut clicked, &mut look, (170, 30), label),
            pressed.key(key(code), &mut look),
            "{label}",
        );
        assert_eq!(clicked, pressed, "{label}");
    }

    let narrow = draw(&home, &look, 60, 20);
    let shortcuts = narrow.lines().last().expect("shortcut row");
    assert!(shortcuts.trim_end().ends_with("F4  Edit"));
    assert!(shortcuts.contains("F1  Help │ Tab  Panel"));
    assert_eq!(narrow.lines().nth(18).unwrap().trim(), "");
    assert!(!narrow.lines().next().unwrap().contains("transferly-tui"));
    assert!(!shortcuts.contains("F5"));

    look.speak("es");
    assert!(draw(&home, &look, 160, 30).contains("F6  Renombrar"));
    home.reply(Reply::Version(Ok("v99.1.0".to_owned())), &mut look);
    nothing(&click(&mut home, &mut look, (160, 30), "F9  Menú"), "");
    assert!(draw(&home, &look, 160, 30).contains("Copiar la ruta"));
    home.key(key(KeyCode::Esc), &mut look);
    nothing(&click(&mut home, &mut look, (160, 30), "F4  Editar"), "");
    let text = draw(&home, &look, 160, 30);
    assert!(text.lines().nth(27).unwrap().contains(" · Próximamente."));
    assert_eq!(text.lines().nth(28).unwrap().trim(), "");
    let top = text.lines().next().unwrap().trim_end();
    assert!(top.contains("transferly-tui"));
    assert!(top.ends_with(&format!(
        "files.example.com · ana@example.com · Personal · {}",
        look.messages.get("main.vaultOpen")
    )));
    assert!(text.lines().last().unwrap().contains("F3  Ver"));
}

/// The mouse and keyboard menu run the existing actions, including their confirmations.
#[test]
fn context_menu_runs_explorer_shortcuts() {
    use ratatui::layout::Rect;

    use super::explorer::{Entry, Kind};
    use super::mouse::MouseKind;

    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.key(key(KeyCode::BackTab), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/work".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/work".to_owned(),
            outcome: Ok(vec![Entry {
                name: "notes.txt".to_owned(),
                kind: Kind::File,
                size: 1,
                modified: None,
            }]),
        },
        &mut look,
    );
    for code in [KeyCode::F(9), KeyCode::Char('y'), KeyCode::Char('i')] {
        nothing(
            &home.key(key(code), &mut look),
            "no entry on the parent row",
        );
        assert!(home.overlay().is_none());
    }

    let size = (150, 30);
    let point = pointer(&draw(&home, &look, size.0, size.1), "notes.txt");
    let bounds = Rect::new(0, 0, size.0, size.1);
    let mut opened = home.clone();
    let (asks, selected) = opened.mouse(bounds, point, MouseKind::Right, false, &mut look);
    nothing(&asks, "right click only opens the menu");
    assert!(!selected);
    let mut outside = home.clone();
    let (asks, _) = outside.mouse(
        bounds,
        ratatui::layout::Position::new(60, 20),
        MouseKind::Right,
        false,
        &mut look,
    );
    nothing(&asks, "a right click away from every row");
    assert!(outside.overlay().is_none());
    assert_eq!(
        opened.overlay(),
        Some(&Overlay::ContextMenu { selected: 0 })
    );
    let screen = draw(&opened, &look, size.0, size.1);
    let menu = screen.lines().take(28).collect::<Vec<_>>().join("\n");
    assert_eq!(pointer(&menu, "Copy to home-nas").y, point.y + 5);

    home.key(key(KeyCode::End), &mut look);
    for (index, (label, shortcut, code)) in [
        ("Open", "Enter", KeyCode::Enter),
        ("View", "F3", KeyCode::F(3)),
        ("Edit", "F4", KeyCode::F(4)),
        ("Copy to home-nas", "F5", KeyCode::F(5)),
        ("Rename", "F2", KeyCode::F(2)),
        ("Copy the path", "y", KeyCode::Char('y')),
        ("Details", "i", KeyCode::Char('i')),
        ("Delete", "Del", KeyCode::Delete),
    ]
    .into_iter()
    .enumerate()
    {
        let point = pointer(&menu, label);
        assert!(
            menu.lines()
                .nth(usize::from(point.y))
                .unwrap()
                .contains(shortcut)
        );
        let mut clicked = opened.clone();
        let mut navigated = opened.clone();
        let mut pressed = home.clone();
        for _ in 0..index {
            navigated.key(key(KeyCode::Down), &mut look);
        }
        let expected = pressed.key(key(code), &mut look);
        assert_eq!(
            clicked
                .mouse(bounds, point, MouseKind::Left, false, &mut look)
                .0,
            expected,
            "{label}"
        );
        assert_eq!(navigated.key(key(KeyCode::Enter), &mut look), expected);
        assert_eq!(clicked, pressed);
        assert_eq!(navigated, pressed);
    }
}

/// The menu keeps its highlight on unrelated input and closes on Escape or an outside click.
#[test]
fn context_menu_dismisses_and_uses_the_palette() {
    use ratatui::style::Modifier;

    use super::explorer::{Entry, Kind};
    use super::mouse::MouseKind;

    let mut look = look();
    let mut home = loaded(&mut look, None);
    home.key(key(KeyCode::Tab), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/".to_owned(),
            outcome: Ok(vec![Entry {
                name: "notes.txt".to_owned(),
                kind: Kind::File,
                size: 1,
                modified: None,
            }]),
        },
        &mut look,
    );
    home.key(key(KeyCode::F(9)), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Char('q')), &mut look);
    assert_eq!(home.overlay(), Some(&Overlay::ContextMenu { selected: 0 }));

    let size = (60, 16);
    let menu = draw(&home, &look, size.0, size.1);
    let delete = pointer(&menu, "Delete");
    let detail = pointer(&menu, "Details");
    assert_eq!(delete.y, detail.y + 2);
    let mut terminal = Terminal::new(TestBackend::new(size.0, size.1)).unwrap();
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .unwrap();
    assert_eq!(
        terminal.backend().buffer()[(delete.x, delete.y)].fg,
        look.palette.danger
    );

    let bounds = ratatui::layout::Rect::new(0, 0, size.0, size.1);
    let separator = ratatui::layout::Position::new(delete.x, delete.y - 1);
    nothing(
        &home
            .mouse(bounds, separator, MouseKind::Left, false, &mut look)
            .0,
        "separator",
    );
    nothing(
        &home
            .mouse(bounds, separator, MouseKind::Down, false, &mut look)
            .0,
        "wheel",
    );
    assert_eq!(home.overlay(), Some(&Overlay::ContextMenu { selected: 0 }));

    let mut plain = Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::Plain,
        Settings::default(),
    );
    let screen = draw(&home, &plain, size.0, size.1);
    let menu = screen.lines().take(14).collect::<Vec<_>>().join("\n");
    let open = pointer(&menu, "Open");
    terminal
        .draw(|frame| home.render(frame, &plain, "*"))
        .unwrap();
    let buffer = terminal.backend().buffer();
    assert!(
        buffer[(open.x, open.y)]
            .modifier
            .contains(Modifier::REVERSED)
    );
    assert!(
        buffer[(delete.x, delete.y)]
            .modifier
            .contains(Modifier::BOLD)
    );

    home.key(key(KeyCode::Esc), &mut plain);
    assert!(home.overlay().is_none());
    home.key(key(KeyCode::F(9)), &mut plain);
    nothing(
        &click(&mut home, &mut plain, size, "1 Local disk"),
        "outside",
    );
    assert!(home.overlay().is_none());
    for (filter, shortcut) in [
        ("open the context menu", "F9"),
        ("copy the path", "y"),
        ("show entry details", "i"),
    ] {
        home.key(key(KeyCode::F(1)), &mut plain);
        home.paste(filter);
        let help = draw(&home, &plain, 120, 30);
        assert!(help.lines().any(|line| {
            line.contains(filter) && line.split_whitespace().any(|word| word == shortcut)
        }));
        home.key(key(KeyCode::F(1)), &mut plain);
    }
}

/// Chips follow their explorer, use the theme and stay readable in plain mode.
#[test]
fn clicks_explorer_chips() {
    use ratatui::style::{Color, Modifier};

    let mut look = look();
    let mut home = loaded(&mut look, None);
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Ok("/srv".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/srv".to_owned(),
            outcome: Ok(Vec::new()),
        },
        &mut look,
    );
    let size = (150, 30);
    let initial = draw(&home, &look, size.0, size.1);
    let border = initial.lines().nth(27).unwrap();
    assert_eq!(
        border
            .matches("Sort Name ↑ · Hidden no · Filter — ╯")
            .count(),
        2
    );
    assert!(border.contains("╰ /srv ─"));

    let narrow = (80, 20);
    nothing(&click(&mut home, &mut look, narrow, "Sort"), "");
    assert_eq!(home.focus(), Focus::Right);
    assert!(draw(&home, &look, size.0, size.1).contains("Sort Size ↑"));
    nothing(&click(&mut home, &mut look, size, "Size ↑"), "");
    nothing(&click(&mut home, &mut look, size, "Date ↑"), "");
    nothing(&click(&mut home, &mut look, narrow, "↑ · Hidden"), "");
    assert!(draw(&home, &look, size.0, size.1).contains("Sort Name ↓"));
    nothing(&click(&mut home, &mut look, narrow, "Hidden no"), "");
    assert!(draw(&home, &look, size.0, size.1).contains("Hidden yes"));
    nothing(&click(&mut home, &mut look, narrow, " · Filter"), "");
    assert_eq!(home.focus(), Focus::Right);
    assert!(!home.panel(Side::Right).captures());

    nothing(&click(&mut home, &mut look, narrow, "Tab  Panel"), "");
    assert_eq!(home.focus(), Focus::Sites);
    home.key(key(KeyCode::Tab), &mut look);
    nothing(&click(&mut home, &mut look, narrow, "Filter —"), "");
    home.paste("data");
    nothing(&click(&mut home, &mut look, narrow, "Enter  Open"), "");
    assert!(!home.panel(Side::Left).captures());
    let text = draw(&home, &look, size.0, size.1);
    assert!(text.contains("Name ↑ · Hidden no · Filter data"));
    assert!(text.contains("Name ↓ · Hidden yes · Filter —"));

    let mut terminal = Terminal::new(TestBackend::new(size.0, size.1)).expect("backend");
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .expect("draw");
    let buffer = terminal.backend().buffer();
    let name = pointer(&text, "Name ↓");
    let hidden = pointer(&text, "no · Filter");
    let help = pointer(&text, "F1  Help");
    assert_eq!(buffer[(name.x, name.y)].bg, look.palette.accent);
    assert_eq!(buffer[(hidden.x, hidden.y)].bg, look.palette.raised);
    assert_eq!(buffer[(help.x, help.y)].bg, look.palette.accent);

    let plain = Look::new(
        Language::Spanish,
        DEFAULT_THEME,
        ColorMode::Plain,
        Settings::default(),
    );
    let text = draw(&home, &plain, size.0, size.1);
    assert!(text.contains("Orden Nombre ↓ · Ocultos sí · Filtro —"));
    assert!(text.contains("F1  Ayuda"));
    terminal
        .draw(|frame| home.render(frame, &plain, "*"))
        .expect("draw");
    let name = pointer(&text, "Nombre ↓");
    let cell = &terminal.backend().buffer()[(name.x, name.y)];
    assert_eq!(cell.bg, Color::Reset);
    assert!(cell.modifier.contains(Modifier::BOLD));
}

/// Top bar text, panel borders and empty popup rows leave the current selection alone.
#[test]
fn ignores_clicks_outside_mouse_targets() {
    use ratatui::layout::{Position, Rect};

    use super::mouse::MouseKind;

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (140, 30);
    for label in ["1 Local disk", "ana@example.com", "╭ Personal"] {
        nothing(&click(&mut home, &mut look, size, label), label);
        assert_eq!(home.focus(), Focus::Sites);
        assert!(home.overlay().is_none());
    }

    let (asks, selected) = home.mouse(
        Rect::new(0, 0, size.0, size.1),
        Position::new(size.0 - 1, size.1 - 1),
        MouseKind::Left,
        false,
        &mut look,
    );
    nothing(&asks, "empty shortcut space");
    assert!(!selected && home.overlay().is_none());

    home.key(key(KeyCode::Char('/')), &mut look);
    let commands = home.overlay().cloned();
    let point = pointer(&draw(&home, &look, size.0, size.1), "1 Local disk");
    nothing(
        &home
            .mouse(
                Rect::new(0, 0, size.0, size.1),
                point,
                MouseKind::Right,
                false,
                &mut look,
            )
            .0,
        "right click leaves other overlays alone",
    );
    nothing(&click(&mut home, &mut look, size, "1 Local disk"), "");
    assert_eq!(home.overlay(), commands.as_ref());
    home.key(key(KeyCode::Esc), &mut look);

    home.key(key(KeyCode::Char('T')), &mut look);
    nothing(&click(&mut home, &mut look, size, "No transfers yet"), "");
    assert_eq!(home.overlay(), Some(&Overlay::Transfers));
    assert!(home.queue().current().is_none());
}

/// Clicking and scrolling over the vault prompt leave it open and preserve the typed password.
#[test]
fn ignores_mouse_input_on_the_vault_prompt() {
    use ratatui::layout::Rect;

    use super::mouse::MouseKind;

    let mut look = look();
    let mut home = loaded(&mut look, Some((true, false)));
    let size = (140, 30);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.paste("secret");
    let vault = home.overlay().cloned();
    let label = "Unlock the vault";
    nothing(&click(&mut home, &mut look, size, label), "");
    assert_eq!(home.overlay(), vault.as_ref());

    let point = pointer(&draw(&home, &look, size.0, size.1), label);
    let (asks, selected) = home.mouse(
        Rect::new(0, 0, size.0, size.1),
        point,
        MouseKind::Down,
        false,
        &mut look,
    );
    nothing(&asks, "scrolling over the prompt");
    assert!(!selected);
    assert_eq!(home.overlay(), vault.as_ref());
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
            .chain([Ask::Stream])
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
        Some(("The server refused the operation (code: internal).", true))
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
        ("other", "The server refused the operation (code: other)."),
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
            Some(Overlay::Help { .. }) => "Help",
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
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Char('x')), &mut look);
    assert_eq!(
        look.settings,
        Settings {
            colors: Colors::Terminal,
            enter_on_file: EnterOnFile::Details,
            hidden_files: true,
            confirm_quit: false,
            check_updates: false,
            mouse: true,
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

/// `?` and F1 show every key grouped; typing filters them, the arrows and pages scroll, Esc clears the filter and then closes, `?` closes
/// when nothing is typed and F1 always; q and F10 quit.
#[test]
fn shows_help_and_quits() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    home.key(key(KeyCode::Char('?')), &mut look);
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("Everywhere") && text.contains("Ctrl+B") && text.contains("open the site"),
        "{text}"
    );
    for scroll in [
        KeyCode::PageDown,
        KeyCode::Down,
        KeyCode::Down,
        KeyCode::Up,
        KeyCode::PageUp,
    ] {
        home.key(key(scroll), &mut look);
    }
    for _ in 0..8 {
        home.key(key(KeyCode::PageDown), &mut look);
    }
    assert!(
        draw(&home, &look, 120, 30).contains("Text fields"),
        "scrolled to the end"
    );
    type_text(&mut home, &mut look, "renam");
    home.paste("e");
    let text = draw(&home, &look, 120, 30);
    assert!(text.contains("F2") && !text.contains("Ctrl+B"), "{text}");
    home.key(key(KeyCode::Char('?')), &mut look);
    assert!(draw(&home, &look, 120, 30).contains("No key matches."));
    home.key(key(KeyCode::Esc), &mut look);
    assert!(
        draw(&home, &look, 120, 30).contains("Ctrl+B"),
        "the filter cleared"
    );
    home.key(key(KeyCode::Esc), &mut look);
    assert!(home.overlay().is_none());
    home.key(key(KeyCode::F(1)), &mut look);
    nothing(
        &home.key(key(KeyCode::Char('q')), &mut look),
        "typed into the filter",
    );
    home.key(key(KeyCode::F(5)), &mut look);
    home.key(key(KeyCode::F(1)), &mut look);
    assert!(home.overlay().is_none());
    home.key(key(KeyCode::Char('?')), &mut look);
    home.key(key(KeyCode::Char('?')), &mut look);
    assert!(home.overlay().is_none());
    command(&mut home, &mut look, "config");
    home.paste("ignored by the settings");
    home.key(key(KeyCode::Esc), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Char('q')), &mut look),
        vec![Ask::Quit]
    );
    assert_eq!(home.key(key(KeyCode::F(10)), &mut look), vec![Ask::Quit]);
    home.say("kept".to_owned(), false);
    home.key(key(KeyCode::Tab), &mut look);
    home.paste("ignored by a panel that is not typing");
    home.key(key(KeyCode::Char('?')), &mut look);
    home.paste("ignored by the help");
    home.key(key(KeyCode::Esc), &mut look);
    assert!(draw(&home, &look, 120, 30).contains("kept"));
    home.paste("nowhere");
}

/// The two panels of the explorer: a site opens on the right in its home folder, keys reach the focused panel, its operations go to
/// the server with the workspace and to the local disk, replies reach the panel that asked, and `o` picks a site on the right.
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
        ..Default::default()
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
        ..Default::default()
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
    assert!(
        draw(&home, &look, 140, 30).contains("The server refused the operation (code: odd_code).")
    );
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
        ..Default::default()
    };
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home/ana".to_owned(),
            outcome: Err(disk),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 140, 30).contains("The operation failed."));

    assert_eq!(home.key(ctrl('d'), &mut look), vec![files(Op::Disconnect)]);
    assert!(home.panel(Side::Right).source().is_none());
    let text = draw(&home, &look, 140, 30);
    assert!(text.contains("Choose a site on the left (Enter) to connect"));
    look.speak("es");
    assert!(
        draw(&home, &look, 140, 30)
            .contains("Elige un sitio a la izquierda (Enter) para conectarte")
    );
    look.speak("en");

    home.key(key(KeyCode::Char('o')), &mut look);
    let text = draw(&home, &look, 140, 30);
    assert!(
        text.contains("Choose a site") && text.contains("acme-sftp"),
        "{text}"
    );
    assert_eq!(text.matches("Local disk").count(), 2, "{text}");
    nothing(
        &home.key(key(KeyCode::Enter), &mut look),
        "a site with credentials in a locked vault asks first",
    );
    assert!(matches!(home.overlay(), Some(Overlay::Vault { .. })));
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Char('o')), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![Ask::Call(Call::Files {
            workspace: id(1),
            site: id(21),
            side: Side::Right,
            op: Op::Home
        })]
    );
    home.key(key(KeyCode::BackTab), &mut look);
    assert_eq!(home.focus(), Focus::Left);
    nothing(&home.key(key(KeyCode::Char('o')), &mut look), "");
    assert!(home.overlay().is_none());

    home.key(key(KeyCode::BackTab), &mut look);
    assert!(draw(&home, &look, 80, 20).contains("Clients"));
    home.key(key(KeyCode::End), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![files(Op::Home)]
    );
    assert_eq!(home.focus(), Focus::Right);
    assert!(draw(&home, &look, 80, 20).contains("home-nas"));
    command(&mut home, &mut look, "workspace team");
    assert!(home.panel(Side::Right).source().is_none());
    assert_eq!(
        home.panel(Side::Left).source(),
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

/// A server whose key is not trusted yet asks to trust it, showing the site, the server and the fingerprint; `y` trusts it and the
/// panel tries again. A key that changed shows the ones trusted until now and asks whether to add or replace; Esc gives up.
#[test]
fn trusts_server_keys() {
    use std::collections::HashMap;
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    let mut params = HashMap::new();
    params.insert("keyType".to_owned(), serde_json::json!("ssh-ed25519"));
    params.insert("fingerprint".to_owned(), serde_json::json!("SHA256:new"));
    let unknown = CallError {
        code: Some("host_key_unknown".to_owned()),
        message: "x".to_owned(),
        params: params.clone(),
    };
    home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Err(unknown),
        },
        &mut look,
    );
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("Trust this server?")
            && text.contains("home-nas.example.com:22")
            && text.contains("SHA256:new"),
        "{text}"
    );
    nothing(
        &home.key(key(KeyCode::Char('a')), &mut look),
        "add is for keys that changed",
    );
    let key_of = |mode| crate::api::HostKey {
        host: "home-nas.example.com".to_owned(),
        port: 22,
        key_type: "ssh-ed25519".to_owned(),
        fingerprint: "SHA256:new".to_owned(),
        mode,
    };
    let trust = |mode| {
        vec![Ask::Call(Call::Trust {
            workspace: id(1),
            side: Side::Right,
            key: key_of(mode),
        })]
    };
    assert_eq!(
        click(&mut home, &mut look, (120, 30), "y trust"),
        trust(None)
    );
    let again = home.reply(
        Reply::Trusted {
            side: Side::Right,
            outcome: Ok(()),
        },
        &mut look,
    );
    assert_eq!(
        again,
        vec![Ask::Call(Call::Files {
            workspace: id(1),
            site: id(22),
            side: Side::Right,
            op: Op::Home
        })]
    );
    home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Ok("/".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/".to_owned(),
            outcome: Ok(vec![]),
        },
        &mut look,
    );

    params.insert("trusted".to_owned(), serde_json::json!(["SHA256:old"]));
    let changed = CallError {
        code: Some("host_key_changed".to_owned()),
        message: "x".to_owned(),
        params,
    };
    home.key(key(KeyCode::Char('r')), &mut look);
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/".to_owned(),
            outcome: Err(changed.clone()),
        },
        &mut look,
    );
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("The key of this server changed") && text.contains("SHA256:old"),
        "{text}"
    );
    nothing(
        &home.key(key(KeyCode::Char('y')), &mut look),
        "a changed key is added or replaced",
    );
    assert_eq!(
        home.key(key(KeyCode::Char('a')), &mut look),
        trust(Some(crate::api::TrustMode::Add))
    );
    let refused = home.reply(
        Reply::Trusted {
            side: Side::Right,
            outcome: Err(refused("forbidden")),
        },
        &mut look,
    );
    nothing(&refused, "");
    assert!(draw(&home, &look, 120, 30).contains("Your role in this"));
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/".to_owned(),
            outcome: Err(changed.clone()),
        },
        &mut look,
    );
    assert_eq!(
        click(&mut home, &mut look, (120, 30), "r replace"),
        trust(Some(crate::api::TrustMode::Replace))
    );
    assert_eq!(
        home.reply(
            Reply::Trusted {
                side: Side::Right,
                outcome: Ok(())
            },
            &mut look
        ),
        vec![Ask::Call(Call::Files {
            workspace: id(1),
            site: id(22),
            side: Side::Right,
            op: Op::List("/".to_owned())
        })]
    );
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/".to_owned(),
            outcome: Err(changed.clone()),
        },
        &mut look,
    );
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    assert!(home.overlay().is_none());
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home".to_owned(),
            outcome: Err(changed),
        },
        &mut look,
    );
    assert!(
        home.overlay().is_none(),
        "the local disk has no keys to trust"
    );
}

/// A site that asks for its password when connecting shows a hidden field; Enter gives it to the server and the panel tries again,
/// an empty field waits, Esc gives up, and a refusal is said in the panel.
#[test]
fn asks_for_passwords() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Err(refused("credentials_required")),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 120, 30).contains("Password of home-nas"));
    nothing(
        &home.key(key(KeyCode::Enter), &mut look),
        "an empty password",
    );
    home.paste("se");
    type_text(&mut home, &mut look, "cret");
    assert!(draw(&home, &look, 120, 30).contains("••••••"));
    let given = home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(
        given,
        vec![Ask::Call(Call::Credential {
            workspace: id(1),
            site: id(22),
            side: Side::Right,
            password: "secret".to_owned()
        })]
    );
    let again = home.reply(
        Reply::Credentialed {
            side: Side::Right,
            outcome: Ok(()),
        },
        &mut look,
    );
    assert_eq!(
        again,
        vec![Ask::Call(Call::Files {
            workspace: id(1),
            site: id(22),
            side: Side::Right,
            op: Op::Home
        })]
    );
    home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Err(refused("credentials_required")),
        },
        &mut look,
    );
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    assert!(home.overlay().is_none());
    home.reply(
        Reply::Credentialed {
            side: Side::Right,
            outcome: Err(refused("auth_failed")),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 120, 30).contains("The server refused"));
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/".to_owned(),
            outcome: Err(refused("credentials_required")),
        },
        &mut look,
    );
    assert!(home.overlay().is_none(), "the local disk asks for nothing");
}

/// What a server asks while a site of a panel connects is shown to answer, hidden unless the server allows showing it; Tab and the
/// arrows move, Enter sends from the last answer, Esc refuses; a prompt that closes elsewhere closes here; prompts of sites no panel
/// shows, or while something else is on the screen, are left to the client that connects. The log keeps the latest steps.
#[test]
fn answers_servers_and_logs() {
    use super::home::{LogLine, Prompt};
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    let prompt = |number: u8, site: u8| Prompt {
        id: id(number),
        workspace: id(1),
        site: id(site),
        name: "Login".to_owned(),
        instruction: String::new(),
        questions: vec![("User:".to_owned(), true), ("Code:".to_owned(), false)],
    };
    home.reply(Reply::Prompt(prompt(50, 20)), &mut look);
    assert!(home.overlay().is_none(), "a site no panel shows");
    home.reply(Reply::Prompt(prompt(50, 22)), &mut look);
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("home-nas asks") && text.contains("Login") && text.contains("Code:"),
        "{text}"
    );
    type_text(&mut home, &mut look, "ana");
    home.key(key(KeyCode::Tab), &mut look);
    home.paste("12");
    type_text(&mut home, &mut look, "34");
    let text = draw(&home, &look, 120, 30);
    assert!(
        text.contains("ana") && text.contains("••••") && !text.contains("1234"),
        "{text}"
    );
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.reply(Reply::Prompt(prompt(51, 22)), &mut look);
    let sent = home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(
        sent,
        vec![Ask::Call(Call::Answer {
            id: id(50),
            answers: vec!["ana".to_owned(), "1234".to_owned()]
        })]
    );
    assert!(home.overlay().is_none());
    home.reply(Reply::Answered(Err(refused("prompt_not_found"))), &mut look);
    assert_eq!(home.notice().map(|n| n.error), Some(true));
    home.reply(Reply::Answered(Ok(())), &mut look);

    let mut bare = prompt(52, 22);
    bare.name = String::new();
    let mut silent = prompt(54, 22);
    silent.questions.clear();
    assert_eq!(
        home.reply(Reply::Prompt(silent), &mut look),
        vec![Ask::Call(Call::Answer {
            id: id(54),
            answers: vec![]
        })]
    );
    home.reply(Reply::Prompt(bare), &mut look);
    assert!(draw(&home, &look, 120, 30).contains("The server asks before"));
    home.reply(Reply::PromptClosed(id(99)), &mut look);
    assert!(home.overlay().is_some(), "another prompt closed");
    home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Esc), &mut look),
        vec![Ask::Call(Call::CancelPrompt(id(52)))]
    );
    home.reply(Reply::Prompt(prompt(53, 22)), &mut look);
    home.reply(Reply::PromptClosed(id(53)), &mut look);
    assert!(home.overlay().is_none());

    home.key(ctrl('e'), &mut look);
    assert!(draw(&home, &look, 140, 40).contains("No transfers yet"));
    while home.focus() != Focus::Bottom {
        home.key(key(KeyCode::Tab), &mut look);
    }
    home.key(key(KeyCode::Char(']')), &mut look);
    assert!(draw(&home, &look, 140, 40).contains("Nothing yet"));
    let line = |step: &str, level: &str| LogLine {
        site: id(22),
        time: Some(jiff::Timestamp::from_second(0).expect("a time")),
        level: level.to_owned(),
        step: step.to_owned(),
        params: vec![("host".to_owned(), "nas".to_owned())],
    };
    for n in 0..501 {
        home.reply(Reply::Log(line(&format!("step{n}"), "info")), &mut look);
    }
    home.reply(Reply::Log(line("warned", "warn")), &mut look);
    home.reply(Reply::Log(line("failed", "error")), &mut look);
    home.reply(
        Reply::Log(LogLine {
            site: id(99),
            time: None,
            ..line("other", "info")
        }),
        &mut look,
    );
    let text = draw(&home, &look, 140, 40);
    assert!(
        text.contains("Transfers [Log]")
            && text.contains("21:00:00 home-nas")
            && text.contains("failed host=nas")
            && text.contains("other"),
        "{text}"
    );
    home.key(ctrl('e'), &mut look);
    assert!(!draw(&home, &look, 140, 40).contains("Transfers [Log]"));
}

/// `c` and `F5` copy what a panel selects to the other after a confirmation with the policy for what exists: uploads and downloads go
/// through this client and copies between sites to the server; what the policy leaves, and folders between this computer and a site,
/// are left out and said; the window of transfers shows them, and the status bar sums them up.
#[test]
fn copies_between_panels() {
    use super::explorer::{Entry, Kind};
    use super::transfers::{Direction, Job, Row};
    let file = |name: &str, seconds: i64| Entry {
        name: name.to_owned(),
        kind: Kind::File,
        size: 10,
        modified: Some(jiff::Timestamp::from_second(seconds).expect("a time")),
    };
    let folder = Entry {
        name: "docs".to_owned(),
        kind: Kind::Folder,
        size: 0,
        modified: None,
    };
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Tab), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/home/ana".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home/ana".to_owned(),
            outcome: Ok(vec![file("a.txt", 50), file("b.txt", 0), folder.clone()]),
        },
        &mut look,
    );
    home.key(ctrl('a'), &mut look);
    home.key(key(KeyCode::F(5)), &mut look);
    assert!(draw(&home, &look, 140, 30).contains("The other panel shows nothing"));
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Ok("/srv".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/srv".to_owned(),
            outcome: Ok(vec![file("a.txt", 10), file("b (1).txt", 0)]),
        },
        &mut look,
    );

    home.key(key(KeyCode::BackTab), &mut look);
    home.key(key(KeyCode::Char('c')), &mut look);
    let text = draw(&home, &look, 140, 30);
    assert!(
        text.contains("Copy 3 entries to /srv:")
            && text.contains("docs/")
            && text.contains("stop with an error"),
        "{text}"
    );
    for policy in [
        KeyCode::Right,
        KeyCode::Right,
        KeyCode::Right,
        KeyCode::Left,
        KeyCode::Char('l'),
        KeyCode::Char('x'),
    ] {
        home.key(key(policy), &mut look);
    }
    assert!(draw(&home, &look, 140, 30).contains("replace it if older"));
    let asks = home.key(key(KeyCode::Enter), &mut look);
    let [
        Ask::Transfer(newer, Side::Right),
        Ask::Transfer(fresh, Side::Right),
        Ask::Transfer(docs, Side::Right),
    ] = asks.as_slice()
    else {
        panic!("{asks:?}")
    };
    assert_eq!(
        (newer.remote.as_str(), newer.mode.as_str(), newer.direction),
        ("/srv/a.txt", "overwrite", Direction::Upload)
    );
    assert_eq!(
        (fresh.remote.as_str(), fresh.mode.as_str()),
        ("/srv/b.txt", "create")
    );
    assert!(docs.folder && docs.remote == "/srv/docs");
    assert!(draw(&home, &look, 140, 30).contains("Transfers · 0 of 3"));
    home.key(ctrl('e'), &mut look);
    assert!(
        draw(&home, &look, 140, 30)
            .lines()
            .next()
            .unwrap()
            .contains("⇅ 3")
    );
    home.key(key(KeyCode::Esc), &mut look);
    assert!(home.overlay().is_none());
    home.key(key(KeyCode::BackTab), &mut look);
    assert_eq!(home.focus(), Focus::Right);
    home.key(ctrl('a'), &mut look);
    home.key(key(KeyCode::Char('c')), &mut look);
    for _ in 0..4 {
        home.key(key(KeyCode::Right), &mut look);
    }
    let asks = home.key(key(KeyCode::Char('y')), &mut look);
    let [
        Ask::Transfer(first, Side::Left),
        Ask::Transfer(second, Side::Left),
    ] = asks.as_slice()
    else {
        panic!("{asks:?}")
    };
    assert_eq!(
        (first.local.to_string_lossy().into_owned(), first.direction),
        ("/home/ana/a (1).txt".to_owned(), Direction::Download)
    );
    assert_eq!(second.local.to_string_lossy(), "/home/ana/b (1).txt");
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Char('c')), &mut look);
    home.key(key(KeyCode::Right), &mut look);
    home.key(key(KeyCode::Right), &mut look);
    nothing(
        &home.key(key(KeyCode::Enter), &mut look),
        "everything exists and is left",
    );
    home.key(key(KeyCode::Char('c')), &mut look);
    nothing(&home.key(key(KeyCode::Char('n')), &mut look), "");

    home.panel_mut(Side::Right)
        .open(super::explorer::Source::Site {
            id: id(20),
            name: "acme-sftp".to_owned(),
        });
    home.key(key(KeyCode::BackTab), &mut look);
    home.panel_mut(Side::Left)
        .open(super::explorer::Source::Site {
            id: id(22),
            name: "home-nas".to_owned(),
        });
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/".to_owned(),
            outcome: Ok(vec![folder]),
        },
        &mut look,
    );
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Char('c')), &mut look);
    let asks = home.key(key(KeyCode::Enter), &mut look);
    assert!(
        matches!(asks.as_slice(), [Ask::Call(Call::Copy { from, to, policy, side: Side::Right, .. })] if from == "/docs" && to == "/docs" && policy == "fail"),
        "{asks:?}"
    );

    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Tab), &mut look);
    home.panel_mut(Side::Right)
        .open(super::explorer::Source::Local);
    home.key(key(KeyCode::BackTab), &mut look);
    home.panel_mut(Side::Left)
        .open(super::explorer::Source::Local);
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/home/ana".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home/ana".to_owned(),
            outcome: Ok(vec![file("a.txt", 0)]),
        },
        &mut look,
    );
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Char('c')), &mut look);
    assert!(
        home.notice()
            .is_some_and(|n| n.text.starts_with("Both panels show the local disk"))
    );
    let _ = (Job::clone, Row::clone);
}

/// The queue in the bottom panel: `[` `]` switch tabs, the arrows move, `p` pauses a transfer that waits or a copy, cuts one of this
/// client that runs and resumes one that was cut, `x` cancels after asking, `R` resumes, `+` `-` reorder; a transfer of this client that
/// ends lists its panel again, and one that fails is marked with its reason.
#[test]
fn manages_the_queue() {
    use super::transfers::{Direction, Job, Row, Status};
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    let at = |n: u8| id(60 + n);
    let job = |n: u8, direction| Job {
        id: at(n),
        direction,
        workspace: id(1),
        site: id(22),
        local: "/l".into(),
        remote: "/r".into(),
        mode: "create".to_owned(),
        from: 0,
        side: if direction == Direction::Upload {
            Side::Right
        } else {
            Side::Left
        },
        folder: false,
    };
    let state = |n: u8, direction: &str, status: &str| -> Row {
        serde_json::from_value(serde_json::json!({"id": at(n), "direction": direction, "name": format!("t{n}"), "status": status, "bytesDone": 5, "totalBytes": 10, "errorCode": if status == "failed" { Some("disk_full") } else { None }})).expect("a row")
    };
    home.key(ctrl('e'), &mut look);
    while home.focus() != Focus::Bottom {
        home.key(key(KeyCode::Tab), &mut look);
    }
    assert!(draw(&home, &look, 140, 30).contains("No transfers yet"));
    nothing(
        &home.key(key(KeyCode::Char('p')), &mut look),
        "an empty queue",
    );
    home.reply(
        Reply::Snapshot(vec![
            state(1, "copy", "queued"),
            state(2, "upload", "running"),
            state(3, "download", "interrupted"),
            state(4, "upload", "failed"),
        ]),
        &mut look,
    );
    home.reply(
        Reply::TransferState(state(5, "upload", "queued")),
        &mut look,
    );
    home.reply(
        Reply::Progress {
            id: at(2),
            done: 7,
            total: None,
            at: std::time::Instant::now(),
        },
        &mut look,
    );
    let text = draw(&home, &look, 140, 30);
    assert!(
        text.contains("t5")
            && text.contains("The server refused the operation")
            && text.contains("cut: R resumes")
            && text.contains("⇅ 3"),
        "{text}"
    );
    let action = |n: u8, action: &str| {
        vec![Ask::Call(Call::TransferAction {
            id: at(n),
            action: action.to_owned(),
        })]
    };
    assert_eq!(
        home.key(key(KeyCode::Char('p')), &mut look),
        action(5, "pause")
    );
    assert_eq!(
        home.key(key(KeyCode::Char('+')), &mut look),
        vec![Ask::Call(Call::Priority {
            id: at(5),
            priority: 1
        })]
    );
    assert_eq!(
        home.key(key(KeyCode::Char('-')), &mut look),
        vec![Ask::Call(Call::Priority {
            id: at(5),
            priority: -1
        })]
    );
    home.key(key(KeyCode::Char('x')), &mut look);
    assert!(draw(&home, &look, 140, 30).contains("Cancel t5?"));
    nothing(
        &home.key(key(KeyCode::Char('z')), &mut look),
        "waits for an answer",
    );
    assert_eq!(
        click(&mut home, &mut look, (140, 30), "y delete"),
        [vec![Ask::Abort(at(5))], action(5, "cancel")].concat()
    );
    home.key(key(KeyCode::Char('x')), &mut look);
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    home.key(key(KeyCode::Delete), &mut look);
    assert!(home.overlay().is_some(), "Delete asks too");
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Char('p')), &mut look),
        action(1, "pause")
    );
    home.key(key(KeyCode::Char('j')), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Char('p')), &mut look),
        vec![Ask::Abort(at(2))]
    );
    home.key(key(KeyCode::Down), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Char('R')), &mut look),
        action(3, "resume"),
        "a transfer this client did not start"
    );
    home.queue_mut().start(
        state(3, "download", "interrupted"),
        Some(job(3, Direction::Download)),
    );
    let resumed = home.key(key(KeyCode::Char('R')), &mut look);
    assert!(
        matches!(resumed.as_slice(), [Ask::Transfer(job, Side::Left)] if job.mode == "resume" && job.from == 0),
        "{resumed:?}"
    );
    home.key(key(KeyCode::Down), &mut look);
    home.queue_mut().start(
        state(4, "upload", "failed"),
        Some(job(4, Direction::Upload)),
    );
    home.queue_mut().cursor = 4;
    let resumed = home.key(key(KeyCode::Char('p')), &mut look);
    assert!(
        matches!(resumed.as_slice(), [Ask::Transfer(job, Side::Right)] if job.from == 5),
        "{resumed:?}"
    );
    nothing(&home.key(key(KeyCode::Char('z')), &mut look), "");
    home.key(key(KeyCode::Char('k')), &mut look);
    home.key(key(KeyCode::Up), &mut look);

    let ended = home.reply(
        Reply::TransferEnded {
            id: at(4),
            side: Side::Right,
            outcome: Err(CallError {
                code: None,
                message: "reset".to_owned(),
                ..Default::default()
            }),
        },
        &mut look,
    );
    nothing(&ended, "");
    assert_eq!(
        home.queue()
            .rows()
            .iter()
            .find(|row| row.id == at(4))
            .map(|row| row.status),
        Some(Status::Interrupted)
    );
    home.reply(
        Reply::TransferEnded {
            id: at(9),
            side: Side::Right,
            outcome: Err(refused("permission_denied")),
        },
        &mut look,
    );
    assert!(draw(&home, &look, 140, 30).contains("does not let"));
    home.reply(
        Reply::TransferEnded {
            id: at(4),
            side: Side::Left,
            outcome: Ok(()),
        },
        &mut look,
    );
    home.key(key(KeyCode::Char(']')), &mut look);
    nothing(&home.key(key(KeyCode::Char('j')), &mut look), "the log tab");
    home.key(key(KeyCode::Char('[')), &mut look);
    assert!(
        draw(&home, &look, 80, 20).contains("t5"),
        "a narrow window shows the explorer"
    );
    home.key(ctrl('e'), &mut look);
    assert_ne!(home.focus(), Focus::Bottom);
}

/// The window of transfers and the queue name what they show: more entries than fit are counted, transfers without a size show only
/// how many run, finished ones say so, a failure marks the transfer, and `x` on one that ended does nothing.
#[test]
fn shows_transfers() {
    use super::explorer::{Entry, Kind};
    use super::transfers::{Direction, Row};
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.queue_mut().start(
        Row::started(id(70), Direction::Copy, "big", "nas", None),
        None,
    );
    assert!(draw(&home, &look, 140, 30).contains("⇅ 1 "));
    let done: Row = serde_json::from_value(serde_json::json!({"id": id(71), "direction": "upload", "name": "ok.bin", "status": "done", "bytesDone": 3, "totalBytes": 3})).expect("a row");
    home.reply(Reply::TransferState(done), &mut look);
    home.reply(
        Reply::TransferEnded {
            id: id(70),
            side: Side::Right,
            outcome: Err(refused("disk_full")),
        },
        &mut look,
    );
    home.key(ctrl('e'), &mut look);
    while home.focus() != Focus::Bottom {
        home.key(key(KeyCode::Tab), &mut look);
    }
    let text = draw(&home, &look, 140, 30);
    assert!(
        text.contains("done") && text.contains("The server refused the operation"),
        "{text}"
    );
    nothing(
        &home.key(key(KeyCode::Char('x')), &mut look),
        "a transfer that ended",
    );
    let canceled: Row = serde_json::from_value(serde_json::json!({"id": id(72), "direction": "copy", "name": "gone", "status": "canceled"})).expect("a row");
    home.reply(Reply::TransferState(canceled), &mut look);
    assert!(draw(&home, &look, 140, 30).contains("canceled"));
    assert!(home.overlay().is_none());

    home.key(key(KeyCode::Tab), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/home/ana".to_owned()),
        },
        &mut look,
    );
    let files: Vec<Entry> = (0..8)
        .map(|n| Entry {
            name: format!("f{n}"),
            kind: Kind::File,
            size: 1,
            modified: None,
        })
        .collect();
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home/ana".to_owned(),
            outcome: Ok(files),
        },
        &mut look,
    );
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Tab), &mut look);
    home.key(key(KeyCode::Char('o')), &mut look);
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.key(key(KeyCode::BackTab), &mut look);
    assert_eq!(home.focus(), Focus::Left);
    home.key(ctrl('a'), &mut look);
    home.key(key(KeyCode::Char('c')), &mut look);
    assert!(draw(&home, &look, 140, 40).contains("and 2 more"));
}

/// The Spanish message line explains what to do and both queue views show the short causes.
#[test]
fn explains_transfer_failures_in_spanish() {
    use super::transfers::{Direction, Row};
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    look.speak("es");
    for (number, name, code, cause) in [
        (
            70,
            "existing.txt",
            "already_exists",
            "Ya existe algo con ese nombre.",
        ),
        (
            71,
            "tab\tname.txt",
            "invalid_path",
            "El nombre tiene un carácter inválido.",
        ),
    ] {
        home.queue_mut().start(
            Row::started(id(number), Direction::Download, name, "nas", None),
            None,
        );
        home.reply(
            Reply::TransferEnded {
                id: id(number),
                side: Side::Right,
                outcome: Err(refused(code)),
            },
            &mut look,
        );
        assert!(draw(&home, &look, 160, 36).contains(cause));
    }
    assert!(home.notice().is_some_and(|notice| {
        notice.text.contains("en el servidor con otra herramienta")
            && notice.text.contains("tabulaciones o saltos de línea")
    }));
    home.reply(
        Reply::TransferState(
            serde_json::from_value(serde_json::json!({
                "id": id(72), "direction": "copy", "name": "no-code.txt", "status": "failed"
            }))
            .expect("a failed transfer"),
        ),
        &mut look,
    );
    for popup in [false, true] {
        if popup {
            home.key(key(KeyCode::Char('T')), &mut look);
        } else {
            home.key(ctrl('e'), &mut look);
        }
        let text = draw(&home, &look, 160, 36);
        assert!(
            text.lines().any(|line| {
                line.contains("tab⇥name.txt")
                    && line.contains("El nombre tiene un carácter inválido.")
            }),
            "{text}"
        );
        assert!(text.contains("Ya existe algo con ese nombre."), "{text}");
        assert!(
            !text.contains("already_exists") && !text.contains("invalid_path"),
            "{text}"
        );
    }
}

/// The window totals weight bytes, sum running speeds and omit progress and time when a size is missing.
#[test]
fn totals_and_gauges_in_the_transfer_window() {
    use ratatui::style::Modifier;

    use super::transfers::Direction::{Copy, Download, Upload};
    use super::transfers::Row;
    use super::transfers::Status::{Done, Failed, Queued, Running};

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let mib = 1024 * 1024;
    for (number, name, direction, status, done, total, speed) in [
        (70, "ready.zip", Upload, Done, 10, 10, 99.0),
        (71, "copy.tar", Copy, Running, 10, 40, 10.0),
        (72, "backup.sql", Download, Running, 20, 40, 5.0),
        (73, "secret.key", Download, Failed, 0, 0, 99.0),
        (74, "waiting.txt", Upload, Queued, 0, 10, 0.0),
    ] {
        let mut row = Row::started(id(number), direction, name, "test-nas", Some(total * mib));
        row.status = status;
        row.bytes_done = done * mib;
        row.speed = speed * f64::from(1024 * 1024);
        home.queue_mut().start(row, None);
    }
    home.key(key(KeyCode::Char('T')), &mut look);
    for width in (60..=96).step_by(4) {
        let screen = draw(&home, &look, width, 30);
        assert!(screen.contains("Transfers"), "{width}\n{screen}");
    }
    let screen = draw(&home, &look, 140, 30);
    assert!(screen.contains("Transfers · 1 of 5 · 40% · 15.0 MiB/s · 0:04 left"));
    for (name, gauge, detail) in [
        ("↑ ready.zip", "██", "100%"),
        ("⇄ copy.tar", "█░", "10.0 MiB/s"),
        ("↓ backup.sql", "█░", "5.0 MiB/s"),
        ("↓ secret.key", "░░", "failed"),
    ] {
        let row = screen.lines().find(|line| line.contains(name)).unwrap();
        assert!(
            row.contains(gauge) && row.contains(detail) && row.contains("test-nas"),
            "{row}"
        );
    }
    let narrow = draw(&home, &look, 60, 20);
    let copy = narrow
        .lines()
        .find(|line| line.contains("copy.tar"))
        .unwrap();
    assert!(copy.contains("█░") && copy.contains("25%"), "{copy}");
    assert!(
        !copy.contains("MiB/s") && !copy.contains("test-nas"),
        "{copy}"
    );

    let mut plain = Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::Plain,
        Settings::default(),
    );
    let mut terminal = Terminal::new(TestBackend::new(140, 30)).unwrap();
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .unwrap();
    let buffer = terminal.backend().buffer();
    let cause = pointer(&screen, "failed");
    assert_eq!(buffer[(cause.x, cause.y)].fg, look.palette.danger);
    for (name, color) in [
        ("ready.zip", look.palette.ok),
        ("copy.tar", look.palette.accent),
    ] {
        let position = pointer(&screen, name);
        let gauge = (position.x..140)
            .find(|column| buffer[(*column, position.y)].symbol() == "█")
            .unwrap();
        assert_eq!(buffer[(gauge, position.y)].fg, color);
    }

    terminal
        .draw(|frame| home.render(frame, &plain, "*"))
        .unwrap();
    let plain_screen = draw(&home, &plain, 140, 30);
    assert!(plain_screen.contains("█░"));
    let selected = pointer(&plain_screen, "waiting.txt");
    assert!(
        terminal.backend().buffer()[(selected.x, selected.y)]
            .modifier
            .contains(Modifier::REVERSED)
    );

    plain.speak("es");
    assert!(
        draw(&home, &plain, 140, 30)
            .contains("Transferencias · 1 de 5 · 40% · 15.0 MiB/s · faltan 0:04")
    );
    home.queue_mut().start(
        Row::started(id(75), Upload, "unknown.bin", "test-nas", None),
        None,
    );
    let screen = draw(&home, &look, 140, 30);
    let title = screen
        .lines()
        .find(|line| line.contains("Transfers · 1 of 6 · 15.0 MiB/s"))
        .unwrap();
    assert!(!title.contains('%') && !title.contains("left"), "{title}");
}

/// The transfer window shares selection and actions with the queue and stays open until dismissed.
#[test]
fn controls_the_selected_transfer_in_the_window() {
    use ratatui::layout::Rect;

    use super::mouse::MouseKind;
    use super::transfers::{Direction, Row, Status};

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let mut failed = Row::started(id(70), Direction::Download, "retry.bin", "nas", None);
    failed.status = Status::Failed;
    let queued = Row::started(id(71), Direction::Copy, "queued.bin", "nas", None);
    home.queue_mut().snapshot(vec![queued.clone(), failed]);
    let action = |number, action: &str| {
        Ask::Call(Call::TransferAction {
            id: id(number),
            action: action.to_owned(),
        })
    };
    let size = (140, 30);
    nothing(&home.key(key(KeyCode::Char('T')), &mut look), "");
    nothing(&home.key(key(KeyCode::Down), &mut look), "");
    assert_eq!(
        home.key(key(KeyCode::Char('R')), &mut look),
        [action(70, "resume")]
    );
    nothing(&home.key(key(KeyCode::Up), &mut look), "");
    assert_eq!(
        home.key(key(KeyCode::Char('p')), &mut look),
        [action(71, "pause")]
    );

    let point = pointer(&draw(&home, &look, size.0, size.1), "queued.bin");
    nothing(
        &home
            .mouse(
                Rect::new(0, 0, size.0, size.1),
                point,
                MouseKind::Down,
                false,
                &mut look,
            )
            .0,
        "",
    );
    assert_eq!(home.queue().cursor, 1);
    nothing(&click(&mut home, &mut look, size, "queued.bin"), "");
    let mut paused = queued;
    paused.paused = true;
    home.queue_mut().put(paused);
    assert_eq!(
        home.key(key(KeyCode::Char('p')), &mut look),
        [action(71, "resume")]
    );
    for cancel in [KeyCode::Char('x'), KeyCode::Delete] {
        nothing(&home.key(key(cancel), &mut look), "");
        assert!(draw(&home, &look, size.0, size.1).contains("Cancel queued.bin?"));
        nothing(&home.key(key(KeyCode::Esc), &mut look), "");
        assert_eq!(home.overlay(), Some(&Overlay::Transfers));
    }
    nothing(&home.key(key(KeyCode::Delete), &mut look), "");
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        [Ask::Abort(id(71)), action(71, "cancel")],
    );
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    assert!(home.overlay().is_none());
    nothing(&home.key(key(KeyCode::Char('T')), &mut look), "");
    assert_eq!(home.overlay(), Some(&Overlay::Transfers));
    assert_eq!(home.queue().cursor, 0);
}

/// `T` and `/transfers` bring the window of transfers in front at any moment, saying when there are none yet.
#[test]
fn shows_transfers_on_demand() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    home.key(key(KeyCode::Char('T')), &mut look);
    assert!(draw(&home, &look, 140, 30).contains("No transfers yet"));
    home.key(key(KeyCode::Esc), &mut look);
    assert!(home.overlay().is_none());
    command(&mut home, &mut look, "transfers");
    assert!(matches!(home.overlay(), Some(Overlay::Transfers)));
}

/// Enter on a file asks to copy it alone to the other panel, or shows its details when the settings say so.
#[test]
fn enters_files() {
    use super::explorer::{Entry, Kind};
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('G')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Right,
            outcome: Ok("/srv".to_owned()),
        },
        &mut look,
    );
    let files = vec![
        Entry {
            name: "x.bin".to_owned(),
            kind: Kind::File,
            size: 2048,
            modified: Some(jiff::Timestamp::from_second(0).expect("a time")),
        },
        Entry {
            name: "y.bin".to_owned(),
            kind: Kind::File,
            size: 1,
            modified: None,
        },
    ];
    home.reply(
        Reply::Listed {
            side: Side::Right,
            path: "/srv".to_owned(),
            outcome: Ok(files),
        },
        &mut look,
    );
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/home/ana".to_owned()),
        },
        &mut look,
    );
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home/ana".to_owned(),
            outcome: Ok(vec![]),
        },
        &mut look,
    );
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Char(' ')), &mut look);
    home.key(key(KeyCode::Up), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    let text = draw(&home, &look, 140, 30);
    assert!(text.contains("Copy 1 entries to /home/ana:"), "{text}");
    assert!(
        matches!(home.overlay(), Some(Overlay::Copy { items, .. }) if items.len() == 1 && items[0].name == "x.bin")
    );
    nothing(
        &click(&mut home, &mut look, (140, 30), "Esc cancel"),
        "click cancels the copy",
    );
    command(&mut home, &mut look, "config");
    for _ in 0..4 {
        home.key(key(KeyCode::Down), &mut look);
    }
    home.key(key(KeyCode::Right), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    let text = draw(&home, &look, 140, 30);
    assert!(
        text.contains("2.0 KiB (2048 B)") && text.contains("/srv/x.bin") && text.contains("file"),
        "{text}"
    );
    nothing(
        &home.key(key(KeyCode::Char('z')), &mut look),
        "any key closes",
    );
    assert!(home.overlay().is_none());
}

/// Enter on a file with nothing on the other panel copies nothing and says why.
#[test]
fn enters_files_without_a_target() {
    use super::explorer::{Entry, Kind};
    let mut look = look();
    let mut home = loaded(&mut look, None);
    home.key(key(KeyCode::Tab), &mut look);
    home.reply(
        Reply::Homed {
            side: Side::Left,
            outcome: Ok("/home/ana".to_owned()),
        },
        &mut look,
    );
    let file = Entry {
        name: "z".to_owned(),
        kind: Kind::File,
        size: 1,
        modified: None,
    };
    home.reply(
        Reply::Listed {
            side: Side::Left,
            path: "/home/ana".to_owned(),
            outcome: Ok(vec![file]),
        },
        &mut look,
    );
    home.key(key(KeyCode::Down), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert!(home.overlay().is_none(), "nowhere to copy to");
    assert!(home.notice().is_some_and(|notice| notice.error));
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

    /// Disabling capture emits a terminal effect, keeps the setting and ignores queued clicks.
    #[test]
    fn disables_mouse_in_settings() {
        use crate::ui::mouse::MouseKind;

        let mut setup = kept("https://files.example.com");
        setup.signed_in = None;
        let click = Event::Mouse {
            kind: MouseKind::Left,
            column: 1,
            row: 1,
            at: std::time::Instant::now(),
        };
        nothing(
            &App::new(setup).handle(click.clone()),
            "on the server screen",
        );

        let mut app = App::new(kept("https://files.example.com"));
        run(&mut app, "config");
        for _ in 0..8 {
            app.handle(press(KeyCode::Down));
        }
        assert!(matches!(
            app.handle(press(KeyCode::Enter)).as_slice(),
            [Effect::Mouse(false), Effect::SaveSettings(settings)] if !settings.mouse
        ));
        nothing(&app.handle(click), "disabled mouse");
        assert!(matches!(
            app.handle(press(KeyCode::Enter)).as_slice(),
            [Effect::Mouse(true), Effect::SaveSettings(settings)] if settings.mouse
        ));
    }

    /// Timed clicks open the site, the wheel moves the targeted explorer, and a file opens its Enter action.
    #[test]
    fn opens_rows_with_the_mouse() {
        use std::time::{Duration, Instant};

        use crate::ui::explorer::{Entry, Kind};
        use crate::ui::mouse::MouseKind;

        let mut app = App::new(kept("https://files.example.com"));
        app.handle(Event::Replied(Reply::Workspaces(Ok(vec![workspace(
            1, "Personal", "personal",
        )]))));
        app.handle(Event::Replied(Reply::Tree {
            workspace: id(1),
            outcome: Ok((Vec::new(), vec![site(22, "home-nas", None, false)])),
        }));
        let at = Instant::now();
        let mouse = |label, app: &App, kind, elapsed| {
            let point = pointer(&text(app), label);
            Event::Mouse {
                kind,
                column: point.x,
                row: point.y,
                at: at + Duration::from_millis(elapsed),
            }
        };
        let first = mouse("home-nas", &app, MouseKind::Left, 0);
        nothing(&app.handle(first), "single click");
        let late = mouse("home-nas", &app, MouseKind::Left, 500);
        nothing(&app.handle(late), "past the double-click interval");
        let second = mouse("home-nas", &app, MouseKind::Left, 550);
        assert!(matches!(
            app.handle(second).as_slice(),
            [Effect::Call(
                _,
                Call::Files {
                    side: Side::Right,
                    op: Op::Home,
                    ..
                }
            )]
        ));
        app.handle(Event::Replied(Reply::Homed {
            side: Side::Right,
            outcome: Ok("/".to_owned()),
        }));
        app.handle(Event::Replied(Reply::Listed {
            side: Side::Right,
            path: "/".to_owned(),
            outcome: Ok(vec![
                Entry {
                    name: "Documents".to_owned(),
                    kind: Kind::Folder,
                    size: 0,
                    modified: None,
                },
                Entry {
                    name: "notes.txt".to_owned(),
                    kind: Kind::File,
                    size: 10,
                    modified: None,
                },
            ]),
        }));
        app.handle(press(KeyCode::Tab));
        let scroll = mouse("Documents", &app, MouseKind::Down, 600);
        nothing(&app.handle(scroll), "wheel only moves the cursor");
        assert_eq!(home(&app).focus(), Focus::Right);
        assert_eq!(
            home(&app).panel(Side::Right).selection()[0].name,
            "notes.txt"
        );
        let scroll = mouse("Documents", &app, MouseKind::Up, 610);
        nothing(&app.handle(scroll), "wheel up selects the folder");

        run(&mut app, "config");
        for _ in 0..4 {
            app.handle(press(KeyCode::Down));
        }
        app.handle(press(KeyCode::Enter));
        app.handle(press(KeyCode::Esc));

        let single = mouse("notes.txt", &app, MouseKind::Left, 650);
        nothing(&app.handle(single), "single file click");
        let double = mouse("notes.txt", &app, MouseKind::Left, 700);
        nothing(&app.handle(double), "details are local state");
        assert!(matches!(
            home(&app).overlay(),
            Some(Overlay::Details { .. })
        ));
        app.handle(press(KeyCode::Esc));

        let context = mouse("notes.txt", &app, MouseKind::Right, 710);
        nothing(&app.handle(context), "opens the context menu");
        let copy = mouse("Copy the path", &app, MouseKind::Left, 720);
        assert_eq!(
            app.handle(copy),
            vec![Effect::Copy("/notes.txt".to_owned())]
        );
        assert!(text(&app).contains("The path was copied."));
        assert_eq!(
            app.handle(press(KeyCode::Char('y'))),
            vec![Effect::Copy("/notes.txt".to_owned())]
        );
        nothing(&app.handle(press(KeyCode::Char('i'))), "details");
        assert!(matches!(
            home(&app).overlay(),
            Some(Overlay::Details { .. })
        ));
        app.handle(press(KeyCode::Esc));

        app.handle(press(KeyCode::Delete));
        let cancel = mouse("n cancel", &app, MouseKind::Left, 750);
        nothing(&app.handle(cancel), "cancelling never deletes");
        app.handle(press(KeyCode::Delete));
        let erase = mouse("y delete", &app, MouseKind::Left, 800);
        assert!(matches!(
            app.handle(erase).as_slice(),
            [Effect::Call(
                _,
                Call::Files {
                    op: Op::Delete(_),
                    ..
                }
            )]
        ));
        let first = mouse("Documents", &app, MouseKind::Left, 850);
        nothing(&app.handle(first), "single folder click");
        let double = mouse("Documents", &app, MouseKind::Left, 900);
        assert!(matches!(
            app.handle(double).as_slice(),
            [Effect::Call(_, Call::Files { op: Op::List(path), .. })] if path == "/Documents"
        ));
    }

    /// A slow listing blocks its panel, ages on ticks, and discards cancelled replies even after retrying the same path.
    #[test]
    fn blocks_and_cancels_a_slow_listing() {
        use std::time::Instant;

        use crate::ui::explorer::{Entry, Kind};
        use crate::ui::mouse::MouseKind;

        let mut app = App::new(kept("https://files.example.com"));
        app.handle(Event::Replied(Reply::Workspaces(Ok(vec![workspace(
            1, "Personal", "personal",
        )]))));
        app.handle(Event::Replied(Reply::Tree {
            workspace: id(1),
            outcome: Ok((Vec::new(), vec![site(22, "home-nas", None, false)])),
        }));
        app.handle(press(KeyCode::Enter));
        let entry = |name: &str, kind| Entry {
            name: name.to_owned(),
            kind,
            size: 1,
            modified: None,
        };
        for (side, path, entries) in [
            (Side::Left, "/local", vec![entry("local.txt", Kind::File)]),
            (
                Side::Right,
                "/",
                vec![entry("docs", Kind::Folder), entry("remote.txt", Kind::File)],
            ),
        ] {
            app.handle(Event::Replied(Reply::Homed {
                side,
                outcome: Ok(path.to_owned()),
            }));
            app.handle(Event::Replied(Reply::Listed {
                side,
                path: path.to_owned(),
                outcome: Ok(entries),
            }));
        }
        assert_eq!(home(&app).focus(), Focus::Right);
        app.handle(press(KeyCode::Enter));
        let side = Side::Right;
        let cancelled = app.listing_ticket(side).expect("pending listing");
        let first = text(&app);
        assert!(first.contains("⠋ Loading /docs…") && !first.contains("Esc to cancel"));
        app.handle(Event::Tick);
        assert!(text(&app).contains("⠙ Loading /docs…"));
        for _ in 1..24 {
            app.handle(Event::Tick);
        }
        assert!(!text(&app).contains("Esc to cancel"));
        app.handle(Event::Tick);
        assert!(text(&app).contains("Esc to cancel"));
        for _ in 0..10 {
            app.handle(Event::Tick);
        }
        assert!(text(&app).contains("⠴ Loading /docs…"));

        for code in [KeyCode::Down, KeyCode::Enter, KeyCode::F(5), KeyCode::F(9)] {
            nothing(&app.handle(press(code)), "");
        }
        nothing(&app.handle(Event::Key(ctrl('c'))), "");
        nothing(&app.handle(Event::Paste("ignored".to_owned())), "");
        let remote = pointer(&text(&app), "remote.txt");
        let at = Instant::now();
        for kind in [
            MouseKind::Left,
            MouseKind::Left,
            MouseKind::Right,
            MouseKind::Down,
        ] {
            nothing(
                &app.handle(Event::Mouse {
                    kind,
                    column: remote.x,
                    row: remote.y,
                    at,
                }),
                "",
            );
        }
        assert!(home(&app).overlay().is_none());
        assert_eq!(home(&app).panel(side).selection()[0].name, "docs");

        nothing(&app.handle(press(KeyCode::BackTab)), "");
        assert_eq!(home(&app).focus(), Focus::Left);
        nothing(&app.handle(press(KeyCode::Down)), "");
        assert_eq!(
            home(&app).panel(Side::Left).selection()[0].name,
            "local.txt"
        );
        nothing(&app.handle(press(KeyCode::Tab)), "");
        assert_eq!(home(&app).focus(), Focus::Right);
        assert!(text(&app).contains("Esc to cancel"));

        let reply = |ticket, outcome| Event::ListingReply {
            side,
            ticket,
            reply: Reply::Listed {
                side,
                path: "/docs".to_owned(),
                outcome,
            },
        };
        nothing(&app.handle(press(KeyCode::Esc)), "");
        let restored = text(&app);
        assert!(restored.contains("remote.txt") && !restored.contains("Loading /docs…"));
        assert_eq!(home(&app).panel(side).path(), "/");
        nothing(&app.handle(reply(cancelled.clone(), Ok(vec![]))), "");
        assert_eq!(home(&app).panel(side).path(), "/");

        app.handle(press(KeyCode::Enter));
        let current = app.listing_ticket(side).expect("new listing");
        assert!(text(&app).contains("⠋ Loading /docs…"));
        nothing(
            &app.handle(reply(cancelled, Err(refused("vault_locked")))),
            "",
        );
        assert!(home(&app).overlay().is_none());
        assert!(text(&app).contains("Loading /docs…"));
        nothing(
            &app.handle(reply(
                current.clone(),
                Ok(vec![entry("arrived.txt", Kind::File)]),
            )),
            "",
        );
        assert!(text(&app).contains("arrived.txt"));
        assert_eq!(home(&app).panel(side).path(), "/docs");

        assert_eq!(run(&mut app, "server"), vec![Effect::StopStream]);
        nothing(&app.handle(reply(current, Ok(vec![]))), "");
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
                .chain([Effect::Stream(session.clone())])
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
        app.handle(Event::LoggedOut(Err(refused("already_exists"))));
        assert_eq!(
            home(&app).notice().map(|n| n.text.as_str()),
            Some("Could not sign out: Something with that name already exists.")
        );
        app.handle(Event::LoggedOut(Ok(())));
        assert!(matches!(app.screen(), Screen::Server { .. }));
        assert!(text(&app).contains("Signed out of https://files.example.com."));
        for ignored in [
            Event::LoggedOut(Err(CallError::default())),
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
        assert_eq!(run(&mut app, "server"), vec![Effect::StopStream]);
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
        let mut ports = ports(
            dir.path(),
            tokens.clone(),
            Arc::new(|_: &str| true),
            Arc::new(Mutex::new(Vec::new())),
        );
        let capture = Arc::new(Mutex::new(Vec::new()));
        let recorded = Arc::clone(&capture);
        ports.mouse = Arc::new(move |enabled| recorded.lock().expect("capture").push(enabled));
        let mut config = Config::default();
        config.signed_in(&server.uri(), "ana@example.com");
        config.save(&ports.config_path).expect("config");

        let mut events = vec![
            (300, press(KeyCode::Char('/'))),
            (0, Event::Paste("config".to_owned())),
            (0, press(KeyCode::Enter)),
        ];
        events.extend((0..3).map(|_| (0, press(KeyCode::Down))));
        events.push((0, press(KeyCode::Right)));
        events.extend((0..5).map(|_| (0, press(KeyCode::Down))));
        events.extend([(100, press(KeyCode::Right)), (0, press(KeyCode::Esc))]);
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

        assert_eq!(*capture.lock().expect("capture"), [false]);
        assert_eq!(app.look().messages.language(), Language::Spanish);
        assert!(matches!(app.screen(), Screen::Server { .. }));
        assert_eq!(tokens.get(&server.uri()).expect("tokens"), None);
        let config = Config::load(&ports.config_path).expect("config");
        assert!(!config.settings.mouse);
        assert_eq!(
            (config.settings.colors, config.current),
            (Colors::Terminal, None)
        );
    }

    /// The loop copies a file of this computer to a site, lists the site again once it is there, and cancels an upload that is still
    /// running, which stops moving its bytes.
    #[tokio::test]
    async fn copies_through_the_loop() {
        use wiremock::matchers::{path_regex, query_param};
        let server = MockServer::start().await;
        let ws = id(1).to_string();
        let base = format!("/api/v1/workspaces/{ws}/sites/{}", id(20));
        let entry = serde_json::json!({"name": "a.txt", "path": "/srv/a.txt", "type": "file", "size": 1, "mode": "", "modifiedAt": "2026-01-01T00:00:00Z", "modifiedPrecision": "second"});
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
                format!("/api/v1/workspaces/{ws}/collections"),
                serde_json::json!([]),
            ),
            (
                "GET",
                format!("/api/v1/workspaces/{ws}/sites"),
                serde_json::json!([site(20, "acme", None, false)]),
            ),
            (
                "GET",
                "/api/v1/vault".to_owned(),
                serde_json::json!({"initialized": true, "unlocked": true}),
            ),
            (
                "GET",
                format!("{base}/files/home"),
                serde_json::json!({"path": "/srv"}),
            ),
            ("GET", format!("{base}/files"), serde_json::json!([])),
        ];
        for (verb, route, body) in answers {
            Mock::given(method(verb))
                .and(path(route))
                .respond_with(ResponseTemplate::new(200).set_body_json(body))
                .mount(&server)
                .await;
        }
        let uploads = format!("{base}/files/content");
        Mock::given(method("PUT"))
            .and(path(uploads.clone()))
            .and(query_param("path", "/srv/a.txt"))
            .respond_with(ResponseTemplate::new(201).set_body_json(&entry))
            .mount(&server)
            .await;
        Mock::given(method("PUT"))
            .and(path(uploads))
            .respond_with(
                ResponseTemplate::new(201)
                    .set_body_json(&entry)
                    .set_delay(Duration::from_secs(20)),
            )
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path_regex("^/api/v1/transfers/.*/cancel$"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;

        let dir = tempfile::tempdir().expect("folder");
        let files = tempfile::tempdir().expect("files");
        std::fs::write(files.path().join("a.txt"), "a").expect("a file");
        std::fs::write(files.path().join("b.txt"), "b").expect("a file");
        let tokens: Arc<dyn TokenStore> = Arc::from(secrets::open(
            dir.path(),
            Ok(keyring_core::mock::Store::new().expect("mock")),
        ));
        let ports = ports(
            dir.path(),
            tokens,
            Arc::new(|_: &str| true),
            Arc::new(Mutex::new(Vec::new())),
        );
        let folder = files.path().to_string_lossy().into_owned();
        let mut events = vec![
            (
                800,
                Event::Replied(Reply::Homed {
                    side: Side::Left,
                    outcome: Ok(folder),
                }),
            ),
            (600, press(KeyCode::Enter)),
        ];
        events.extend([
            (600, press(KeyCode::Tab)),
            (0, press(KeyCode::Tab)),
            (0, press(KeyCode::Down)),
            (0, press(KeyCode::Char('c'))),
            (0, press(KeyCode::Enter)),
        ]);
        events.extend([
            (800, press(KeyCode::Esc)),
            (0, press(KeyCode::Down)),
            (0, press(KeyCode::Char('c'))),
            (0, press(KeyCode::Enter)),
        ]);
        events.extend([
            (500, Event::Key(ctrl('e'))),
            (0, press(KeyCode::Char('x'))),
            (0, press(KeyCode::Char('y'))),
            (600, Event::Key(ctrl('c'))),
        ]);
        let mut app = App::new(kept(&server.uri()));
        let mut terminal = Terminal::new(TestBackend::new(140, 30)).expect("backend");
        tokio::time::timeout(
            Duration::from_secs(12),
            runtime::run(&mut app, &ports, paced(events), &mut draw_on(&mut terminal)),
        )
        .await
        .expect("the loop ended before the slow upload")
        .expect("drawing");
        let rows = home(&app).queue().rows().to_vec();
        assert_eq!(rows.len(), 2, "{rows:?}");
        let received = server.received_requests().await.expect("requests");
        assert!(
            received
                .iter()
                .any(|request| request.url.path().ends_with("/cancel")),
            "the cancel reached the server"
        );
    }
}

/// The real connector schemas, shared with the web form tests.
fn site_catalog() -> Vec<transferly_client::models::Connector> {
    serde_json::from_str(include_str!("../../../frontend/src/test/connectors.json"))
        .expect("connector catalog")
}

/// The table routes search, navigation, clicking, opening and deletion through the existing explorer and tree.
#[test]
fn manages_sites_from_the_table() {
    use crate::api::SiteAction;

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 34);
    home.key(key(KeyCode::Tab), &mut look);
    assert_eq!(home.focus(), Focus::Left);
    command(&mut home, &mut look, "sites");
    let text = draw(&home, &look, size.0, size.1);
    assert!(text.contains("Sites · Personal (3)"));
    assert!(text.contains("Clients / Acme / acme-sftp"));
    assert!(text.contains("home-nas.example.com"));
    assert!(!text.contains("Last used"));

    click(&mut home, &mut look, size, "Search:");
    home.paste("BETA-FTP");
    home.key(key(KeyCode::Enter), &mut look);
    let text = draw(&home, &look, size.0, size.1);
    assert!(text.contains("Sites · Personal (1)"));
    assert!(!text.contains("home-nas"));
    home.key(key(KeyCode::Char('D')), &mut look);
    assert!(
        draw(&home, &look, size.0, size.1).contains("Delete beta-ftp and its stored credentials?")
    );
    click(&mut home, &mut look, size, "Esc cancel");
    assert!(!draw(&home, &look, size.0, size.1).contains("Delete beta-ftp"));
    home.key(key(KeyCode::Char('D')), &mut look);
    assert_eq!(
        click(&mut home, &mut look, size, "Enter delete"),
        vec![Ask::Call(Call::Site(id(1), SiteAction::Delete(id(21))))],
    );
    nothing(&home.key(key(KeyCode::Enter), &mut look), "");
    assert_eq!(
        home.reply(Reply::Site(id(1), Ok(())), &mut look),
        vec![Ask::Call(Call::Tree(id(1)))],
    );
    assert!(draw(&home, &look, size.0, size.1).contains("No sites match."));
    home.key(key(KeyCode::Char('/')), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    click(&mut home, &mut look, size, "home-nas.example.com");
    assert!(matches!(
        home.key(key(KeyCode::Enter), &mut look).as_slice(),
        [Ask::Call(Call::Files { site, side: Side::Right, .. })] if *site == id(22)
    ));

    command(&mut home, &mut look, "sites");
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::Char('M')), &mut look);
    click(&mut home, &mut look, size, "Clients / Acme");
    assert_eq!(
        click(&mut home, &mut look, size, "Enter move"),
        vec![Ask::Call(Call::Site(
            id(1),
            SiteAction::Move(id(22), serde_json::json!({"collectionId": id(11)}))
        ))],
    );
    home.reply(Reply::Site(id(1), Err(refused("forbidden"))), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    assert_eq!(home.focus(), Focus::Right);
}

/// A draft preserves typed secrets across a vault unlock, focuses a refused field and tests again after trusting the host.
#[test]
fn fills_tests_and_saves_a_site_draft() {
    use crate::api::SiteAction;

    let mut look = look();
    let mut home = loaded(&mut look, Some((true, false)));
    let size = (132, 42);
    home.key(key(KeyCode::Char('S')), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Char('N')), &mut look),
        vec![Ask::Call(Call::Connectors)]
    );
    assert!(draw(&home, &look, size.0, size.1).contains("Loading connector schemas"));
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("New site · SFTP"));
    nothing(&home.key(key(KeyCode::Enter), &mut look), "");
    assert!(draw(&home, &look, size.0, size.1).contains("Fill in this field."));
    click(&mut home, &mut look, size, " Name ");
    home.paste("Example");
    click(&mut home, &mut look, size, " Host ");
    home.paste("draft.example.net");
    click(&mut home, &mut look, size, " Username ");
    home.paste("deploy");
    click(&mut home, &mut look, size, " Password ");
    home.paste("test password");
    assert!(!draw(&home, &look, size.0, size.1).contains("test password"));
    nothing(&home.key(ctrl('t'), &mut look), "");
    assert!(matches!(home.overlay(), Some(Overlay::Vault { .. })));
    home.paste("master password");
    assert_eq!(
        home.key(key(KeyCode::Enter), &mut look),
        vec![Ask::Call(Call::Unlock("master password".to_owned()))]
    );
    let test = home.reply(Reply::Unlocked(Ok(())), &mut look);
    let [Ask::Call(Call::Site(workspace, SiteAction::Test(body)))] = test.as_slice() else {
        panic!("expected draft test");
    };
    assert_eq!(*workspace, id(1));
    assert_eq!(body["site"]["config"]["port"], "22");
    assert_eq!(body["site"]["config"]["host"], "draft.example.net");
    assert!(body["site"]["config"].get("password").is_none());
    assert_eq!(body["site"]["secrets"]["password"], "test password");

    let mut issue = refused("invalid_config");
    issue.params.insert("field".to_owned(), "host".into());
    issue.params.insert("reason".to_owned(), "host".into());
    home.reply(Reply::Site(id(1), Err(issue)), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("Enter a host name or address"));
    assert_eq!(home.key(ctrl('t'), &mut look), test);
    let mut issue = refused("host_key_unknown");
    issue
        .params
        .insert("keyType".to_owned(), "ssh-ed25519".into());
    issue
        .params
        .insert("fingerprint".to_owned(), "SHA256:draft".into());
    home.reply(Reply::Site(id(1), Err(issue)), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("SHA256:draft"));
    assert!(matches!(
        home.key(key(KeyCode::Enter), &mut look).as_slice(),
        [Ask::Call(Call::TrustDraft(workspace, key))] if *workspace == id(1) && key.host == "draft.example.net" && key.port == 22
    ));
    assert_eq!(home.reply(Reply::DraftTrusted(Ok(())), &mut look), test);
    home.reply(Reply::Site(id(1), Ok(())), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("Connection succeeded."));
    assert!(matches!(
        home.key(key(KeyCode::Enter), &mut look).as_slice(),
        [Ask::Call(Call::Site(_, SiteAction::Create(body)))] if body["name"] == "Example"
    ));
}

/// Editing keeps stored secrets, while a duplicate explicitly refers to its source.
#[test]
fn edits_and_duplicates_a_site() {
    use crate::api::SiteAction;

    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('E')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    assert!(draw(&home, &look, 132, 42).contains("Stored in your vault"));
    assert!(matches!(
        home.key(key(KeyCode::Enter), &mut look).as_slice(),
        [Ask::Call(Call::Site(_, SiteAction::Edit(site, body)))]
            if *site == id(20) && body["name"] == "acme-sftp" && body.get("secrets").is_none()
    ));
    home.reply(Reply::Site(id(1), Ok(())), &mut look);
    home.key(key(KeyCode::Char('C')), &mut look);
    assert!(draw(&home, &look, 132, 42).contains("Duplicate site · SFTP"));
    assert!(matches!(
        home.key(key(KeyCode::Enter), &mut look).as_slice(),
        [Ask::Call(Call::Site(_, SiteAction::Create(body)))] if body["secretsFrom"] == id(20).to_string()
    ));
}

/// Connector choices recompute dependent defaults and show the right authentication fields.
#[test]
fn site_form_follows_connector_choices() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 42);
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('N')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);

    home.key(key(KeyCode::Left), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("New site · FTP / FTPS"));
    click(&mut home, &mut look, size, " Encryption ");
    home.key(key(KeyCode::Right), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("990"));
    click(&mut home, &mut look, size, " Kind ──");
    home.key(key(KeyCode::Right), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("New site · SFTP"));
    click(&mut home, &mut look, size, " Name ");
    home.paste("Keys");
    click(&mut home, &mut look, size, " Host ");
    home.paste("files.example.net");
    click(&mut home, &mut look, size, " Logon type ");
    home.key(key(KeyCode::Left), &mut look);
    let screen = draw(&home, &look, size.0, size.1);
    assert!(screen.contains("anonymous"));
    assert!(!screen.contains(" Password "));
    home.key(key(KeyCode::Left), &mut look);
    home.key(key(KeyCode::Tab), &mut look);
    home.paste("deploy");
    home.key(key(KeyCode::Tab), &mut look);
    home.paste("BEGIN KEY\r\nexample line\nEND KEY\n");
    assert!(draw(&home, &look, size.0, size.1).contains("Paste the contents"));
    home.key(key(KeyCode::BackTab), &mut look);

    let asks = home.key(ctrl('t'), &mut look);
    assert!(matches!(
        asks.as_slice(),
        [Ask::Call(Call::Site(_, crate::api::SiteAction::Test(body)))]
            if body["site"]["secrets"]["private_key"] == "BEGIN KEY\nexample line\nEND KEY\n"
                && body["site"]["config"]["username"] == "deploy"
    ));
    home.key(key(KeyCode::Esc), &mut look);
    home.reply(Reply::Site(id(1), Ok(())), &mut look);
    assert!(!draw(&home, &look, size.0, size.1).contains("New site · SFTP"));
    home.key(key(KeyCode::Esc), &mut look);
    assert_eq!(home.focus(), Focus::Sites);
}

/// A draft trusts the peer actually reported by the server, even when there is a jump host.
#[test]
fn site_test_trusts_the_reported_peer() {
    use crate::api::TrustMode;

    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('E')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    let draft = home.key(ctrl('t'), &mut look);
    let mut refused_key = refused("host_key_changed");
    refused_key.params.extend([
        ("host".to_owned(), "jump.example.net".into()),
        ("port".to_owned(), 2222.into()),
        ("keyType".to_owned(), "ssh-ed25519".into()),
        ("fingerprint".to_owned(), "SHA256:new".into()),
        ("trusted".to_owned(), serde_json::json!(["SHA256:old"])),
    ]);
    home.reply(Reply::Site(id(1), Err(refused_key.clone())), &mut look);
    assert!(draw(&home, &look, 132, 42).contains("jump.example.net:2222"));
    home.key(key(KeyCode::Esc), &mut look);
    assert_eq!(home.key(ctrl('t'), &mut look), draft);
    home.reply(Reply::Site(id(1), Err(refused_key)), &mut look);
    assert!(matches!(
        home.key(key(KeyCode::Char('a')), &mut look).as_slice(),
        [Ask::Call(Call::TrustDraft(_, key))]
            if key.host == "jump.example.net" && key.port == 2222 && key.mode == Some(TrustMode::Add)
    ));
    home.reply(Reply::DraftTrusted(Err(refused("forbidden"))), &mut look);
    assert!(draw(&home, &look, 132, 42).contains("Your role in this workspace"));
}

/// The server can require vault unlock after the draft has been started.
#[test]
fn site_form_handles_an_expired_vault_session() {
    use crate::api::SiteAction;

    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('C')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    let save = home.key(key(KeyCode::Enter), &mut look);
    assert!(matches!(
        save.as_slice(),
        [Ask::Call(Call::Site(_, SiteAction::Create(_)))]
    ));
    home.reply(Reply::Site(id(1), Err(refused("vault_locked"))), &mut look);
    assert!(matches!(home.overlay(), Some(Overlay::Vault { .. })));
    home.key(key(KeyCode::Esc), &mut look);
    assert!(home.overlay().is_none());
    nothing(&home.key(key(KeyCode::Enter), &mut look), "");
    home.paste("vault password");
    home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(home.reply(Reply::Unlocked(Ok(())), &mut look), save);
    assert_eq!(
        home.reply(Reply::Site(id(1), Ok(())), &mut look),
        vec![Ask::Call(Call::Tree(id(1)))],
    );
    home.key(key(KeyCode::Esc), &mut look);
    assert_eq!(home.focus(), Focus::Sites);
}

/// Exercises search input, keyboard and mouse selection, and deletion of an open site.
#[test]
fn navigates_the_site_table_and_deletes_an_open_site() {
    use ratatui::layout::{Position, Rect};

    use super::mouse::MouseKind;
    use crate::api::SiteAction;

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 42);
    let area = Rect::new(0, 0, size.0, size.1);
    home.key(key(KeyCode::Char('S')), &mut look);
    nothing(&click(&mut home, &mut look, size, "/  Search"), "");
    nothing(&home.key(key(KeyCode::Char('z')), &mut look), "");
    nothing(&home.key(key(KeyCode::F(1)), &mut look), "");
    assert!(draw(&home, &look, size.0, size.1).contains("Search: z"));
    home.key(key(KeyCode::Esc), &mut look);
    for code in [
        KeyCode::End,
        KeyCode::Home,
        KeyCode::Down,
        KeyCode::Up,
        KeyCode::F(1),
    ] {
        nothing(&home.key(key(code), &mut look), "");
    }
    assert_eq!(
        home.mouse(area, Position::new(0, 0), MouseKind::Left, false, &mut look),
        (Vec::new(), false),
    );

    let screen = draw(&home, &look, size.0, size.1);
    let point = pointer(&screen, "home-nas.example.com");
    nothing(
        &home.mouse(area, point, MouseKind::Down, false, &mut look).0,
        "",
    );
    assert!(matches!(
        home.mouse(area, point, MouseKind::Left, true, &mut look).0.as_slice(),
        [Ask::Call(Call::Files { site, side: Side::Right, .. })] if *site == id(22)
    ));

    command(&mut home, &mut look, "sites");
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::Char('M')), &mut look);
    for code in [KeyCode::Down, KeyCode::Up, KeyCode::F(1), KeyCode::Esc] {
        nothing(&home.key(key(code), &mut look), "");
    }
    home.key(key(KeyCode::Char('D')), &mut look);
    nothing(&home.key(key(KeyCode::Char('n')), &mut look), "");
    assert!(!draw(&home, &look, size.0, size.1).contains("Delete home-nas and"));
    home.key(key(KeyCode::Char('D')), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Char('y')), &mut look),
        vec![Ask::Call(Call::Site(id(1), SiteAction::Delete(id(22))))],
    );
    nothing(&click(&mut home, &mut look, size, "Esc cancel"), "");
    nothing(&home.reply(Reply::Site(id(2), Ok(())), &mut look), "");
    assert_eq!(
        home.reply(Reply::Site(id(1), Ok(())), &mut look),
        vec![Ask::Call(Call::Tree(id(1)))],
    );
    assert!(home.panel(Side::Right).source().is_none());
    home.key(key(KeyCode::Char('D')), &mut look);
    home.reply(
        Reply::Tree {
            workspace: id(1),
            outcome: Ok((Vec::new(), Vec::new())),
        },
        &mut look,
    );
    let screen = draw(&home, &look, size.0, size.1);
    assert!(screen.contains("No sites match."));
    assert!(!screen.contains("stored credentials?"));
}

/// Checks catalog failure, fallback to FTP, and replies after a draft or table was dismissed.
#[test]
fn handles_missing_and_delayed_site_catalogs() {
    use ratatui::layout::Rect;

    use super::mouse::MouseKind;

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 50);
    nothing(
        &home.reply(Reply::Connectors(Ok(site_catalog())), &mut look),
        "",
    );
    nothing(&home.reply(Reply::Site(id(1), Ok(())), &mut look), "");
    nothing(&home.reply(Reply::DraftTrusted(Ok(())), &mut look), "");
    home.key(key(KeyCode::Char('S')), &mut look);
    nothing(&home.reply(Reply::DraftTrusted(Ok(())), &mut look), "");

    assert_eq!(
        click(&mut home, &mut look, size, "N  New"),
        vec![Ask::Call(Call::Connectors)],
    );
    let point = pointer(
        &draw(&home, &look, size.0, size.1),
        "Loading connector schemas",
    );
    let area = Rect::new(0, 0, size.0, size.1);
    for kind in [MouseKind::Down, MouseKind::Left] {
        nothing(&home.mouse(area, point, kind, false, &mut look).0, "");
    }
    nothing(&home.key(key(KeyCode::Tab), &mut look), "");
    nothing(
        &home.reply(Reply::Connectors(Err(refused("forbidden"))), &mut look),
        "",
    );
    assert!(
        draw(&home, &look, size.0, size.1)
            .contains("Your role in this workspace does not allow that.")
    );

    home.key(key(KeyCode::Char('N')), &mut look);
    nothing(
        &home.reply(Reply::Connectors(Err(refused("vault_locked"))), &mut look),
        "",
    );
    assert!(matches!(home.overlay(), Some(Overlay::Vault { .. })));
    nothing(&home.reply(Reply::Unlocked(Ok(())), &mut look), "");

    home.key(key(KeyCode::Char('N')), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    let mut catalog = site_catalog();
    catalog.retain(|connector| connector.r#type == "ftp");
    home.reply(Reply::Connectors(Ok(catalog)), &mut look);
    home.key(key(KeyCode::Char('E')), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("No schema is available"));
    home.key(key(KeyCode::Char('N')), &mut look);
    home.key(key(KeyCode::Right), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("New site · FTP / FTPS"));
    let screen = draw(&home, &look, size.0, 70);
    assert!(screen.contains("Charset / Character set"));
    assert!(screen.contains("Proxy / Proxy"));
}

/// Round-trips saved metadata and validates text, choice input and form footer clicks.
#[test]
fn edits_site_metadata_and_validates_the_speed_limit() {
    use crate::api::SiteAction;

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 80);
    let saved = serde_json::from_value(serde_json::json!({
        "id": id(20), "collectionId": id(11), "name": "Archive", "connector": "sftp",
        "config": {"host": "files.example.net"}, "speedLimitBps": 42, "remoteDir": "/archive",
        "color": "blue", "secrets": [], "createdAt": "2026-01-01T00:00:00Z",
        "updatedAt": "2026-01-01T00:00:00Z"
    }))
    .expect("saved site");
    home.reply(
        Reply::Tree {
            workspace: id(1),
            outcome: Ok((
                vec![collection(11, "Archive collection", None)],
                vec![saved],
            )),
        },
        &mut look,
    );
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('E')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    let screen = draw(&home, &look, size.0, size.1);
    assert!(screen.contains("‹ Archive collection ›"));
    assert!(screen.contains("‹ Blue ›"));
    assert!(screen.contains("/archive"));
    assert!(screen.contains("SSH jump host / Jump host"));
    nothing(&click(&mut home, &mut look, size, "Edit site · SFTP"), "");
    home.paste("ignored kind");
    home.key(key(KeyCode::Right), &mut look);
    click(&mut home, &mut look, size, " Collection ──");
    home.paste("ignored collection");
    home.key(key(KeyCode::Char('x')), &mut look);
    click(&mut home, &mut look, size, " Speed limit (bytes/s) ");
    home.key(ctrl('u'), &mut look);
    home.paste("-1");
    nothing(&click(&mut home, &mut look, size, "Enter save"), "");
    let message = "Enter a nonnegative whole number.";
    assert!(draw(&home, &look, size.0, size.1).contains(message));
    nothing(&click(&mut home, &mut look, size, message), "");
    home.key(ctrl('u'), &mut look);
    home.key(key(KeyCode::Char('9')), &mut look);
    let asks = click(&mut home, &mut look, size, "Ctrl+T test");
    assert!(matches!(
        asks.as_slice(),
        [Ask::Call(Call::Site(_, SiteAction::Test(body)))]
            if body["siteId"] == id(20).to_string()
                && body["site"]["speedLimitBps"] == 9
                && body["site"]["remoteDir"] == "/archive"
                && body["site"]["collectionId"] == id(11).to_string()
                && body["site"]["color"] == "blue"
    ));
}

/// OAuth credentials must be connected in the web app before the site can be saved.
#[test]
fn refuses_an_unconnected_site_account() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 42);
    let mut catalog = site_catalog();
    catalog.retain_mut(|connector| {
        connector.enabled = connector.r#type == "gdrive";
        connector.enabled
    });

    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('N')), &mut look);
    home.reply(Reply::Connectors(Ok(catalog)), &mut look);
    click(&mut home, &mut look, size, " Name ──");
    home.paste("Drive");
    assert!(draw(&home, &look, size.0, size.1).contains("Connect the account in the web app."));
    click(&mut home, &mut look, size, " Google account ");
    nothing(&home.key(key(KeyCode::Char('x')), &mut look), "");
    home.paste("ignored token");
    nothing(&home.key(key(KeyCode::Enter), &mut look), "");
    let screen = draw(&home, &look, size.0, size.1);
    assert!(screen.contains("Connect the account first."));
    assert!(!screen.contains("ignored token"));
}

/// A site action waits for a workspace and gives setup guidance when the vault is uninitialized.
#[test]
fn requires_a_workspace_and_vault_for_site_credentials() {
    let mut look = look();
    let (mut home, _) = Home::new(session(), "ana@example.com", false);
    let size = (132, 42);
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('N')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    click(&mut home, &mut look, size, " Name ──");
    home.paste("Example");
    nothing(&home.key(key(KeyCode::Enter), &mut look), "");

    let mut home = loaded(&mut look, Some((false, false)));
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('E')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    nothing(&home.key(ctrl('t'), &mut look), "");
    assert!(draw(&home, &look, size.0, size.1).contains("Set up your vault"));
    assert!(home.overlay().is_none());
    assert!(matches!(
        home.key(key(KeyCode::Enter), &mut look).as_slice(),
        [Ask::Call(Call::Site(_, crate::api::SiteAction::Edit(..)))]
    ));
    nothing(
        &home.reply(Reply::Site(id(1), Err(refused("vault_locked"))), &mut look),
        "",
    );
    assert!(draw(&home, &look, size.0, size.1).contains("Set up your vault"));
    assert!(home.overlay().is_none());
}

/// Validation replies focus visible settings and preserve a form-level error for other failures.
#[test]
fn shows_field_and_operation_errors_for_site_drafts() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    let size = (132, 42);
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('E')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    for (field, reason, message) in [
        (None, "required", "A setting of the site does not follow"),
        (
            Some("private_key"),
            "required",
            "A setting of the site does not follow",
        ),
        (Some("username"), "length", "Use at most 255 characters."),
        (Some("port"), "unrecognized", "Choose one of the options."),
    ] {
        assert!(matches!(
            home.key(ctrl('t'), &mut look).as_slice(),
            [Ask::Call(Call::Site(_, crate::api::SiteAction::Test(_)))]
        ));
        assert!(draw(&home, &look, size.0, size.1).contains("Waiting for the server"));
        let mut error = refused("invalid_config");
        if let Some(field) = field {
            error.params.insert("field".to_owned(), field.into());
        }
        error.params.insert("reason".to_owned(), reason.into());
        nothing(&home.reply(Reply::Site(id(1), Err(error)), &mut look), "");
        assert!(draw(&home, &look, size.0, size.1).contains(message));
    }
    look.speak("es");
    home.key(ctrl('t'), &mut look);
    home.reply(
        Reply::Site(id(1), Err(refused("site_name_taken"))),
        &mut look,
    );
    let text = draw(&home, &look, size.0, size.1);
    assert!(text.contains("Otro sitio de la misma colección"), "{text}");
    assert!(!text.contains("site_name_taken"), "{text}");
}

/// Jump-host tests require a reported peer and propagate authentication failure when trusting it.
#[test]
fn handles_incomplete_site_host_keys_and_expired_trust() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    let size = (132, 80);
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('E')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);

    let mut error = refused("host_key_unknown");
    home.key(key(KeyCode::Enter), &mut look);
    home.reply(Reply::Site(id(1), Err(error.clone())), &mut look);
    assert!(home.overlay().is_none());
    home.key(ctrl('t'), &mut look);
    home.reply(Reply::Site(id(1), Err(error.clone())), &mut look);
    assert!(home.overlay().is_none());

    click(&mut home, &mut look, size, " SSH jump host / Jump host ");
    home.paste("bastion.example.net");
    error.params.extend([
        ("keyType".to_owned(), "ssh-ed25519".into()),
        ("fingerprint".to_owned(), "SHA256:jump".into()),
    ]);
    home.key(ctrl('t'), &mut look);
    home.reply(Reply::Site(id(1), Err(error.clone())), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("Verify the host key in the web app"));

    error.params.extend([
        ("host".to_owned(), "bastion.example.net".into()),
        ("port".to_owned(), 2222.into()),
    ]);
    home.key(ctrl('t'), &mut look);
    home.reply(Reply::Site(id(1), Err(error)), &mut look);
    assert!(matches!(
        home.key(key(KeyCode::Enter), &mut look).as_slice(),
        [Ask::Call(Call::TrustDraft(_, key))] if key.host == "bastion.example.net" && key.port == 2222
    ));
    assert!(matches!(
        home.reply(
            Reply::DraftTrusted(Err(refused("unauthenticated"))),
            &mut look
        )
        .as_slice(),
        [Ask::Leave(Some(_))]
    ));
}

/// A legacy key refusal without a peer cannot be trusted when the target host is blank.
#[test]
fn ignores_a_site_key_with_no_target_host() {
    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 42);
    home.key(key(KeyCode::Char('S')), &mut look);
    home.key(key(KeyCode::Char('N')), &mut look);
    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    click(&mut home, &mut look, size, " Name ──");
    home.paste("New target");
    assert!(matches!(
        home.key(ctrl('t'), &mut look).as_slice(),
        [Ask::Call(Call::Site(_, crate::api::SiteAction::Test(_)))]
    ));
    let mut error = refused("host_key_unknown");
    error.params.extend([
        ("keyType".to_owned(), "ssh-ed25519".into()),
        ("fingerprint".to_owned(), "SHA256:target".into()),
    ]);
    nothing(&home.reply(Reply::Site(id(1), Err(error)), &mut look), "");
    assert!(home.overlay().is_none());
    assert!(draw(&home, &look, size.0, size.1).contains("The key of this server is not trusted"));
}

/// The rarer paths of the site table: lowercase keys, keys without a site, a late catalog error, pasting into the form, the wheel
/// while searching, clicks below the rows, a site without a collection and the choices of collections.
#[test]
fn walks_the_rare_paths_of_the_site_table() {
    use super::mouse::MouseKind;
    use ratatui::layout::{Position, Rect};

    let mut look = look();
    let mut home = loaded(&mut look, None);
    let size = (132, 60);
    home.key(key(KeyCode::Char('S')), &mut look);
    nothing(&home.key(ctrl('x'), &mut look), "");
    nothing(
        &home.reply(Reply::Connectors(Err(CallError::default())), &mut look),
        "",
    );
    home.key(key(KeyCode::Char('/')), &mut look);
    let bounds = Rect::new(0, 0, size.0, size.1);
    let (asks, _) = home.mouse(
        bounds,
        Position::new(40, 10),
        MouseKind::Down,
        false,
        &mut look,
    );
    nothing(&asks, "the wheel waits while searching");
    home.key(key(KeyCode::Esc), &mut look);
    home.paste("nowhere to go");
    let (asks, selected) = home.mouse(
        bounds,
        Position::new(40, 40),
        MouseKind::Left,
        false,
        &mut look,
    );
    nothing(&asks, "below the last row");
    let (asks, _) = home.mouse(
        bounds,
        Position::new(5, 4),
        MouseKind::Right,
        false,
        &mut look,
    );
    nothing(&asks, "a right click on the table");
    assert!(!selected);

    home.reply(Reply::Connectors(Ok(site_catalog())), &mut look);
    home.key(key(KeyCode::Home), &mut look);
    home.key(key(KeyCode::Char('m')), &mut look);
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::Char('e')), &mut look);
    assert!(draw(&home, &look, size.0, size.1).contains("‹ Top level ›"));
    home.key(key(KeyCode::Esc), &mut look);
    home.key(key(KeyCode::Char('n')), &mut look);
    click(&mut home, &mut look, size, " Name ──");
    home.paste("Pasted name");
    click(&mut home, &mut look, size, " Collection ──");
    home.key(key(KeyCode::Right), &mut look);
    click(&mut home, &mut look, size, " Color ──");
    home.key(key(KeyCode::Right), &mut look);
    let screen = draw(&home, &look, size.0, size.1);
    assert!(screen.contains("Pasted name"));
    assert!(!screen.contains("‹ None ›"));
    assert!(!screen.contains("‹ Top level ›"));
}

/// F3 requests the file under the local cursor, then opens a numbered searchable view.
#[test]
fn previews_local_text_and_ignores_stale_replies() {
    use ratatui::layout::{Position, Rect};
    use ratatui::style::Modifier;

    use super::mouse::MouseKind;

    let mut look = Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::Plain,
        Settings::default(),
    );
    let (mut home, _) = Home::new(session(), "ana@example.com", false);
    home.key(key(KeyCode::Tab), &mut look);
    list_preview_file(&mut home, &mut look, Side::Left, "notes.txt");
    nothing(
        &home.key(key(KeyCode::F(3)), &mut look),
        "on the parent row",
    );
    home.key(key(KeyCode::End), &mut look);
    assert_eq!(
        home.key(key(KeyCode::F(3)), &mut look),
        vec![Ask::Call(Call::Preview {
            request: 1,
            remote: None,
            path: std::path::Path::new("/preview")
                .join("notes.txt")
                .to_string_lossy()
                .into_owned(),
        })]
    );
    assert!(draw(&home, &look, 100, 12).contains("Loading preview"));
    nothing(&home.key(key(KeyCode::Down), &mut look), "");
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    nothing(
        &home.reply(Reply::Preview(1, Ok((vec![], false))), &mut look),
        "",
    );
    home.key(key(KeyCode::F(3)), &mut look);
    nothing(
        &home.reply(Reply::Preview(1, Err(refused("not_found"))), &mut look),
        "",
    );
    let bytes = format!("first\nmatch twice MATCH\n{}final\n", "middle\n".repeat(20));
    nothing(
        &home.reply(Reply::Preview(2, Ok((bytes.into_bytes(), true))), &mut look),
        "",
    );
    let screen = draw(&home, &look, 100, 12);
    assert!(screen.contains("notes.txt · Text · 42 B"));
    assert!(screen.contains("   1  first"));
    assert!(screen.contains("Showing the first 4 MiB"));

    for kind in [MouseKind::Left, MouseKind::Down, MouseKind::Up] {
        let (asks, selected) = home.mouse(
            Rect::new(0, 0, 100, 12),
            Position::new(2, 3),
            kind,
            false,
            &mut look,
        );
        nothing(&asks, "");
        assert!(!selected);
    }
    for code in [KeyCode::PageDown, KeyCode::PageUp, KeyCode::End] {
        nothing(&home.key(key(code), &mut look), "");
    }
    assert!(draw(&home, &look, 100, 12).contains("final"));
    for code in [
        KeyCode::Home,
        KeyCode::Right,
        KeyCode::Left,
        KeyCode::Char('n'),
        KeyCode::Char('/'),
    ] {
        nothing(&home.key(key(code), &mut look), "");
    }
    home.paste("match");
    nothing(&home.key(key(KeyCode::Left), &mut look), "");
    assert!(draw(&home, &look, 100, 12).contains("Enter Find"));
    nothing(&home.key(key(KeyCode::Enter), &mut look), "");
    assert!(draw(&home, &look, 100, 12).contains("match 1 of 2"));
    nothing(&home.key(key(KeyCode::Char('n')), &mut look), "");
    let screen = draw(&home, &look, 100, 12);
    assert!(screen.contains("match 2 of 2"));
    nothing(&home.key(key(KeyCode::Char('N')), &mut look), "");
    assert!(draw(&home, &look, 100, 12).contains("match 1 of 2"));
    nothing(&home.key(key(KeyCode::Char('N')), &mut look), "");
    let mut terminal = Terminal::new(TestBackend::new(100, 12)).expect("backend");
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .expect("draw");
    let point = pointer(&screen, "MATCH");
    let matched = &terminal.backend().buffer()[(point.x, point.y)];
    assert!(matched.modifier.contains(Modifier::REVERSED));
    nothing(&home.key(key(KeyCode::Char('/')), &mut look), "");
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    nothing(&home.key(key(KeyCode::Char('/')), &mut look), "");
    nothing(&home.key(ctrl('u'), &mut look), "");
    home.paste("absent");
    nothing(&home.key(key(KeyCode::Enter), &mut look), "");
    assert!(draw(&home, &look, 100, 12).contains("no matches"));
    nothing(&home.key(key(KeyCode::Char('q')), &mut look), "");
    assert!(!draw(&home, &look, 100, 20).contains("Showing the first"));
}

/// The right panel opens tables and web-only previews and explains failures.
#[test]
fn previews_remote_tables_and_web_only_files() {
    let mut look = look();
    let mut home = loaded(&mut look, Some((true, true)));
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    list_preview_file(&mut home, &mut look, Side::Right, "data.csv");
    home.key(key(KeyCode::End), &mut look);
    assert_eq!(
        home.key(key(KeyCode::F(3)), &mut look),
        vec![Ask::Call(Call::Preview {
            request: 1,
            remote: Some((id(1), id(22))),
            path: "/preview/data.csv".to_owned(),
        })]
    );
    let bytes = "Name;City;Amount\n\"A; B\";Bogotá;10\nCarla"
        .as_bytes()
        .to_vec();
    nothing(
        &home.reply(Reply::Preview(1, Ok((bytes, false))), &mut look),
        "",
    );
    home.paste("ignored");
    assert!(draw(&home, &look, 60, 16).contains("Na"));
    let screen = draw(&home, &look, 80, 12);
    assert!(screen.contains("Name"));
    assert!(screen.contains("City"));
    assert!(screen.contains("A; B"));
    nothing(&home.key(key(KeyCode::Right), &mut look), "");
    assert!(!draw(&home, &look, 80, 12).contains("Name"));
    for code in [KeyCode::Left, KeyCode::End, KeyCode::Esc] {
        nothing(&home.key(key(code), &mut look), "");
    }

    list_preview_file(&mut home, &mut look, Side::Right, "workbook.xlsx");
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::F(3)), &mut look);
    let workbook = include_bytes!("../../../testenv/preview-samples/office/workbook.xlsx");
    nothing(
        &home.reply(Reply::Preview(2, Ok((workbook.to_vec(), false))), &mut look),
        "",
    );
    let screen = draw(&home, &look, 100, 15);
    assert!(screen.contains("Sales by person"));
    assert!(screen.contains("4471.5"));
    assert!(screen.contains("Sales  Notes  Big"));
    for code in [KeyCode::Tab, KeyCode::Char(']')] {
        nothing(&home.key(key(code), &mut look), "");
    }
    assert!(draw(&home, &look, 100, 15).contains("Square"));
    assert!(draw(&home, &look, 12, 15).contains("Big"));
    nothing(
        &home.key(KeyEvent::new(KeyCode::Tab, KeyModifiers::SHIFT), &mut look),
        "",
    );
    assert!(draw(&home, &look, 100, 15).contains("Formulas show"));
    for code in [KeyCode::BackTab, KeyCode::Char('['), KeyCode::Esc] {
        nothing(&home.key(key(code), &mut look), "");
    }
    home.key(key(KeyCode::F(3)), &mut look);
    nothing(
        &home.reply(
            Reply::Preview(3, Ok((b"broken".to_vec(), false))),
            &mut look,
        ),
        "",
    );
    assert_eq!(
        home.notice().expect("error").text,
        "The workbook could not be read."
    );
    home.key(key(KeyCode::F(3)), &mut look);
    nothing(
        &home.reply(Reply::Preview(4, Err(refused("not_found"))), &mut look),
        "",
    );
    assert!(home.notice().expect("server error").error);

    list_preview_file(&mut home, &mut look, Side::Right, "report.pdf");
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::F(3)), &mut look);
    look.speak("es");
    nothing(
        &home.reply(Reply::Preview(5, Ok((b"%PDF-".to_vec(), false))), &mut look),
        "",
    );
    assert!(draw(&home, &look, 100, 12).contains("Pulsa w para abrir"));
    nothing(&home.key(key(KeyCode::Right), &mut look), "");
    home.key(key(KeyCode::Char('/')), &mut look);
    home.key(key(KeyCode::Char('x')), &mut look);
    home.key(key(KeyCode::Enter), &mut look);
    assert_eq!(
        home.key(key(KeyCode::Char('w')), &mut look),
        vec![Ask::Browse("https://files.example.com/".to_owned())]
    );
    nothing(&home.key(key(KeyCode::Esc), &mut look), "");
    list_preview_file(&mut home, &mut look, Side::Right, "empty.csv");
    home.key(key(KeyCode::End), &mut look);
    home.key(key(KeyCode::F(3)), &mut look);
    nothing(
        &home.reply(Reply::Preview(6, Ok((Vec::new(), false))), &mut look),
        "",
    );
    assert!(draw(&home, &look, 100, 12).contains("empty.csv · CSV"));
}

/// Searches every cell and scrolls to a match outside both axes of the viewer.
#[test]
fn searches_table_headers_and_offscreen_cells() {
    let mut look = look();
    let bytes = format!(
        "Name,widecolumn,Bogotá header\nfirst,Bogotá as well,none\n{}last,other,BOGOTÁ final",
        "filler,not here,not here\n".repeat(15)
    );
    let mut home = open_local_preview(&mut look, "cities.csv", bytes.as_bytes());
    find_preview(&mut home, &mut look, "bogotá");

    let screen = draw(&home, &look, 48, 9);
    assert!(screen.contains("match 1 of 3"));
    assert!(screen.contains("Bogotá header"));
    assert!(!screen.contains("Name"));
    let mut terminal = Terminal::new(TestBackend::new(48, 9)).expect("backend");
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .expect("draw");
    let header = pointer(&screen, "Bogotá header");
    assert_eq!(
        terminal.backend().buffer()[(header.x, header.y)].bg,
        look.palette.accent
    );

    nothing(&home.key(key(KeyCode::Char('n')), &mut look), "");
    let screen = draw(&home, &look, 48, 9);
    assert!(screen.contains("match 2 of 3"));
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .expect("draw");
    let other = pointer(&screen, "Bogotá header");
    let cell = &terminal.backend().buffer()[(other.x, other.y)];
    assert_eq!(cell.fg, look.palette.accent_text);
    assert!(cell.modifier.contains(ratatui::style::Modifier::UNDERLINED));

    nothing(&home.key(key(KeyCode::Char('n')), &mut look), "");
    let screen = draw(&home, &look, 48, 9);
    assert!(screen.contains("match 3 of 3"));
    assert!(screen.contains("BOGOTÁ final"));
    assert!(!screen.contains("Bogotá as well"));
    nothing(&home.key(key(KeyCode::Char('n')), &mut look), "");
    assert!(draw(&home, &look, 48, 9).contains("match 1 of 3"));
    nothing(&home.key(key(KeyCode::Char('N')), &mut look), "");
    assert!(draw(&home, &look, 48, 9).contains("match 3 of 3"));

    let mut home = open_local_preview(&mut look, "cities.tsv", b"Name\tCity\nA\tLima");
    find_preview(&mut home, &mut look, "LIMA");
    assert!(draw(&home, &look, 48, 9).contains("match 1 of 1"));
    find_preview(&mut home, &mut look, "");
    assert!(!draw(&home, &look, 48, 9).contains("match 1 of 1"));
}

/// Searches the active sheet before falling back to the other sheets.
#[test]
fn searches_workbook_sheets_with_localized_status() {
    let mut look = Look::new(
        Language::Spanish,
        DEFAULT_THEME,
        ColorMode::Plain,
        Settings::default(),
    );
    let workbook = include_bytes!("../../../testenv/preview-samples/office/workbook.xlsx");
    let mut home = open_local_preview(&mut look, "workbook.xlsx", workbook);
    find_preview(&mut home, &mut look, "SQUARE");
    let screen = draw(&home, &look, 100, 15);
    assert!(screen.contains("Square"));
    assert!(screen.contains("coincidencia 1 de 1"));

    find_preview(&mut home, &mut look, "VALUE");
    let screen = draw(&home, &look, 100, 15);
    assert!(screen.contains("Value"));
    assert!(screen.contains("coincidencia 1 de 1"));

    find_preview(&mut home, &mut look, "FORMULA");
    let screen = draw(&home, &look, 100, 15);
    assert!(screen.contains("Formulas show"));
    assert!(screen.contains("coincidencia 1 de 2"));
    nothing(&home.key(key(KeyCode::Char('N')), &mut look), "");
    assert!(draw(&home, &look, 100, 15).contains("coincidencia 2 de 2"));
    find_preview(&mut home, &mut look, "missing");
    assert!(draw(&home, &look, 100, 15).contains("sin coincidencias"));
    find_preview(&mut home, &mut look, "0");
    assert!(draw(&home, &look, 100, 15).contains("1200.5"));
    nothing(&home.key(key(KeyCode::Char('N')), &mut look), "");
    assert!(draw(&home, &look, 100, 15).contains("Square"));
}

/// Colors source tokens with the theme and preserves legibility in `NO_COLOR` mode.
#[test]
fn highlights_source_with_the_active_palette() {
    use ratatui::style::Color;

    let mut look = look();
    let mut home = open_local_preview(
        &mut look,
        "example.rs",
        b"fn display() {\n    let label = \"green\";\n    let count = 42;\n    // explanation\n}",
    );
    let screen = draw(&home, &look, 80, 12);
    assert!(screen.contains("example.rs · Rust · 42 B"));
    let mut terminal = Terminal::new(TestBackend::new(80, 12)).expect("backend");
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .expect("draw");
    for (word, color) in [
        ("fn display", look.palette.accent),
        ("// explanation", look.palette.muted),
        ("green", look.palette.ok),
        ("42;", look.palette.warn),
        ("display", look.palette.info),
    ] {
        let point = pointer(&screen, word);
        assert_eq!(
            terminal.backend().buffer()[(point.x, point.y)].fg,
            color,
            "{word}"
        );
    }

    find_preview(&mut home, &mut look, "FN DISPLAY");
    terminal
        .draw(|frame| home.render(frame, &look, "*"))
        .expect("draw");
    let point = pointer(&screen, "fn display");
    assert_eq!(
        terminal.backend().buffer()[(point.x, point.y)].bg,
        look.palette.accent
    );

    let plain = Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::detect(
            |name| (name == "NO_COLOR").then(|| "1".to_owned()),
            Colors::Theme,
        ),
        Settings::default(),
    );
    terminal
        .draw(|frame| home.render(frame, &plain, "*"))
        .expect("draw");
    for cell in &terminal.backend().buffer().content {
        assert_eq!((cell.fg, cell.bg), (Color::Reset, Color::Reset));
    }
    assert!(draw(&home, &plain, 80, 12).contains("// explanation"));
}

/// Loads a real preview through the explorer and its reply.
fn open_local_preview(look: &mut Look, name: &str, bytes: &[u8]) -> Home {
    let (mut home, _) = Home::new(session(), "ana@example.com", false);
    home.key(key(KeyCode::Tab), look);
    list_preview_file(&mut home, look, Side::Left, name);
    home.key(key(KeyCode::End), look);
    assert!(matches!(
        home.key(key(KeyCode::F(3)), look).as_slice(),
        [Ask::Call(Call::Preview { request: 1, .. })]
    ));
    nothing(
        &home.reply(Reply::Preview(1, Ok((bytes.to_vec(), false))), look),
        "",
    );
    home
}

/// Replaces the current preview query and submits it.
fn find_preview(home: &mut Home, look: &mut Look, query: &str) {
    nothing(&home.key(key(KeyCode::Char('/')), look), "");
    nothing(&home.key(ctrl('u'), look), "");
    home.paste(query);
    nothing(&home.key(key(KeyCode::Enter), look), "");
}

/// Replaces a panel listing with one file for a preview test.
fn list_preview_file(home: &mut Home, look: &mut Look, side: Side, name: &str) {
    use super::explorer::{Entry, Kind};

    home.reply(
        Reply::Homed {
            side,
            outcome: Ok("/preview".to_owned()),
        },
        look,
    );
    home.reply(
        Reply::Listed {
            side,
            path: "/preview".to_owned(),
            outcome: Ok(vec![Entry {
                name: name.to_owned(),
                kind: Kind::File,
                size: 42,
                modified: None,
            }]),
        },
        look,
    );
}
