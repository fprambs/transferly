//! Mockups of T7 (docs/ROADMAP.md): the mouse, the bars of termscp, the forms of termirs, the window of transfers and the previews, for
//! approval before they are built.

use ratatui::Frame;
use ratatui::layout::{Constraint, Layout, Rect};
use ratatui::style::{Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, BorderType, Cell, Clear, LineGauge, Paragraph, Row, Table, TableState};

use super::{centered, explorer_panel, local_rows, panel, popup, remote_rows, sites, tabs, theme};

/// A key and its action, as the bottom row of termscp shows them: the key on the accent, the action in plain text.
fn key_row(keys: &[(&str, &str)]) -> Line<'static> {
    let mut spans = Vec::new();
    for (index, (key, action)) in keys.iter().enumerate() {
        if index > 0 {
            spans.push(Span::raw(" │ ").fg(theme::muted()));
        }
        spans.push(Span::from(format!("{key} ")).fg(theme::ink()).bg(theme::accent()).bold());
        spans.push(Span::from(format!(" {action}")).fg(theme::text()));
    }
    Line::from(spans)
}

/// A setting of a panel on the panel border: its name, and its value as a chip that a click or its key changes.
fn chip(name: &str, value: &str, on: bool) -> Vec<Span<'static>> {
    let value = if on {
        Span::from(value.to_owned()).fg(theme::ink()).bg(theme::accent())
    } else {
        Span::from(value.to_owned()).fg(theme::text()).bg(theme::raised())
    };
    vec![Span::from(format!("{name} ")).fg(theme::muted()), value]
}

/// The status line of the session, without the keys, which move to their own row.
fn session(frame: &mut Frame, area: Rect) {
    frame.render_widget(Block::new().style(Style::new().bg(theme::surface()).fg(theme::muted())), area);
    frame.render_widget(Line::from(" ana@files.acme.com · Acme Web · vault unlocked · ⇅ 2 · 41%"), area);
}

/// Draws the settings on the bottom border of an explorer panel.
fn explorer_settings(frame: &mut Frame, area: Rect, sort: &str) {
    let spans = [
        chip("Sort", sort, true),
        vec![Span::raw(" · ").fg(theme::muted())],
        chip("Hidden", "no", false),
        vec![Span::raw(" · ").fg(theme::muted())],
        chip("Filter", "—", false),
        vec![Span::raw(" ")],
    ].concat();
    frame.render_widget(
        Line::from(spans).right_aligned().bg(theme::surface()),
        Rect::new(area.x + 1, area.bottom() - 1, area.width - 2, 1),
    );
}

/// The explorer with settings on the borders and a separate row of clickable shortcuts.
pub fn bars(frame: &mut Frame) {
    let [top, body, _, keys] = Layout::vertical([
        Constraint::Length(1),
        Constraint::Fill(1),
        Constraint::Length(1),
        Constraint::Length(1),
    ])
    .areas(frame.area());
    frame.render_widget(
        Line::from(" transferly-tui 1.4.0 · files.acme.com · ana@acme.com · Acme Web · vault unlocked · ⇅ 2 · 41% ")
            .right_aligned()
            .bg(theme::surface()),
        top,
    );
    frame.render_widget(
        Line::from(" 1 prod-web ").fg(theme::ink()).bg(theme::accent()).bold(),
        Rect::new(top.x, top.y, 12, 1),
    );
    let [side, left, right] = Layout::horizontal([Constraint::Length(24), Constraint::Fill(1), Constraint::Fill(1)]).areas(body);
    sites(frame, side, false);
    explorer_panel(frame, left, "Local disk", "/home/ana", &local_rows(), 2, false);
    explorer_panel(frame, right, "prod-web · SFTP", "/var/www", &remote_rows(), 3, true);
    explorer_settings(frame, left, "Name ↑");
    explorer_settings(frame, right, "Date ↓");
    frame.render_widget(
        key_row(&[("F1", "Help"), ("Tab", "Panel"), ("Enter", "Open"), ("F3", "View"), ("F4", "Edit"), ("F5", "Copy"), ("F6", "Rename"), ("F7", "Folder"), ("F8", "Delete"), ("F9", "Menu"), ("F10", "Quit")])
            .style(Style::new().bg(theme::surface())),
        keys,
    );
}

/// The menu of a right click on an entry, next to it: every action with its key, the danger ones in red.
pub fn context_menu(frame: &mut Frame) {
    bars(frame);
    let area = Rect::new(78, 9, 30, 12);
    frame.render_widget(Clear, area);
    let block = Block::bordered().border_type(BorderType::Rounded).border_style(Style::new().fg(theme::accent())).style(Style::new().bg(theme::surface()));
    let inner = block.inner(area);
    frame.render_widget(block, area);
    let item = |action: &str, key: &str, selected: bool| {
        let line = Line::from(vec![Span::from(format!(" {action:<18}")), Span::from(format!("{key:>8} ")).fg(theme::muted())]);
        if selected { line.bg(theme::raised()).fg(theme::accent_text()).bold() } else { line }
    };
    let lines = vec![
        item("Open", "Enter", false),
        item("View", "F3", true),
        item("Edit", "F4", false),
        item("Copy to Local", "F5", false),
        item("Rename", "F2", false),
        item("Copy the path", "y", false),
        item("Details", "i", false),
        Line::from(" ────────────────────────────").fg(theme::line()),
        Line::from(vec![Span::from(" Delete            ").fg(theme::danger()), Span::from("     Del ").fg(theme::muted())]),
    ];
    frame.render_widget(Paragraph::new(lines), inner);
}

/// The sites as a table, as termirs lists its connections: search on top, the columns that tell sites apart, keys at the bottom.
pub fn site_list(frame: &mut Frame) {
    let [top, body, keys, bar] = Layout::vertical([Constraint::Length(1), Constraint::Fill(1), Constraint::Length(1), Constraint::Length(1)]).areas(frame.area());
    tabs(frame, top);
    let block = panel("Sites · Acme Web (9)", true).title_bottom(Line::from(" Search: prod▏ ").fg(theme::accent_text()));
    let rows = vec![
        ["Production / prod-web", "web1.acme.com", "22", "deploy", "SFTP", "today 18:40"],
        ["Production / prod-db", "db1.acme.com", "22", "deploy", "SFTP", "yesterday"],
        ["Staging / staging", "stg.acme.com", "2222", "deploy", "SFTP", "11-20"],
        ["Customers / globex", "ftp.globex.com", "21", "acme", "FTPS", "10-31"],
        ["backups", "s3.eu-west-1", "443", "AKIA…", "S3", "11-24"],
        ["nas", "nas.home.lan", "443", "ana", "WebDAV", "11-02"],
    ];
    let rows: Vec<Row> = rows.into_iter().map(|cells| Row::new(cells.map(Cell::from))).collect();
    let table = Table::new(rows, [Constraint::Fill(3), Constraint::Fill(2), Constraint::Length(6), Constraint::Length(10), Constraint::Length(8), Constraint::Length(12)])
        .header(Row::new(["Name", "Host", "Port", "User", "Kind", "Last used"]).fg(theme::faint()).bold())
        .row_highlight_style(Style::new().bg(theme::raised()).fg(theme::accent_text()).bold())
        .highlight_symbol("› ")
        .block(block);
    frame.render_stateful_widget(table, body, &mut TableState::new().with_selected(Some(0)));
    frame.render_widget(key_row(&[("Enter", "Open"), ("N", "New"), ("E", "Edit"), ("D", "Delete"), ("C", "Duplicate"), ("M", "Move"), ("/", "Search"), ("Ctrl+L", "Import")]).style(Style::new().bg(theme::surface())), keys);
    session(frame, bar);
}

/// A field of a form, as termirs frames each one: its label as the title of its border, the value or a hint inside.
fn form_field(frame: &mut Frame, area: Rect, label: &str, value: &str, hint: bool, focused: bool) {
    let color = if focused { theme::accent() } else { theme::line() };
    let block = Block::bordered().border_type(BorderType::Rounded).border_style(Style::new().fg(color)).title(Line::from(format!(" {label} ")).fg(if focused { theme::accent_text() } else { theme::muted() }));
    let text = if hint { Line::from(value.to_owned()).fg(theme::faint()).italic() } else { Line::from(format!("{value}{}", if focused { "▏" } else { "" })) };
    frame.render_widget(Paragraph::new(text).block(block), area);
}

/// A new site, as termirs adds a connection: the fields of the connector one under the other, the test of the connection and the keys.
pub fn new_site(frame: &mut Frame) {
    site_list(frame);
    let inner = popup(frame, 76, 30, "New site · SFTP", "Tab next · Ctrl+T test · Enter save · Esc cancel");
    let fields = Layout::vertical([Constraint::Length(3); 8].into_iter().chain([Constraint::Length(2)])).split(inner);
    frame.render_widget(Line::from(vec![Span::from(" Kind  "), Span::from(" ‹ SFTP › ").fg(theme::ink()).bg(theme::accent()).bold(), Span::from("   FTP/FTPS  S3  WebDAV  Azure  Google Drive").fg(theme::muted())]), fields[0].inner(ratatui::layout::Margin::new(0, 1)));
    let [host, port] = Layout::horizontal([Constraint::Fill(1), Constraint::Length(12)]).areas(fields[1]);
    form_field(frame, host, "Host", "web1.acme.com", false, false);
    form_field(frame, port, "Port", "22", false, false);
    form_field(frame, fields[2], "Username", "deploy", false, true);
    form_field(frame, fields[3], "Password", "Kept in your vault", true, false);
    form_field(frame, fields[4], "Private key", "~/.ssh/id_ed25519 · or leave empty to use the password", true, false);
    form_field(frame, fields[5], "Name", "prod-web", false, false);
    form_field(frame, fields[6], "Start in", "/var/www", false, false);
    form_field(frame, fields[7], "Collection", "Production", false, false);
    frame.render_widget(
        Line::from(vec![Span::from(" ✓ ").fg(theme::ok()).bold(), Span::from("Connected in 0.4 s · host key SHA256:3vF…q8k trusted").fg(theme::ok())]),
        fields[8],
    );
}

/// Importing `~/.ssh/config`: its hosts with a mark, what each becomes, and which exist already.
pub fn import(frame: &mut Frame) {
    site_list(frame);
    let inner = popup(frame, 84, 16, "Import ~/.ssh/config (5 hosts)", "Space mark · a all · Enter import 3 · Esc cancel");
    let rows = vec![
        Row::new(["✓", "web1", "deploy@web1.acme.com:22", "key ~/.ssh/id_ed25519", "new"]),
        Row::new(["✓", "db1", "deploy@db1.acme.com:22", "jump via web1", "new"]),
        Row::new(["✓", "nas", "ana@nas.home.lan:2222", "password, asked", "new"]),
        Row::new([" ", "github.com", "git@github.com:22", "key", "skipped: not a file server"]),
        Row::new([" ", "stg", "deploy@stg.acme.com:2222", "key", "exists as staging"]),
    ]
    .into_iter()
    .map(|row| row.fg(theme::text()))
    .collect::<Vec<_>>();
    let table = Table::new(rows, [Constraint::Length(2), Constraint::Length(12), Constraint::Fill(2), Constraint::Fill(2), Constraint::Fill(2)])
        .header(Row::new(["", "Host", "Address", "Sign in", "Becomes"]).fg(theme::faint()).bold())
        .row_highlight_style(Style::new().bg(theme::raised()).bold());
    frame.render_stateful_widget(table, inner, &mut TableState::new().with_selected(Some(1)));
}

/// The window of transfers in front: the total on top, each transfer with its gauge, speed and time left.
pub fn transfer_window(frame: &mut Frame) {
    bars(frame);
    let inner = popup(frame, 104, 12, "Transfers · 2 of 5 · 41% · 18.4 MiB/s · 0:42 left", "Esc hide · T show again · p pause · x cancel · Ctrl+E queue");
    let items = [
        ("↑", "site-v2.tar.gz", "prod-web", 1.0, "48.2 MiB", "done", ""),
        ("↑", "assets.zip", "prod-web", 0.62, "131.9 / 212.7 MiB", "11.2 MiB/s", "0:07"),
        ("↓", "backup-2026-11-24.sql.gz", "prod-db", 0.18, "9.4 / 52.0 MiB", "7.2 MiB/s", "0:06"),
        ("⇄", "reports/", "backups → nas", 0.0, "", "waiting", ""),
        ("↓", "secret.key", "prod-web", 0.0, "", "permission denied", ""),
    ];
    let rows = Layout::vertical([Constraint::Length(2); 5]).split(inner);
    for ((arrow, name, site, ratio, done, speed, left), row) in items.iter().zip(rows.iter()) {
        let [a, n, g, d, s, l] = Layout::horizontal([Constraint::Length(2), Constraint::Fill(2), Constraint::Fill(2), Constraint::Length(18), Constraint::Length(18), Constraint::Length(5)]).spacing(1).areas(Rect::new(row.x, row.y, row.width, 1));
        let color = if speed.starts_with("permission") { theme::danger() } else if *speed == "done" { theme::ok() } else { theme::accent() };
        frame.render_widget(Line::from(*arrow).bold(), a);
        frame.render_widget(Line::from(vec![Span::from(*name), Span::from(format!("  {site}")).fg(theme::muted())]), n);
        frame.render_widget(LineGauge::default().filled_symbol("█").unfilled_symbol("░").filled_style(Style::new().fg(color)).unfilled_style(Style::new().fg(theme::line())).ratio(*ratio).label(format!("{:>3}%", (ratio * 100.0) as u8)), g);
        frame.render_widget(Line::from(*done).right_aligned(), d);
        frame.render_widget(Line::from(*speed).right_aligned().fg(if color == theme::danger() { theme::danger() } else { theme::text() }), s);
        frame.render_widget(Line::from(*left).right_aligned(), l);
    }
}

/// A preview over the explorer: the frame with the name, what the file is, and the keys.
fn preview(frame: &mut Frame, title: &str, keys: &str) -> Rect {
    bars(frame);
    let area = centered(frame.area(), frame.area().width - 6, frame.area().height - 4);
    frame.render_widget(Clear, area);
    let block = Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(Style::new().fg(theme::accent()))
        .title(Line::from(format!(" {title} ")).fg(theme::accent_text()).bold())
        .title_bottom(Line::from(format!(" {keys} ")).right_aligned())
        .style(Style::new().bg(theme::surface()));
    let inner = block.inner(area);
    frame.render_widget(block, area);
    inner
}

/// A table of a CSV or a sheet: numbered rows, a header, the cell under the cursor highlighted.
fn sheet(frame: &mut Frame, area: Rect, header: &[&str], rows: &[Vec<&str>]) {
    let widths: Vec<Constraint> = std::iter::once(Constraint::Length(5)).chain(header.iter().map(|_| Constraint::Fill(1))).collect();
    let rows: Vec<Row> = rows
        .iter()
        .enumerate()
        .map(|(at, cells)| {
            let mut all = vec![Cell::from(format!("{:>4}", at + 1)).fg(theme::faint())];
            all.extend(cells.iter().enumerate().map(|(column, cell)| {
                let cell = Cell::from(*cell);
                if at == 2 && column == 3 { cell.bg(theme::accent()).fg(theme::ink()).bold() } else { cell }
            }));
            Row::new(all)
        })
        .collect();
    let header = Row::new(std::iter::once(Cell::from("")).chain(header.iter().map(|name| Cell::from(*name)))).fg(theme::accent_text()).bold().bottom_margin(1);
    frame.render_widget(Table::new(rows, widths).header(header).column_spacing(2).row_highlight_style(Style::new().bg(theme::raised())), area);
}

/// The sales of the previews.
fn sales() -> Vec<Vec<&'static str>> {
    vec![
        vec!["2026-11-01", "Acme", "Widgets", "1,240", "CLP 3.100.000", "paid"],
        vec!["2026-11-02", "Globex", "Bolts", "88", "CLP 264.000", "paid"],
        vec!["2026-11-03", "Initech", "Gears", "512", "CLP 1.536.000", "due"],
        vec!["2026-11-04", "Umbrella", "Widgets", "77", "CLP 192.500", "paid"],
        vec!["2026-11-05", "Hooli", "Springs", "1,003", "CLP 2.006.000", "late"],
        vec!["2026-11-06", "Acme", "Bolts", "430", "CLP 1.290.000", "paid"],
    ]
}

/// F3 on a CSV: a table with its columns aligned, the size and the separator it found.
pub fn preview_csv(frame: &mut Frame) {
    let inner = preview(frame, "ventas.csv · 1,204 rows · 6 columns · separator ;", "↑↓←→ move · / search · Enter copy cell · Esc close");
    sheet(frame, inner, &["Date", "Customer", "Product", "Units", "Total", "State"], &sales());
}

/// F3 on a spreadsheet: the sheet as a table and its sheets as tabs at the bottom.
pub fn preview_xlsx(frame: &mut Frame) {
    let inner = preview(frame, "informe-noviembre.xlsx · Ventas · A1:F1204", "Tab sheet · ↑↓←→ move · / search · Esc close");
    let [table, sheets] = Layout::vertical([Constraint::Fill(1), Constraint::Length(1)]).areas(inner);
    sheet(frame, table, &["A · Date", "B · Customer", "C · Product", "D · Units", "E · Total", "F · State"], &sales());
    frame.render_widget(
        Line::from(vec![
            Span::from(" Ventas ").fg(theme::ink()).bg(theme::accent()).bold(),
            Span::from("  Clientes  ").fg(theme::muted()),
            Span::from("  Resumen  ").fg(theme::muted()),
            Span::from("  · formulas show their value; the first 5,000 rows").fg(theme::faint()),
        ]),
        sheets,
    );
}

/// F3 on a text or code file: numbered lines, the search found, and how much of the file was read.
pub fn preview_text(frame: &mut Frame) {
    let inner = preview(frame, "deploy.sh · 2.1 KiB · UTF-8", "↑↓ PgUp PgDn scroll · / search: rsync (2 of 3) · n next · Esc close");
    let code = [
        "#!/usr/bin/env bash",
        "set -euo pipefail",
        "",
        "TARGET=/var/www/current",
        "RELEASE=\"/var/www/releases/$(date +%Y%m%d%H%M)\"",
        "",
        "mkdir -p \"$RELEASE\"",
        "rsync -a --delete build/ \"$RELEASE/\"",
        "ln -sfn \"$RELEASE\" \"$TARGET\"",
        "systemctl reload nginx",
    ];
    let lines: Vec<Line> = code
        .iter()
        .enumerate()
        .map(|(at, line)| {
            let mut spans = vec![Span::from(format!("{:>4}  ", at + 1)).fg(theme::faint())];
            if let Some(start) = line.find("rsync") {
                spans.push(Span::from(line[..start].to_owned()));
                spans.push(Span::from("rsync").fg(theme::ink()).bg(theme::accent()).bold());
                spans.push(Span::from(line[start + 5..].to_owned()));
            } else {
                spans.push(Span::from((*line).to_owned()));
            }
            Line::from(spans)
        })
        .collect();
    frame.render_widget(Paragraph::new(lines), inner);
}
