//! Tests of a panel of the explorer: listing and moving through folders, sorting, filtering, marks, the forms and the confirmation, and
//! how it is drawn.

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use jiff::tz::TimeZone;
use ratatui::Terminal;
use ratatui::backend::TestBackend;
use uuid::Uuid;

use super::DEFAULT_THEME;
use super::explorer::{Entry, Kind, Op, Panel, PanelAsk, Source, human};
use super::look::Look;
use super::theme::ColorMode;
use crate::config::Settings;
use crate::i18n::Language;

/// A pressed key.
fn key(code: KeyCode) -> KeyEvent {
    KeyEvent::new(code, KeyModifiers::NONE)
}

/// A key pressed with Ctrl.
fn ctrl(c: char) -> KeyEvent {
    KeyEvent::new(KeyCode::Char(c), KeyModifiers::CONTROL)
}

/// An entry of a kind, a size and a date given in seconds since 2026-01-01.
fn entry(name: &str, kind: Kind, size: u64, seconds: i64) -> Entry {
    Entry {
        name: name.to_owned(),
        kind,
        size,
        modified: Some(jiff::Timestamp::from_second(1_767_225_600 + seconds).expect("a time")),
    }
}

/// The entries of a folder: two folders, three files and a hidden file.
fn folder() -> Vec<Entry> {
    vec![
        entry("docs", Kind::Folder, 0, 50),
        entry("Backups", Kind::Folder, 0, 10),
        entry("b.txt", Kind::File, 3000, 30),
        entry("A.log", Kind::File, 10, 20),
        entry("c.bin", Kind::Link, 2_000_000, 40),
        entry(".hidden", Kind::File, 1, 60),
    ]
}

/// A panel on a site, listed at /upload.
fn site_panel() -> Panel {
    let mut panel = Panel::new(false);
    assert_eq!(
        panel.open(Source::Site {
            id: Uuid::nil(),
            name: "acme".to_owned()
        }),
        Op::Home
    );
    assert_eq!(
        panel.homed(Ok("/upload".to_owned())),
        vec![PanelAsk::Op(Op::List("/upload".to_owned()))]
    );
    panel.listed("/upload", Ok(folder()));
    panel
}

/// Draws a panel and returns its text.
fn draw(panel: &Panel, width: u16, height: u16) -> String {
    let look = Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::TrueColor,
        Settings::default(),
    );
    let mut terminal = Terminal::new(TestBackend::new(width, height)).expect("backend");
    terminal
        .draw(|frame| panel.render(frame, frame.area(), &look, true, &TimeZone::UTC))
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

/// Moves and redraws a large remote listing, then verifies that changing its filter updates the visible rows.
#[test]
fn scrolls_a_folder_with_fifty_thousand_files() {
    let mut panel = site_panel();
    panel.key(key(KeyCode::Char('r')));
    let entries = (0..50_000)
        .rev()
        .map(|index| entry(&format!("file-{index:05}.txt"), Kind::File, index, 0))
        .collect();
    let started = std::time::Instant::now();
    panel.listed("/upload", Ok(entries));
    let listing = started.elapsed();
    let look = Look::new(
        Language::English,
        DEFAULT_THEME,
        ColorMode::TrueColor,
        Settings::default(),
    );
    let mut terminal = Terminal::new(TestBackend::new(100, 24)).expect("backend");
    let started = std::time::Instant::now();
    for direction in [KeyCode::Down, KeyCode::Up] {
        for _ in 0..60 {
            assert_eq!(panel.key(key(direction)), Vec::new());
            terminal
                .draw(|frame| panel.render(frame, frame.area(), &look, true, &TimeZone::UTC))
                .expect("draw");
        }
        if direction == KeyCode::Down {
            assert_eq!(
                panel.current().expect("current file").name,
                "file-00059.txt"
            );
        }
    }
    eprintln!(
        "50,000 files: listing {listing:?}, 120 key presses and draws {:?}",
        started.elapsed()
    );
    assert!(panel.current().is_none());
    panel.key(key(KeyCode::Char('f')));
    panel.paste("FILE-49999");
    panel.key(key(KeyCode::Enter));
    assert!(draw(&panel, 100, 24).contains("file-49999.txt"));
    assert_eq!(
        panel.current().expect("filtered file").name,
        "file-49999.txt"
    );
}

/// The listing dims the retained rows and shows its translated path and hint with colors disabled.
#[test]
fn draws_listing_with_no_color() {
    use ratatui::style::Modifier;

    let mut panel = site_panel();
    panel.key(key(KeyCode::Char('r')));
    for _ in 0..25 {
        panel.tick();
    }
    let look = Look::new(
        Language::Spanish,
        DEFAULT_THEME,
        ColorMode::Plain,
        Settings::default(),
    );
    let mut terminal = Terminal::new(TestBackend::new(80, 15)).expect("backend");
    terminal
        .draw(|frame| panel.render(frame, frame.area(), &look, true, &TimeZone::UTC))
        .expect("draw");
    let buffer = terminal.backend().buffer();
    assert!(buffer[(3, 2)].modifier.contains(Modifier::DIM));
    assert_eq!(buffer[(3, 2)].symbol(), "B");
    let text = buffer
        .content()
        .iter()
        .map(ratatui::buffer::Cell::symbol)
        .collect::<String>();
    assert!(text.contains("Cargando /upload…"));
    assert!(text.contains("Esc para cancelar"));
    let row = 6;
    let spinner = (0..80)
        .find(|column| buffer[(*column, row)].symbol() == "⠴")
        .expect("spinner inside the panel");
    assert!(!buffer[(spinner, row)].modifier.contains(Modifier::DIM));
}

/// Control characters are visible in the explorer, while activation and paths use the original name.
#[test]
fn draws_control_characters_in_filenames() {
    let mut panel = site_panel();
    let name = "tab\tname\n.txt";
    assert_eq!(
        panel.homed(Ok("/upload".to_owned())),
        vec![PanelAsk::Op(Op::List("/upload".to_owned()))]
    );
    panel.listed("/upload", Ok(vec![entry(name, Kind::File, 1, 0)]));
    assert!(draw(&panel, 80, 15).contains("tab⇥name�.txt"));
    panel.key(key(KeyCode::Down));
    assert!(matches!(
        panel.key(key(KeyCode::Enter)).as_slice(),
        [PanelAsk::Activate(file)] if file.name == name
    ));
    assert_eq!(panel.join(name), "/upload/tab\tname\n.txt");
}

/// The names of the rows, in order, as drawn.
fn names(panel: &Panel) -> Vec<String> {
    draw(panel, 80, 14)
        .lines()
        .skip(1)
        .filter_map(|row| row.strip_prefix('│'))
        .filter(|row| row.starts_with("  ") || row.starts_with("✓ "))
        .map(|row| row.chars().skip(2).collect::<String>())
        .filter_map(|row| row.split_whitespace().next().map(str::to_owned))
        .filter(|name| name != "│")
        .collect()
}

/// A folder lists folders first and then files, by name ignoring case, with the folder above; hidden files show on `.`; `s` sorts by
/// size and then date, `S` reverses; sizes are in binary units and dates in the time zone given.
#[test]
fn lists_and_sorts() {
    let mut panel = site_panel();
    assert_eq!(panel.path(), "/upload");
    assert_eq!(
        panel.key(key(KeyCode::Char('z'))),
        Vec::new(),
        "a key without an action"
    );
    assert_eq!(
        names(&panel),
        ["../", "Backups/", "docs/", "A.log", "b.txt", "c.bin"]
    );
    let text = draw(&panel, 80, 14);
    assert!(
        text.contains("acme")
            && text.contains("/upload")
            && text.contains("2.9 KiB")
            && text.contains("1.9 MiB")
            && text.contains("2026-01-01 00:00")
    );
    panel.key(key(KeyCode::Char('.')));
    assert!(names(&panel).contains(&".hidden".to_owned()));
    panel.key(key(KeyCode::Char('.')));
    panel.key(key(KeyCode::Char('s')));
    assert_eq!(names(&panel)[3..], ["A.log", "b.txt", "c.bin"]);
    panel.key(key(KeyCode::Char('S')));
    assert_eq!(
        names(&panel)[1..],
        ["Backups/", "docs/", "c.bin", "b.txt", "A.log"]
    );
    panel.key(key(KeyCode::Char('s')));
    assert_eq!(
        names(&panel)[1..],
        ["docs/", "Backups/", "c.bin", "b.txt", "A.log"]
    );
    panel.key(key(KeyCode::Char('s')));
    panel.key(key(KeyCode::Char('S')));
    assert_eq!(names(&panel)[1..3], ["Backups/", "docs/"]);
    assert_eq!(human(10), "10 B");
    assert_eq!(human(u64::MAX), "16384.0 PiB");
    let mut root = Panel::new(false);
    root.open(Source::Site {
        id: Uuid::nil(),
        name: "root".to_owned(),
    });
    root.homed(Ok("/".to_owned()));
    root.listed("/", Ok(vec![]));
    assert_eq!(root.key(key(KeyCode::Enter)), Vec::new(), "nothing to open");
}

/// Enter opens a folder or a link and goes up from the folder above; Backspace goes up and keeps the folder left under the cursor; an
/// answer for a folder no longer asked for is dropped, and a failed listing keeps the folder in view with the reason.
#[test]
fn moves_through_folders() {
    let mut panel = site_panel();
    panel.key(key(KeyCode::Char('j')));
    panel.key(key(KeyCode::Down));
    assert_eq!(
        panel.key(key(KeyCode::Enter)),
        vec![PanelAsk::Op(Op::List("/upload/docs".to_owned()))]
    );
    panel.listed("/elsewhere", Ok(vec![]));
    assert_eq!(panel.path(), "/upload", "a stale answer");
    panel.listed("/upload/docs", Ok(vec![entry("x", Kind::File, 1, 0)]));
    assert_eq!(panel.path(), "/upload/docs");
    assert_eq!(
        panel.key(key(KeyCode::Backspace)),
        vec![PanelAsk::Op(Op::List("/upload".to_owned()))]
    );
    panel.listed("/upload", Ok(folder()));
    assert!(
        draw(&panel, 80, 14)
            .lines()
            .any(|row| row.contains("docs/") && row.contains("docs")),
        "docs under the cursor"
    );
    panel.key(key(KeyCode::Char('G')));
    assert_eq!(
        panel.key(key(KeyCode::Char('l'))),
        vec![PanelAsk::Op(Op::List("/upload/c.bin".to_owned()))]
    );
    panel.listed("/upload/c.bin", Err("not a folder".to_owned()));
    assert_eq!(panel.path(), "/upload");
    assert!(draw(&panel, 80, 14).contains("not a folder"));
    panel.key(key(KeyCode::Up));
    assert!(
        matches!(panel.key(key(KeyCode::Right)).as_slice(), [PanelAsk::Activate(entry)] if entry.name == "b.txt"),
        "a file is handed to the main screen"
    );
    panel.key(key(KeyCode::Home));
    assert_eq!(
        panel.key(key(KeyCode::Enter)),
        vec![PanelAsk::Op(Op::List("/".to_owned()))]
    );
    panel.listed("/", Ok(vec![entry("upload", Kind::Folder, 0, 0)]));
    assert_eq!(
        panel.key(key(KeyCode::Left)),
        Vec::new(),
        "nothing above the root"
    );
    assert_eq!(
        panel.key(key(KeyCode::Enter)),
        vec![PanelAsk::Op(Op::List("/upload".to_owned()))]
    );
    panel.listed("/upload", Ok(vec![]));
    assert_eq!(names(&panel), ["../"]);
    assert_eq!(
        panel.key(key(KeyCode::Char('h'))),
        vec![PanelAsk::Op(Op::List("/".to_owned()))]
    );
    panel.key(key(KeyCode::Esc));
    panel.key(key(KeyCode::PageDown));
    panel.key(key(KeyCode::PageUp));
    panel.key(key(KeyCode::End));
    panel.key(key(KeyCode::Char('k')));
    panel.key(key(KeyCode::Char('g')));
    assert_eq!(
        panel.key(key(KeyCode::Char('r'))),
        vec![PanelAsk::Op(Op::List("/upload".to_owned()))]
    );
    assert!(draw(&panel, 80, 14).contains("Loading /upload…"));
}

/// The local disk joins and goes up with the paths of the system; its home folder may fail to be found.
#[test]
fn browses_the_local_disk() {
    let mut panel = Panel::new(true);
    assert!(draw(&panel, 60, 10).contains("Choose a site on the left (Enter) to connect"));
    assert_eq!(
        (
            Source::Local.site(),
            Source::Site {
                id: Uuid::nil(),
                name: String::new()
            }
            .site()
        ),
        (None, Some(Uuid::nil()))
    );
    assert_eq!(
        panel.key(key(KeyCode::Char('j'))),
        Vec::new(),
        "an empty panel ignores keys"
    );
    panel.open(Source::Local);
    assert!(draw(&panel, 60, 10).contains("Loading…"));
    assert_eq!(panel.homed(Err("no home folder".to_owned())), Vec::new());
    assert!(draw(&panel, 60, 10).contains("no home folder"));
    let root = std::env::temp_dir();
    let root = root.to_string_lossy().into_owned();
    panel.homed(Ok(root.clone()));
    panel.listed(&root, Ok(vec![entry("inner", Kind::Folder, 0, 0)]));
    panel.key(key(KeyCode::Down));
    let inner = std::path::Path::new(&root)
        .join("inner")
        .to_string_lossy()
        .into_owned();
    assert_eq!(
        panel.key(key(KeyCode::Enter)),
        vec![PanelAsk::Op(Op::List(inner.clone()))]
    );
    panel.listed(&inner, Ok(vec![]));
    assert_eq!(
        panel.key(key(KeyCode::Backspace)),
        vec![PanelAsk::Op(Op::List(root.clone()))]
    );
    panel.listed(&root, Ok(vec![entry("inner", Kind::Folder, 0, 0)]));
    assert_eq!(panel.source(), Some(&Source::Local));
    assert_eq!(
        panel.key(ctrl('d')),
        Vec::new(),
        "the local disk does not disconnect"
    );
    panel.close();
    assert_eq!(panel.source(), None);
}

/// Space and Insert mark and move down, Ctrl+A marks everything, Esc clears the marks and then the filter; `f` filters while typing,
/// Enter keeps the filter and Esc clears it.
#[test]
fn marks_and_filters() {
    let mut panel = site_panel();
    panel.key(key(KeyCode::Char(' ')));
    panel.key(key(KeyCode::Char(' ')));
    panel.key(key(KeyCode::Insert));
    assert_eq!(panel.marked(), ["Backups", "docs"]);
    panel.key(key(KeyCode::Up));
    panel.key(key(KeyCode::Char(' ')));
    assert_eq!(panel.marked(), ["Backups"]);
    assert!(draw(&panel, 80, 14).contains("✓ Backups/"));
    panel.key(ctrl('a'));
    assert_eq!(panel.marked().len(), 5);
    panel.key(key(KeyCode::Esc));
    assert_eq!(panel.marked(), Vec::<String>::new());

    panel.key(key(KeyCode::Char('f')));
    assert!(panel.captures());
    panel.paste("T");
    for c in "XT".chars() {
        panel.key(key(KeyCode::Char(c)));
    }
    assert!(draw(&panel, 80, 14).contains("Filter: TXT▏"));
    assert_eq!(names(&panel), ["b.txt"]);
    panel.key(key(KeyCode::Enter));
    assert!(!panel.captures());
    assert!(draw(&panel, 80, 14).contains("Filter: TXT"));
    panel.key(key(KeyCode::Esc));
    assert_eq!(names(&panel).len(), 6);
    panel.key(ctrl('f'));
    panel.key(key(KeyCode::Char('z')));
    assert_eq!(names(&panel), Vec::<String>::new());
    assert!(draw(&panel, 80, 14).contains("This folder is empty."));
    panel.key(key(KeyCode::Esc));
    assert!(!panel.captures());
    assert_eq!(names(&panel).len(), 6);
}

/// `n` and F7 create a folder, F2 renames the entry under the cursor (the same name does nothing), Ctrl+G goes to a path; Esc and an
/// empty field give up; the change lists the folder again and selects the new name, and a failure shows its reason.
#[test]
fn creates_renames_and_goes() {
    let mut panel = site_panel();
    panel.key(key(KeyCode::Char('n')));
    assert!(panel.captures());
    assert!(draw(&panel, 80, 14).contains("New folder"));
    assert_eq!(panel.key(key(KeyCode::Enter)), Vec::new(), "an empty name");
    panel.paste("n");
    for c in "ew".chars() {
        panel.key(key(KeyCode::Char(c)));
    }
    assert_eq!(
        panel.key(key(KeyCode::Enter)),
        vec![PanelAsk::Op(Op::Mkdir("/upload/new".to_owned()))]
    );
    assert_eq!(
        panel.changed(Ok(())),
        vec![PanelAsk::Op(Op::List("/upload".to_owned()))]
    );
    let mut listed = folder();
    listed.push(entry("new", Kind::Folder, 0, 0));
    panel.listed("/upload", Ok(listed));
    assert!(draw(&panel, 80, 14).lines().any(|row| row.contains("new/")));
    panel.key(key(KeyCode::F(7)));
    assert_eq!(panel.key(key(KeyCode::Esc)), Vec::new());
    assert!(!panel.captures());

    panel.key(key(KeyCode::Char('G')));
    panel.key(key(KeyCode::F(2)));
    assert!(draw(&panel, 80, 14).contains("Rename"));
    assert_eq!(panel.key(key(KeyCode::Enter)), Vec::new(), "the same name");
    panel.key(key(KeyCode::F(2)));
    panel.key(ctrl('u'));
    assert_eq!(panel.key(key(KeyCode::Enter)), Vec::new(), "an empty name");
    assert!(panel.captures(), "the form stays open");
    panel.key(key(KeyCode::Esc));
    panel.key(key(KeyCode::F(6)));
    assert!(draw(&panel, 80, 14).contains("Rename"));
    panel.key(key(KeyCode::Char('2')));
    assert_eq!(
        panel.key(key(KeyCode::Enter)),
        vec![PanelAsk::Op(Op::Rename {
            from: "/upload/c.bin".to_owned(),
            to: "/upload/c.bin2".to_owned()
        })]
    );
    assert_eq!(panel.changed(Err("exists".to_owned())), Vec::new());
    assert!(draw(&panel, 80, 14).contains("exists"));
    panel.key(key(KeyCode::Home));
    assert_eq!(panel.key(key(KeyCode::F(2))), Vec::new());
    assert!(!panel.captures(), "the folder above is not renamed");

    panel.key(ctrl('g'));
    assert!(draw(&panel, 80, 14).contains("Go to"));
    panel.key(ctrl('u'));
    assert_eq!(panel.key(key(KeyCode::Enter)), Vec::new(), "an empty path");
    panel.key(ctrl('g'));
    panel.key(ctrl('u'));
    for c in "/srv".chars() {
        panel.key(key(KeyCode::Char(c)));
    }
    assert_eq!(
        panel.key(key(KeyCode::Enter)),
        vec![PanelAsk::Op(Op::List("/srv".to_owned()))]
    );
}

/// Delete and F8 ask before deleting the marked entries, or the one under the cursor, naming them; `y` and Enter delete, `n` and Esc
/// keep them, other keys wait; Ctrl+D disconnects a site.
#[test]
fn deletes_after_asking() {
    let mut panel = site_panel();
    panel.key(key(KeyCode::Home));
    panel.key(key(KeyCode::Delete));
    assert!(!panel.captures(), "the folder above is not deleted");
    panel.key(key(KeyCode::Down));
    panel.key(key(KeyCode::Delete));
    let text = draw(&panel, 80, 16);
    assert!(
        text.contains("Delete 1 entries?")
            && text.contains("/upload/Backups/")
            && text.contains("This cannot be undone.")
    );
    panel.key(key(KeyCode::Char('x')));
    assert!(panel.captures());
    assert_eq!(panel.key(key(KeyCode::Char('n'))), Vec::new());
    panel.key(ctrl('a'));
    panel.key(key(KeyCode::F(8)));
    panel.key(key(KeyCode::Esc));
    let mut many = folder();
    many.extend((0..8).map(|n| entry(&format!("f{n}"), Kind::File, 1, 0)));
    panel.key(key(KeyCode::Char('r')));
    panel.listed("/upload", Ok(many));
    panel.key(ctrl('a'));
    panel.key(key(KeyCode::Delete));
    let text = draw(&panel, 80, 22);
    assert!(text.contains("and 7 more"), "{text}");
    let asks = panel.key(key(KeyCode::Char('y')));
    let [PanelAsk::Op(Op::Delete(paths))] = asks.as_slice() else {
        panic!("{asks:?}")
    };
    assert_eq!(paths.len(), 13);
    assert!(
        paths.contains(&("/upload/docs".to_owned(), true))
            && paths.contains(&("/upload/b.txt".to_owned(), false))
    );
    assert_eq!(panel.marked(), Vec::<String>::new());
    panel.key(key(KeyCode::Delete));
    assert_eq!(panel.key(key(KeyCode::Enter)).len(), 1);
    assert_eq!(panel.key(ctrl('d')), vec![PanelAsk::Op(Op::Disconnect)]);
    assert!(draw(&panel, 30, 3).contains("acme"));
}
