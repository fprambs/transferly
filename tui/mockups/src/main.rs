//! Draws the screens of the design of the terminal client (docs/design/terminal-client.md) with ratatui's test backend and writes
//! them, as text, to the Markdown file given as the first argument, and as HTML pages for screenshots to the folder given as the
//! second. The data is made up; only the layout, the widgets and the colors matter.

use std::fmt::Write as _;

mod html;
mod real;

use ratatui::Terminal;
use ratatui::backend::TestBackend;
use ratatui::buffer::Buffer;
use ratatui::layout::{Constraint, Layout, Rect};
use ratatui::style::{Color, Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, BorderType, Cell, Clear, LineGauge, Paragraph, Row, Table, TableState, Tabs, Wrap};
use ratatui::Frame;

/// The themes of the web (tui/themes.json, exported from frontend/src/lib/theme/themes.ts by make gen) and the one the screens wear.
mod theme {
    use std::cell::RefCell;
    use std::collections::HashMap;

    use ratatui::style::Color;
    use serde::Deserialize;

    /// A theme as the web defines it: its identifier, name and colors by token.
    #[derive(Clone, Deserialize)]
    pub struct Theme {
        /// Identifier, such as transferly-dark.
        pub id: String,
        /// Name shown in the settings.
        pub name: String,
        /// Colors by token, as #rrggbb.
        pub tokens: HashMap<String, String>,
    }

    /// The file of themes.
    #[derive(Deserialize)]
    struct File {
        themes: Vec<Theme>,
    }

    /// Every theme of the web, in its order.
    pub fn all() -> Vec<Theme> {
        let file: File = serde_json::from_str(include_str!("../../themes.json")).expect("tui/themes.json");
        file.themes
    }

    thread_local! {
        /// The theme the screens are drawn in.
        static CURRENT: RefCell<Option<Theme>> = const { RefCell::new(None) };
    }

    /// Makes the screens wear a theme.
    pub fn wear(theme: &Theme) {
        CURRENT.with(|current| *current.borrow_mut() = Some(theme.clone()));
    }

    /// The color of a token of the current theme.
    fn token(name: &str) -> Color {
        CURRENT.with(|current| {
            let current = current.borrow();
            let hex = current.as_ref().and_then(|theme| theme.tokens.get(name)).expect("a theme with every token");
            let value = u32::from_str_radix(hex.trim_start_matches('#'), 16).expect("a #rrggbb color");
            Color::Rgb((value >> 16) as u8, (value >> 8) as u8, value as u8)
        })
    }

    /// Background of the screen.
    pub fn bg() -> Color { token("bg") }
    /// Background of bars and popups.
    pub fn surface() -> Color { token("surface") }
    /// Background of the selected row.
    pub fn raised() -> Color { token("raised") }
    /// Borders of panels without the focus.
    pub fn line() -> Color { token("line") }
    /// Text.
    pub fn text() -> Color { token("text") }
    /// Secondary text.
    pub fn muted() -> Color { token("muted") }
    /// Faint text, such as headers.
    pub fn faint() -> Color { token("faint") }
    /// Accent: the focus, progress and the selected tab.
    pub fn accent() -> Color { token("accent") }
    /// Text in the accent color.
    pub fn accent_text() -> Color { token("accentText") }
    /// Text on the accent color.
    pub fn ink() -> Color { token("ink") }
    /// Success.
    pub fn ok() -> Color { token("ok") }
    /// Danger.
    pub fn danger() -> Color { token("danger") }
    /// Folders and links.
    pub fn info() -> Color { token("info") }
}

/// A screen of the design: its name, what it shows, its size and how to draw it.
struct Screen {
    name: &'static str,
    about: &'static str,
    width: u16,
    height: u16,
    draw: fn(&mut Frame),
}

/// Renders every screen and writes the Markdown document.
fn main() {
    let out = std::env::args().nth(1).unwrap_or_else(|| "docs/design/terminal-client-mockups.md".into());
    let pages = std::env::args().nth(2);
    let screens = [
        Screen { name: "Explorer", about: "The main screen: tabs, sites, two panels, the transfer queue and the status bar.", width: 120, height: 34, draw: explorer },
        Screen { name: "Explorer on a narrow terminal", about: "Below 100 columns: the sites panel hides and only the focused panel shows; Tab switches.", width: 80, height: 24, draw: narrow },
        Screen { name: "Server", about: "First start: the address of the server, with the profiles used before.", width: 100, height: 28, draw: server },
        Screen { name: "Approval in the browser", about: "Waiting for the browser to hand back the code through 127.0.0.1.", width: 100, height: 28, draw: approval },
        Screen { name: "Paste the code", about: "Over SSH or without a listener: the code shown by the browser is pasted here.", width: 100, height: 28, draw: paste },
        Screen { name: "Vault", about: "A site needs the vault: the master password, then the action runs again.", width: 120, height: 34, draw: vault },
        Screen { name: "Host key", about: "A question of the server: trust the key of a host seen for the first time.", width: 120, height: 34, draw: host_key },
        Screen { name: "Copy with existing files", about: "F5 with a selection: where it goes and what to do with files that exist.", width: 120, height: 34, draw: copy },
        Screen { name: "Delete", about: "Delete, in red, naming what goes.", width: 120, height: 34, draw: delete },
        Screen { name: "Help", about: "? lists every key of the screen, with a filter.", width: 120, height: 34, draw: help },
        Screen { name: "Palette", about: "Ctrl+K finds actions, sites and folders by name.", width: 120, height: 34, draw: palette },
        Screen { name: "Commands", about: "/ opens the command line at the bottom, with the commands that match, as in Claude Code.", width: 120, height: 34, draw: commands },
        Screen { name: "Settings", about: "/config: language and theme of the account, shared with the web, and settings of this computer.", width: 120, height: 34, draw: config },
        Screen { name: "Error", about: "A guided error: message, probable cause, what to do and the stable code.", width: 120, height: 34, draw: error },
        Screen { name: "Log", about: "The bottom panel on its log tab, following the connection.", width: 120, height: 34, draw: log },
    ];
    let mut doc = String::from(
        "# Mockups of the terminal client\n\nGenerated by `tui/mockups` (`make tui-mockups`) with ratatui 0.30; do not edit by hand. The data is made up. \
         See [the design](terminal-client.md) for the keys and the reasons.\n",
    );
    let themes = theme::all();
    theme::wear(&themes[0]);
    for screen in screens {
        let buffer = draw(&screen);
        let slug = screen.name.to_lowercase().replace(' ', "-");
        let _ = write!(
            doc,
            "\n## {}\n\n{} {} × {}.\n\n![{}](screens/{slug}.png)\n\n```text\n{}```\n",
            screen.name, screen.about, screen.width, screen.height, screen.name, text(&buffer)
        );
        if let Some(dir) = &pages {
            std::fs::write(format!("{dir}/{slug}.html"), html::page(&buffer, defaults())).expect("writing a page");
        }
    }
    doc.push_str("\n## Themes\n\nThe explorer in each theme of the web, read from `tui/themes.json`; the theme is the one of the account, shared with the web.\n");
    let gallery = Screen { name: "", about: "", width: 120, height: 34, draw: explorer };
    for theme in &themes {
        theme::wear(theme);
        let buffer = draw(&gallery);
        let _ = write!(doc, "\n### {}\n\n![{}](screens/theme-{}.png)\n", theme.name, theme.name, theme.id);
        if let Some(dir) = &pages {
            std::fs::write(format!("{dir}/theme-{}.html", theme.id), html::page(&buffer, defaults())).expect("writing a page");
        }
    }
    doc.push_str("\n## The application\n\nScreens of `transferly-tui` itself, drawn by its interface after the events that lead to them, not mockups.\n");
    for shot in real::shots() {
        let _ = write!(doc, "\n### {}\n\n{}\n\n![{}](screens/{}.png)\n", shot.slug, shot.about, shot.slug, shot.slug);
        if let Some(dir) = &pages {
            std::fs::write(format!("{dir}/{}.html", shot.slug), html::page(&shot.buffer, shot.defaults)).expect("writing a page");
        }
    }
    std::fs::write(&out, doc).expect("writing the mockups");
}

/// Draws a screen on a test backend, on the background of the theme, and returns its buffer.
fn draw(screen: &Screen) -> Buffer {
    let mut terminal = Terminal::new(TestBackend::new(screen.width, screen.height)).expect("test backend");
    terminal
        .draw(|frame| {
            frame.render_widget(Block::new().style(Style::new().bg(theme::bg()).fg(theme::text())), frame.area());
            (screen.draw)(frame);
        })
        .expect("drawing");
    terminal.backend().buffer().clone()
}

/// The cells of a buffer as lines of text.
fn text(buffer: &Buffer) -> String {
    let mut text = String::new();
    for y in 0..buffer.area.height {
        let line: String = (0..buffer.area.width).map(|x| buffer[(x, y)].symbol().to_string()).collect();
        text.push_str(line.trim_end());
        text.push('\n');
    }
    text
}

/// A panel with rounded borders, a title and, when focused, a bold title.
fn panel(title: &str, focused: bool) -> Block<'static> {
    let title = Line::from(format!(" {title} "));
    let block = Block::bordered().border_type(BorderType::Rounded);
    if focused {
        block.border_style(Style::new().fg(theme::accent())).title(title.fg(theme::accent_text()).bold())
    } else {
        block.border_style(Style::new().fg(theme::line())).title(title.fg(theme::muted()))
    }
}

/// A rectangle of the given size centered in area.
fn centered(area: Rect, width: u16, height: u16) -> Rect {
    let width = width.min(area.width);
    let height = height.min(area.height);
    Rect::new(area.x + (area.width - width) / 2, area.y + (area.height - height) / 2, width, height)
}

/// Clears a centered rectangle and draws a popup block on it, returning the inner area.
fn popup(frame: &mut Frame, width: u16, height: u16, title: &str, keys: &str) -> Rect {
    popup_in(frame, width, height, title, keys, theme::accent())
}

/// A popup whose border and title have the given color, such as the danger color of confirmations that destroy.
fn popup_in(frame: &mut Frame, width: u16, height: u16, title: &str, keys: &str, color: Color) -> Rect {
    let area = centered(frame.area(), width, height);
    frame.render_widget(Clear, area);
    let block = Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(Style::new().fg(color))
        .title(Line::from(format!(" {title} ")).fg(color).bold())
        .title_bottom(Line::from(format!(" {keys} ")).right_aligned())
        .style(Style::new().bg(theme::surface()));
    let inner = block.inner(area);
    frame.render_widget(block, area);
    inner
}

/// The tab bar with the version on the right.
fn tabs(frame: &mut Frame, area: Rect) {
    let [left, right] = Layout::horizontal([Constraint::Fill(1), Constraint::Length(44)]).areas(area);
    frame.render_widget(Tabs::new(vec![" 1 prod-web ", " 2 backups ", " 3 local "]).select(0).divider("│").style(Style::new().fg(theme::muted())).highlight_style(Style::new().fg(theme::ink()).bg(theme::accent()).bold()), left);
    frame.render_widget(Line::from(vec![Span::from("transferly-tui 1.4.0 ").dim(), Span::from("→ 1.5.0 · U to update ").fg(theme::accent_text()).bold()]).right_aligned(), right);
}

/// The status bar: where the session is, then the keys of the focused panel.
fn status(frame: &mut Frame, area: Rect, keys: &str) {
    frame.render_widget(Block::new().style(Style::new().bg(theme::surface()).fg(theme::muted())), area);
    let keys = format!("{keys}  ? help ");
    let [left, right] = Layout::horizontal([Constraint::Fill(1), Constraint::Length(keys.chars().count() as u16)]).spacing(2).areas(area);
    frame.render_widget(Line::from(" ana@files.acme.com · Acme Web · vault unlocked"), left);
    frame.render_widget(Line::from(keys).right_aligned(), right);
}

/// The tree of sites of the workspace.
fn sites(frame: &mut Frame, area: Rect, focused: bool) {
    let lines = vec![
        Line::from("▾ Production"),
        Line::from(vec![Span::from("    "), Span::from("●").fg(theme::ok()), Span::from(" prod-web")]),
        Line::from("      prod-db"),
        Line::from("▾ Staging"),
        Line::from("      staging"),
        Line::from("▸ Customers"),
        Line::from("  backups (S3)"),
        Line::from("  drive (Google)"),
        Line::from("  nas (WebDAV)"),
    ];
    frame.render_widget(Paragraph::new(lines).block(panel("Sites · Acme Web", focused).title_bottom(" f filter ")), area);
}

/// An explorer panel: a table of entries with marks, sizes and dates, its site as title and its path at the bottom.
fn explorer_panel(frame: &mut Frame, area: Rect, title: &str, path: &str, rows: &[(&str, &str, &str, &str)], selected: usize, focused: bool) {
    let rows: Vec<Row> = rows
        .iter()
        .map(|(mark, name, size, date)| {
            let name_cell = if name.ends_with('/') { Cell::from(*name).fg(theme::info()).bold() } else { Cell::from(*name) };
            let row = Row::new(vec![Cell::from(*mark), name_cell, Cell::from(Line::from(*size).right_aligned()), Cell::from(Line::from(*date).fg(theme::muted()))]);
            if *mark == "✓" { row.fg(theme::accent_text()) } else { row }
        })
        .collect();
    let table = Table::new(rows, [Constraint::Length(1), Constraint::Fill(1), Constraint::Length(9), Constraint::Length(11)])
        .header(Row::new(vec!["", "Name", "     Size", "Modified"]).fg(theme::faint()))
        .row_highlight_style(if focused { Style::new().bg(theme::raised()).bold() } else { Style::new().bg(theme::line()) })
        .highlight_symbol("› ")
        .block(panel(title, focused).title_bottom(Line::from(format!(" {path} "))));
    let mut state = TableState::new().with_selected(Some(selected));
    frame.render_stateful_widget(table, area, &mut state);
}

/// The local panel of the mockups.
fn local_rows() -> Vec<(&'static str, &'static str, &'static str, &'static str)> {
    vec![
        (" ", "../", "", ""),
        (" ", "releases/", "", "11-20 09:12"),
        ("✓", "site-v2.tar.gz", "48.2 MiB", "11-24 18:40"),
        ("✓", "assets.zip", "212.7 MiB", "11-24 18:41"),
        (" ", "notes.md", "4.1 KiB", "11-22 11:03"),
        (" ", ".env.example", "312 B", "10-02 16:20"),
    ]
}

/// The remote panel of the mockups.
fn remote_rows() -> Vec<(&'static str, &'static str, &'static str, &'static str)> {
    vec![
        (" ", "../", "", ""),
        (" ", "current/", "", "11-23 22:05"),
        (" ", "shared/", "", "09-30 10:44"),
        (" ", "site-v1.tar.gz", "47.9 MiB", "11-01 08:15"),
        (" ", "deploy.log", "88.0 KiB", "11-24 18:30"),
    ]
}

/// The bottom panel on its transfers tab.
fn transfers(frame: &mut Frame, area: Rect, focused: bool) {
    let block = panel("Transfers 3 │ Log", focused).title_bottom(Line::from(" p pause · x cancel · Enter details ").right_aligned());
    let inner = block.inner(area);
    frame.render_widget(block, area);
    let items = [
        ("↑ site-v2.tar.gz → prod-web:/var/www", 0.62, "29.9 / 48.2 MiB", "11.2 MiB/s", "0:02"),
        ("↑ assets.zip → prod-web:/var/www", 0.08, "17.0 / 212.7 MiB", "10.8 MiB/s", "0:18"),
        ("⇄ backups:/db/2026-11-24.sql.gz → nas:/archive", 0.0, "queued", "", ""),
    ];
    let rows = Layout::vertical([Constraint::Length(1); 3]).split(inner);
    for ((name, ratio, done, speed, left), row) in items.iter().zip(rows.iter()) {
        let [n, g, d, s, l] = Layout::horizontal([
            Constraint::Fill(3),
            Constraint::Fill(2),
            Constraint::Length(17),
            Constraint::Length(11),
            Constraint::Length(5),
        ])
        .spacing(1)
        .areas(*row);
        frame.render_widget(Line::from(*name), n);
        frame.render_widget(LineGauge::default().filled_symbol("━").unfilled_symbol("─").filled_style(Style::new().fg(theme::accent())).unfilled_style(Style::new().fg(theme::line())).ratio(*ratio).label(format!("{:>3}%", (ratio * 100.0) as u8)), g);
        frame.render_widget(Line::from(*done).right_aligned(), d);
        frame.render_widget(Line::from(*speed).right_aligned(), s);
        frame.render_widget(Line::from(*left).right_aligned(), l);
    }
}

/// The main screen, with the right panel focused.
fn explorer(frame: &mut Frame) {
    explorer_with(frame, |frame, area| transfers(frame, area, false));
}

/// The main screen with the given bottom panel.
fn explorer_with(frame: &mut Frame, bottom: fn(&mut Frame, Rect)) {
    let [top, body, lower, bar] = Layout::vertical([Constraint::Length(1), Constraint::Fill(1), Constraint::Length(9), Constraint::Length(1)]).areas(frame.area());
    tabs(frame, top);
    let [side, left, right] = Layout::horizontal([Constraint::Length(24), Constraint::Fill(1), Constraint::Fill(1)]).areas(body);
    sites(frame, side, false);
    explorer_panel(frame, left, "Local", "~/projects/site · 2 marked, 260.9 MiB", &local_rows(), 2, false);
    explorer_panel(frame, right, "prod-web · SFTP", "/var/www", &remote_rows(), 3, true);
    bottom(frame, lower);
    status(frame, bar, "F5 copy · F7 folder · Del delete · f filter · / commands");
}

/// The main screen on a narrow terminal.
fn narrow(frame: &mut Frame) {
    let [top, body, lower, bar] = Layout::vertical([Constraint::Length(1), Constraint::Fill(1), Constraint::Length(5), Constraint::Length(1)]).areas(frame.area());
    frame.render_widget(Tabs::new(vec![" 1 prod-web ", " 2 backups "]).select(0).divider("│").style(Style::new().fg(theme::muted())).highlight_style(Style::new().fg(theme::ink()).bg(theme::accent()).bold()), top);
    explorer_panel(frame, body, "prod-web · SFTP  (Tab: Local)", "/var/www", &remote_rows(), 3, true);
    let block = panel("Transfers 3 │ Log", false);
    let inner = block.inner(lower);
    frame.render_widget(block, lower);
    frame.render_widget(LineGauge::default().filled_symbol("━").unfilled_symbol("─").filled_style(Style::new().fg(theme::accent())).unfilled_style(Style::new().fg(theme::line())).ratio(0.62).label("↑ site-v2.tar.gz  62%"), inner);
    frame.render_widget(Line::from(" ana@files.acme.com · unlocked · F5 copy · / commands · ? help"), bar);
}

/// The card of the sign-in screens: the name of the application above a bordered form.
fn card(frame: &mut Frame, title: &str, keys: &str) -> Rect {
    let area = centered(frame.area(), 72, 20);
    let [head, body] = Layout::vertical([Constraint::Length(3), Constraint::Fill(1)]).areas(area);
    frame.render_widget(
        Paragraph::new(vec![Line::from("Transferly").bold().centered(), Line::from("file transfers from the terminal").dim().centered()]),
        head,
    );
    let block = panel(title, true).title_bottom(Line::from(format!(" {keys} ")).right_aligned());
    let inner = block.inner(body).inner(ratatui::layout::Margin::new(2, 1));
    frame.render_widget(block, body);
    inner
}

/// A one-line text field with a label as the title of its border.
fn field(frame: &mut Frame, area: Rect, label: &str, value: &str, focused: bool) {
    frame.render_widget(Paragraph::new(value.to_string()).block(panel(label, focused)), area);
}

/// The first screen: the address of the server and the recent profiles.
fn server(frame: &mut Frame) {
    let inner = card(frame, "Sign in", "Enter continue · ↓ profiles · Esc quit");
    let [url, gap, recent] = Layout::vertical([Constraint::Length(3), Constraint::Length(1), Constraint::Fill(1)]).areas(inner);
    field(frame, url, "Server address", "https://files.acme.com▏", true);
    let _ = gap;
    frame.render_widget(
        Paragraph::new(vec![
            Line::from("Used before").dim(),
            Line::from("  files.acme.com      ana@acme.com      2 days ago"),
            Line::from("  transferly.lan      admin@home.lan    last month"),
        ]),
        recent,
    );
}

/// Waiting for the browser to approve.
fn approval(frame: &mut Frame) {
    let inner = card(frame, "Approve in your browser", "o open again · c copy · p paste a code · Esc back");
    frame.render_widget(
        Paragraph::new(vec![
            Line::from("Your browser opened this address. Sign in there and approve"),
            Line::from("transferly-tui on laptop."),
            Line::from(""),
            Line::from("https://files.acme.com/cli/authorize?code_challenge=E9Melho…").underlined(),
            Line::from(""),
            Line::from("⠹ Waiting for the approval…  (the code expires in 4:52)").bold(),
            Line::from(""),
            Line::from("No browser here? Press p and paste the code the page shows.").dim(),
        ])
        .wrap(Wrap { trim: false }),
        inner,
    );
}

/// Pasting the code shown by the browser.
fn paste(frame: &mut Frame) {
    let inner = card(frame, "Paste the code", "Enter sign in · Esc back");
    let [text, code, note] = Layout::vertical([Constraint::Length(4), Constraint::Length(3), Constraint::Fill(1)]).areas(inner);
    frame.render_widget(
        Paragraph::new(vec![
            Line::from("Open this address on any device, sign in and approve:"),
            Line::from("https://files.acme.com/cli/authorize?code_challenge=E9Melho…").underlined(),
            Line::from("Then paste the code the page shows."),
        ])
        .wrap(Wrap { trim: false }),
        text,
    );
    field(frame, code, "Code", "rsA7h63aXQetjzVD7pKsfA1yzQggA868KGK49ScbGr8▏", true);
    frame.render_widget(Line::from("The code works once, for 5 minutes.").dim(), note);
}

/// The vault popup over the explorer.
fn vault(frame: &mut Frame) {
    explorer(frame);
    let inner = popup(frame, 60, 9, "Unlock the vault", "Enter unlock · Esc cancel");
    let [text, input] = Layout::vertical([Constraint::Length(3), Constraint::Length(3)]).areas(inner);
    frame.render_widget(Paragraph::new("prod-web keeps its password in your vault. Enter the master password to connect.").wrap(Wrap { trim: true }), text);
    field(frame, input, "Master password", "••••••••••••▏", true);
}

/// The question to trust a host key.
fn host_key(frame: &mut Frame) {
    explorer(frame);
    let inner = popup(frame, 76, 12, "Trust this server?", "y trust · n refuse · Esc cancel");
    frame.render_widget(
        Paragraph::new(vec![
            Line::from(vec![Span::from("prod-web").bold(), Span::from(" (files.acme.com:22) is not known yet. Check its key with")]),
            Line::from("whoever runs that server before you trust it."),
            Line::from(""),
            Line::from("ssh-ed25519   SHA256:4t8Cq1xR0bYH1n9mYQe7f0R1u0x3pJ2b6cA9dK5sLzE"),
            Line::from(""),
            Line::from("Trusted keys are shared with your workspace and listed in Servers.").dim(),
            Line::from(""),
            Line::from("            [ y Trust and connect ]      [ n Refuse ]").bold(),
        ]),
        inner,
    );
}

/// The confirmation of a copy with files that exist at the destination.
fn copy(frame: &mut Frame) {
    explorer(frame);
    let inner = popup(frame, 72, 13, "Copy 2 files to prod-web", "Enter copy · ↑↓ choose · Esc cancel");
    frame.render_widget(
        Paragraph::new(vec![
            Line::from("site-v2.tar.gz, assets.zip  (260.9 MiB)"),
            Line::from(vec![Span::from("to  "), Span::from("prod-web:/var/www").bold()]),
            Line::from(""),
            Line::from("When a file exists there:"),
            Line::from("  › Stop and ask again").bg(theme::raised()).fg(theme::accent_text()).bold(),
            Line::from("    Replace it"),
            Line::from("    Skip it"),
            Line::from("    Keep the newer"),
            Line::from("    Keep both (name (1).ext)"),
        ]),
        inner,
    );
}

/// The confirmation of a delete.
fn delete(frame: &mut Frame) {
    explorer(frame);
    let inner = popup_in(frame, 64, 10, "Delete from prod-web", "y delete · n cancel", theme::danger());
    frame.render_widget(
        Paragraph::new(vec![
            Line::from("⚠  Delete these 2 entries?").bold().fg(theme::danger()),
            Line::from(""),
            Line::from("   /var/www/site-v1.tar.gz"),
            Line::from("   /var/www/deploy.log"),
            Line::from(""),
            Line::from("This cannot be undone.").fg(theme::danger()),
            Line::from("         [ y Delete ]        [ n Cancel ]").bold(),
        ]),
        inner,
    );
}

/// The help overlay.
fn help(frame: &mut Frame) {
    explorer(frame);
    let inner = popup(frame, 96, 26, "Keys", "type to filter · Esc close");
    let [left, right] = Layout::horizontal([Constraint::Fill(1), Constraint::Fill(1)]).spacing(2).areas(inner);
    let column = |items: &[(&str, &str)]| -> Vec<Line<'static>> {
        items.iter().map(|(k, a)| if a.is_empty() { Line::from(k.to_string()).bold() } else { Line::from(format!("  {k:<16}{a}")) }).collect()
    };
    frame.render_widget(
        Paragraph::new(column(&[
            ("Everywhere", ""),
            ("? F1", "this help"),
            ("/", "commands: /config, /theme…"),
            ("Ctrl+K", "command palette"),
            ("Ctrl+B", "sites panel"),
            ("Ctrl+E", "transfers and log"),
            ("Ctrl+O", "quick connection"),
            ("Tab Shift+Tab", "next, previous panel"),
            ("Alt+1…9", "go to a tab"),
            ("Ctrl+T Ctrl+W", "new tab, close tab"),
            ("Ctrl+C", "cancel, then quit"),
            ("q F10", "quit"),
            ("", ""),
            ("Transfers", ""),
            ("p", "pause or resume"),
            ("x Del", "cancel"),
            ("+ -", "priority"),
        ])),
        left,
    );
    frame.render_widget(
        Paragraph::new(column(&[
            ("Files", ""),
            ("↑↓ j k", "move"),
            ("Enter → l", "open the folder"),
            ("Backspace ← h", "up one level"),
            ("Space Ins", "mark"),
            ("Ctrl+A Esc", "mark all, clear"),
            ("F5 c", "copy to the other panel"),
            ("F7 n", "new folder"),
            ("F2", "rename"),
            ("Del F8", "delete"),
            ("f Ctrl+F", "filter"),
            ("i", "details"),
            (". s r", "hidden, sort, refresh"),
            ("Ctrl+G Ctrl+Y", "go to path, sync browsing"),
            ("o Ctrl+D", "open a site here, disconnect"),
        ])),
        right,
    );
}

/// The command palette.
fn palette(frame: &mut Frame) {
    explorer(frame);
    let area = centered(frame.area(), 70, 12);
    let area = Rect { y: 4, ..area };
    frame.render_widget(Clear, area);
    let block = panel("Ctrl+K", true);
    let inner = block.inner(area);
    frame.render_widget(block, area);
    let [input, list] = Layout::vertical([Constraint::Length(1), Constraint::Fill(1)]).areas(inner);
    frame.render_widget(Line::from("› back▏"), input);
    frame.render_widget(
        Paragraph::new(vec![
            Line::from(" Site      backups (S3)                                 Enter").bg(theme::raised()).fg(theme::accent_text()).bold(),
            Line::from(" Site      backups-old (SFTP)                           Enter"),
            Line::from(" Folder    prod-web:/var/backups                        Enter"),
            Line::from(" Action    Go back up one level                     Backspace"),
            Line::from(" Action    Lock the vault"),
            Line::from(" Web       Open the backups site in the browser"),
        ]),
        list,
    );
}

/// A guided error.
fn error(frame: &mut Frame) {
    explorer(frame);
    let inner = popup_in(frame, 76, 11, "Could not connect to prod-db", "Enter close · c copy the code", theme::danger());
    frame.render_widget(
        Paragraph::new(vec![
            Line::from("The server refused the user name or the password.").bold().fg(theme::danger()),
            Line::from(""),
            Line::from(vec![Span::from("Probable cause  ").dim(), Span::from("the password saved for prod-db changed on the server.")]),
            Line::from(vec![Span::from("What to do      ").dim(), Span::from("update the password of the site in the web, then retry.")]),
            Line::from(""),
            Line::from("Code: remote_auth_failed").dim(),
        ])
        .wrap(Wrap { trim: false }),
        inner,
    );
}

/// The main screen with the bottom panel on its log tab, focused.
fn log(frame: &mut Frame) {
    explorer_with(frame, |frame, area| {
        let block = panel("Transfers │ Log · prod-web", true).title_bottom(Line::from(" [ ] tabs · f follow ").right_aligned());
        frame.render_widget(
            Paragraph::new(vec![
                Line::from("18:40:02  resolving files.acme.com"),
                Line::from("18:40:02  connecting to 203.0.113.24:22"),
                Line::from("18:40:02  key ssh-ed25519 SHA256:4t8Cq1x… trusted"),
                Line::from("18:40:03  signed in as deploy with a password"),
                Line::from("18:40:03  opened /var/www"),
                Line::from("18:40:11  upload site-v2.tar.gz started"),
                Line::from("18:40:14  ● following"),
            ])
            .block(block),
            area,
        );
    });
}

/// The command line of `/`: it takes the place of the status bar, with the commands that match above it, as in Claude Code.
fn commands(frame: &mut Frame) {
    explorer(frame);
    let area = frame.area();
    let items = [
        ("/config", "settings: language, theme, keys of files, updates"),
        ("/theme", "wear another theme, shared with the web"),
        ("/language", "English or Español, shared with the web"),
        ("/workspace", "change the workspace"),
        ("/server", "switch to another server or add one"),
        ("/sessions", "terminal sessions of this account"),
        ("/web", "open this folder in the browser"),
        ("/update", "install the new release"),
        ("/keys", "every key of the screen"),
        ("/logout", "sign this terminal out"),
        ("/quit", "close transferly-tui"),
    ];
    let height = items.len() as u16 + 2;
    let list = Rect::new(area.x, area.height - 1 - height, 64, height);
    frame.render_widget(Clear, list);
    let block = Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(Style::new().fg(theme::accent()))
        .title_bottom(Line::from(" ↑↓ choose · Tab complete · Enter run · Esc close ").right_aligned())
        .style(Style::new().bg(theme::surface()));
    let inner = block.inner(list);
    frame.render_widget(block, list);
    let lines: Vec<Line> = items
        .iter()
        .enumerate()
        .map(|(index, (name, about))| {
            let line = Line::from(vec![Span::from(format!(" {name:<12}")).bold(), Span::from(*about).fg(theme::muted())]);
            if index == 0 { line.bg(theme::raised()).fg(theme::accent_text()) } else { line }
        })
        .collect();
    frame.render_widget(Paragraph::new(lines), inner);
    let input = Rect::new(area.x, area.height - 1, area.width, 1);
    frame.render_widget(Clear, input);
    frame.render_widget(Block::new().style(Style::new().bg(theme::surface())), input);
    frame.render_widget(Line::from(vec![Span::from(" › ").fg(theme::accent_text()).bold(), Span::from("/con▏")]), input);
}

/// The settings of /config: each with its value, and whether it belongs to the account, shared with the web, or to this computer.
fn config(frame: &mut Frame) {
    explorer(frame);
    let inner = popup(frame, 88, 18, "Settings · /config", "↑↓ choose · ←→ change · Esc close");
    let rows = [
        ("Language", "‹ Español ›", "account, web too"),
        ("Theme", "Transferly Dark", "account, web too"),
        ("Colors", "the theme's", "this computer"),
        ("Time zone", "America/Santiago", "account, web too"),
        ("Enter on a file", "copy to the other panel", "this computer"),
        ("Hidden files", "hidden", "this computer"),
        ("Ask before quitting with transfers", "yes", "this computer"),
        ("Check for updates", "once a day", "this computer"),
    ];
    let table = Table::new(
        rows.iter().map(|(name, value, scope)| Row::new(vec![Cell::from(*name), Cell::from(*value).fg(theme::accent_text()), Cell::from(*scope).fg(theme::muted())])),
        [Constraint::Length(36), Constraint::Length(26), Constraint::Fill(1)],
    )
    .row_highlight_style(Style::new().bg(theme::raised()).bold())
    .highlight_symbol("› ");
    let [list, note] = Layout::vertical([Constraint::Fill(1), Constraint::Length(2)]).areas(inner);
    frame.render_stateful_widget(table, list, &mut TableState::new().with_selected(Some(0)));
    frame.render_widget(
        Paragraph::new("The language, the theme and the time zone are those of your account: changing them here changes them in the web too.")
            .fg(theme::muted())
            .wrap(Wrap { trim: true }),
        note,
    );
}

/// The colors of cells without their own color, from the theme the mockups wear.
fn defaults() -> html::Defaults {
    html::Defaults { fg: theme::text(), bg: theme::bg(), muted: theme::muted() }
}
