//! The full-screen F3 preview and its navigation.

mod content;

use std::num::NonZeroUsize;

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::Frame;
use ratatui::layout::{Constraint, Layout, Position, Rect};
use ratatui::style::{Modifier, Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Cell, Paragraph, Row, Table, Wrap};

use super::{Ask, Call, CallError, Home, Look, Side, centered, explain, panel};
use crate::ui::explorer::{Kind, Source, human};
use crate::ui::input::Input;
use crate::ui::theme::Palette;
use crate::ui::widgets::visible_name;
use content::Body;

/// Maximum number of file bytes retained for a preview.
pub(in crate::ui) const PREVIEW_LIMIT: NonZeroUsize = NonZeroUsize::new(4 * 1024 * 1024).unwrap();

/// How many lines or rows Page Up and Page Down move.
const PAGE: usize = 10;

/// A file being fetched or displayed.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct View {
    name: String,
    size: u64,
    content: Option<(content::Kind, Body)>,
    cut: bool,
    row: usize,
    column: usize,
    sheet: usize,
    input: Option<Input>,
    query: String,
    matches: Vec<(usize, usize)>,
    found: usize,
}

impl Home {
    /// Requests the file under the cursor on either panel.
    #[rustfmt::skip]
    pub(super) fn open_preview(&mut self, side: Side) -> Vec<Ask> {
        let panel = self.panel(side);
        let Some(entry) = panel.current().filter(|entry| entry.kind == Kind::File) else {
            return Vec::new();
        };
        let remote = match panel.source() {
            Some(Source::Site { id, .. }) => {
                let Some(workspace) = self.workspace else { return Vec::new() };
                Some((workspace, *id))
            }
            _ => None,
        };
        let path = panel.join(&entry.name);
        self.preview_request = self.preview_request.wrapping_add(1);
        self.preview = Some(Box::new(View::new(&entry.name, entry.size)));
        self.notice = None;
        vec![Ask::Call(Call::Preview {
            request: self.preview_request,
            remote,
            path,
        })]
    }

    /// Displays the reply only while its preview is still open.
    pub(super) fn preview_reply(
        &mut self,
        request: u64,
        outcome: Result<(Vec<u8>, bool), CallError>,
        look: &Look,
    ) -> Vec<Ask> {
        if request == self.preview_request
            && let Some(view) = &mut self.preview
            && let Err(error) = outcome.and_then(|(bytes, cut)| view.load(&bytes, cut))
        {
            self.preview = None;
            self.say(explain(&error, look), true);
        }
        Vec::new()
    }

    /// Handles modal navigation or opens the web app through the command's existing effect.
    pub(super) fn preview_key(&mut self, key: KeyEvent) -> Vec<Ask> {
        let view = self.preview.as_mut().expect("an open preview");
        match view.key(key) {
            Action::Stay => Vec::new(),
            Action::Close => {
                self.preview = None;
                Vec::new()
            }
            Action::Web => vec![self.web()],
        }
    }
}

/// What a preview key asks of the home screen.
enum Action {
    /// Keep the preview open.
    Stay,
    /// Return to the explorer.
    Close,
    /// Open the web application.
    Web,
}

impl View {
    /// A pending preview with its title ready to show.
    fn new(name: &str, size: u64) -> Self {
        Self {
            name: visible_name(name),
            size,
            content: None,
            cut: false,
            row: 0,
            column: 0,
            sheet: 0,
            input: None,
            query: String::new(),
            matches: Vec::new(),
            found: 0,
        }
    }

    /// Parses the received prefix into displayable data.
    fn load(&mut self, bytes: &[u8], cut: bool) -> Result<(), CallError> {
        self.content = Some(content::parse(&self.name, bytes, cut).map_err(unreadable)?);
        self.cut = cut;
        Ok(())
    }

    /// Pastes into the search field while it is open.
    pub(super) fn paste(&mut self, text: &str) {
        if let Some(input) = &mut self.input {
            input.insert(text);
        }
    }

    /// Collects exact matches in reading order and jumps to the first.
    #[rustfmt::skip]
    fn search(&mut self, input: &Input) {
        input.value().clone_into(&mut self.query);
        self.matches.clear();
        self.found = 0;
        let lines: &[String] = match &self.content { Some((_, Body::Text(lines))) if !self.query.is_empty() => lines, _ => &[] };
        for (row, line) in lines.iter().enumerate() {
            self.matches.extend(line.match_indices(self.query.as_str()).map(|(start, _)| (row, start)));
        }
        self.jump();
    }

    /// Selects the current match and scrolls to it, if there is one.
    fn jump(&mut self) {
        if let Some(&(row, start)) = self.matches.get(self.found)
            && let Some((_, Body::Text(lines))) = &self.content
        {
            self.row = row;
            let width = Span::raw(&lines[row][..start]).width();
            self.column = width.saturating_sub(12);
        }
    }

    /// Applies a navigation key, or hands a close or browser request to Home.
    fn key(&mut self, key: KeyEvent) -> Action {
        if let Some(mut input) = self.input.take() {
            match key.code {
                KeyCode::Esc => {}
                KeyCode::Enter => self.search(&input),
                _ => {
                    input.handle(key);
                    self.input = Some(input);
                }
            }
            return Action::Stay;
        }
        if matches!(key.code, KeyCode::Esc | KeyCode::Char('q')) {
            return Action::Close;
        }
        let Some((_, body)) = &self.content else {
            return Action::Stay;
        };
        match body {
            Body::Web if key.code == KeyCode::Char('w') => return Action::Web,
            Body::Web => {}
            Body::Text(lines) => match key.code {
                KeyCode::Char('/') => self.input = Some(Input::with(&self.query)),
                KeyCode::Char('n') if !self.matches.is_empty() => {
                    self.found = (self.found + 1) % self.matches.len();
                    self.jump();
                }
                KeyCode::Left => self.column = self.column.saturating_sub(4),
                KeyCode::Right => self.column = (self.column + 4).min(usize::from(u16::MAX)),
                _ => self.scroll(key.code, lines.len()),
            },
            Body::Tables(tables) => {
                let count = tables.len();
                if count > 1
                    && matches!(
                        key.code,
                        KeyCode::Tab | KeyCode::BackTab | KeyCode::Char('[' | ']')
                    )
                {
                    let forward = key.code == KeyCode::Char(']')
                        || (key.code == KeyCode::Tab
                            && !key.modifiers.contains(KeyModifiers::SHIFT));
                    self.sheet = (self.sheet + if forward { 1 } else { count - 1 }) % count;
                    self.column = 0;
                    self.row = 0;
                } else {
                    let table = &tables[self.sheet];
                    match key.code {
                        KeyCode::Left => self.column = self.column.saturating_sub(1),
                        KeyCode::Right => {
                            self.column =
                                (self.column + 1).min(table.widths.len().saturating_sub(1));
                        }
                        _ => self.scroll(key.code, table.rows.len().saturating_sub(1)),
                    }
                }
            }
        }
        Action::Stay
    }

    /// Moves the selected line or table row, sticking to the ends.
    fn scroll(&mut self, key: KeyCode, rows: usize) {
        let last = rows.saturating_sub(1);
        self.row = match key {
            KeyCode::Up => self.row.saturating_sub(1),
            KeyCode::Down => (self.row + 1).min(last),
            KeyCode::PageUp => self.row.saturating_sub(PAGE),
            KeyCode::PageDown => (self.row + PAGE).min(last),
            KeyCode::Home => 0,
            KeyCode::End => last,
            _ => self.row,
        };
    }

    /// Draws the bordered preview, its content and the matching key hints.
    pub(super) fn render(&self, frame: &mut Frame, look: &Look, spinner: &str) {
        let palette = &look.palette;
        let messages = &look.messages;
        let kind = self
            .content
            .as_ref()
            .map_or("preview.file", |(kind, _)| kind.label());
        let title = format!(
            "{} · {} · {}",
            self.name,
            messages.get(kind),
            human(self.size)
        );
        let block = panel(&title, true, palette).style(Style::new().bg(palette.surface));
        let inside = block.inner(frame.area());
        frame.render_widget(block, frame.area());
        let [body, note, footer] = Layout::vertical([
            Constraint::Fill(1),
            Constraint::Length(u16::from(self.cut)),
            Constraint::Length(1),
        ])
        .areas(inside);
        let keys = match &self.content {
            None => {
                let loading = format!("{spinner} {}", messages.get("preview.loading"));
                render_centered(frame, body, loading);
                "preview.close"
            }
            Some((_, Body::Web)) => {
                render_centered(frame, body, messages.get("preview.unavailable"));
                "preview.webKeys"
            }
            Some((_, Body::Text(lines))) => {
                self.render_text(frame, body, lines, palette);
                "preview.textKeys"
            }
            Some((kind, Body::Tables(tables))) => {
                if *kind == content::Kind::Workbook {
                    let [data, tabs] =
                        Layout::vertical([Constraint::Fill(1), Constraint::Length(1)]).areas(body);
                    self.render_table(frame, data, tables, palette);
                    self.render_tabs(frame, tabs, tables, palette);
                    "preview.sheetKeys"
                } else {
                    self.render_table(frame, body, tables, palette);
                    "preview.tableKeys"
                }
            }
        };
        if self.cut {
            frame.render_widget(
                Line::from(messages.get("preview.cut")).fg(palette.muted),
                note,
            );
        }
        let footer_text = if let Some(input) = &self.input {
            let prefix: String = input.value().chars().take(input.cursor()).collect();
            let cursor = u16::try_from(2 + Span::raw(prefix).width()).unwrap_or(u16::MAX);
            frame.set_cursor_position(Position::new(
                footer.x + cursor.min(footer.width.saturating_sub(1)),
                footer.y,
            ));
            format!(
                "/ {}  ·  {}",
                input.value(),
                messages.get("preview.searchKeys")
            )
        } else if !self.query.is_empty() {
            let found = if self.matches.is_empty() {
                0
            } else {
                self.found + 1
            };
            format!(
                "{}  ·  / {} ({found}/{})",
                messages.get(keys),
                self.query,
                self.matches.len()
            )
        } else {
            messages.get(keys)
        };
        frame.render_widget(Line::from(footer_text).fg(palette.accent_text), footer);
    }

    /// Draws only the visible lines, with a fixed gutter and highlights for matches.
    fn render_text(&self, frame: &mut Frame, area: Rect, lines: &[String], palette: &Palette) {
        let digits = lines.len().to_string().len().max(4);
        let gutter = u16::try_from(digits + 2).unwrap_or(u16::MAX);
        let [numbers, text] =
            Layout::horizontal([Constraint::Length(gutter), Constraint::Fill(1)]).areas(area);
        let height = usize::from(area.height);
        let start = self.row.min(lines.len().saturating_sub(height));
        let mut labels = Vec::new();
        let mut content = Vec::new();
        for (row, line) in lines.iter().enumerate().skip(start).take(height) {
            labels.push(Line::from(format!("{:>digits$}  ", row + 1)).fg(palette.faint));
            content.push(self.text_line(row, line, palette));
        }
        frame.render_widget(Paragraph::new(labels), numbers);
        frame.render_widget(
            Paragraph::new(content).scroll((0, u16::try_from(self.column).unwrap_or(u16::MAX))),
            text,
        );
    }

    /// Highlights every match, emphasizing the one selected by search.
    fn text_line<'a>(&self, row: usize, line: &'a str, palette: &Palette) -> Line<'a> {
        if self.query.is_empty() {
            return Line::from(line);
        }
        let mut spans = Vec::new();
        let mut previous = 0;
        for (start, matched) in line.match_indices(self.query.as_str()) {
            spans.push(Span::raw(&line[previous..start]));
            let current = self.matches.get(self.found) == Some(&(row, start));
            let mark = Span::raw(matched).style(selection(palette));
            spans.push(if current { mark.underlined() } else { mark });
            previous = start + matched.len();
        }
        spans.push(Span::raw(&line[previous..]));
        Line::from(spans)
    }

    /// Draws a header and numbered visible rows with the selected cell highlighted.
    fn render_table(
        &self,
        frame: &mut Frame,
        area: Rect,
        tables: &[content::Table],
        palette: &Palette,
    ) {
        let Some(table) = tables
            .get(self.sheet)
            .filter(|table| !table.rows.is_empty())
        else {
            return;
        };
        let (header, data) = (&table.rows[0], &table.rows[1..]);
        let mut width = area.width.saturating_sub(7);
        let widths = table
            .widths
            .iter()
            .skip(self.column)
            .map_while(|&column| {
                if width == 0 {
                    return None;
                }
                let visible = column.min(width);
                width = width.saturating_sub(visible).saturating_sub(2);
                Some(Constraint::Length(visible))
            })
            .collect::<Vec<_>>();
        let columns = widths.len();
        let widths = std::iter::once(Constraint::Length(5)).chain(widths);
        let header = Row::new(std::iter::once(Cell::from("")).chain(table_cells(
            header,
            self.column,
            columns,
        )))
        .fg(palette.accent_text)
        .bold()
        .bottom_margin(1);
        let height = usize::from(area.height.saturating_sub(2));
        let start = self.row.saturating_sub(height.saturating_sub(1));
        let rows = data
            .iter()
            .enumerate()
            .skip(start)
            .take(height)
            .map(|(index, row)| {
                let label = Cell::from(format!("{:>4}", index + 1)).fg(palette.faint);
                let cells =
                    table_cells(row, self.column, columns)
                        .enumerate()
                        .map(|(column, cell)| {
                            if index == self.row && column == 0 {
                                cell.style(selection(palette))
                            } else {
                                cell
                            }
                        });
                Row::new(std::iter::once(label).chain(cells))
            });
        frame.render_widget(
            Table::new(rows, widths).header(header).column_spacing(2),
            area,
        );
    }

    /// Draws the sheet tabs, keeping the active tab in view on narrow screens.
    fn render_tabs(
        &self,
        frame: &mut Frame,
        area: Rect,
        tables: &[content::Table],
        palette: &Palette,
    ) {
        let mut tabs: Vec<_> = tables
            .iter()
            .enumerate()
            .map(|(index, table)| {
                let tab = Span::raw(format!(" {} ", table.name));
                if index == self.sheet {
                    tab.style(selection(palette))
                } else {
                    tab.fg(palette.muted)
                }
            })
            .collect();
        let width: usize = tabs.iter().take(self.sheet + 1).map(Span::width).sum();
        if width > usize::from(area.width) {
            drop(tabs.drain(..self.sheet));
        }
        frame.render_widget(Line::from(tabs), area);
    }
}

/// Produces a cell for each visible column, including blank fields of short records.
fn table_cells(row: &[String], start: usize, count: usize) -> impl Iterator<Item = Cell<'_>> {
    (start..start + count).map(move |index| Cell::from(row.get(index).map_or("", String::as_str)))
}

/// Styles selected tabs, cells and matches in color and monochrome.
fn selection(palette: &Palette) -> Style {
    if palette.plain {
        Style::new().add_modifier(Modifier::REVERSED | Modifier::BOLD)
    } else {
        Style::new().fg(palette.ink).bg(palette.accent).bold()
    }
}

/// Centers a loading status or a message about opening the file on the web.
fn render_centered(frame: &mut Frame, area: Rect, message: String) {
    frame.render_widget(
        Paragraph::new(message).centered().wrap(Wrap { trim: true }),
        centered(area, area.width, 3),
    );
}

/// Gives workbook failures a stable translation key.
fn unreadable((): ()) -> CallError {
    CallError {
        code: Some("preview_unreadable".to_owned()),
        ..CallError::default()
    }
}
