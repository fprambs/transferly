//! The full-screen F3 preview and its navigation.

mod content;
mod syntax;

use std::num::NonZeroUsize;
use std::ops::Range;

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
    matches: Vec<Hit>,
    found: usize,
}

/// A text byte range or a table cell, ordered by sheet, row and column.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Hit {
    sheet: usize,
    row: usize,
    columns: Range<usize>,
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
        let content = content::parse(&self.name, bytes, cut).map_err(unreadable)?;
        *self = Self {
            content: Some(content),
            cut,
            ..Self::new(&self.name, self.size)
        };
        Ok(())
    }

    /// Pastes into the search field while it is open.
    pub(super) fn paste(&mut self, text: &str) {
        if let Some(input) = &mut self.input {
            input.insert(text);
        }
    }

    /// Finds matches in reading order, searching other sheets only when needed.
    #[rustfmt::skip]
    fn search(&mut self, input: &Input) {
        input.value().clone_into(&mut self.query);
        self.matches.clear();
        self.found = 0;
        let query: String = self.query.chars().flat_map(char::to_lowercase).collect();
        if query.is_empty() {
            return;
        }
        let no_lines = Vec::new();
        let no_tables = Vec::new();
        let lines: &Vec<String> = match &self.content { Some((_, Body::Text(text))) => &text.lines, _ => &no_lines };
        let tables: &Vec<content::Table> = match &self.content { Some((_, Body::Tables(tables))) => tables, _ => &no_tables };
        for (row, line) in lines.iter().enumerate() {
            self.matches.extend(text_matches(line, &query).into_iter().map(|columns| Hit { sheet: 0, row, columns }));
        }
        if !tables.is_empty() {

                table_matches(&mut self.matches, &tables[self.sheet], self.sheet, &query);
                if self.matches.is_empty() {
                    for (sheet, table) in tables
                        .iter()
                        .enumerate()
                        .filter(|(sheet, _)| *sheet != self.sheet)
                    {
                        table_matches(&mut self.matches, table, sheet, &query);
                    }
                }
        }
        self.jump();
    }

    /// Selects the current match and scrolls to it, if there is one.
    fn jump(&mut self) {
        if let Some(hit) = self.matches.get(self.found) {
            if let Some((_, Body::Text(text))) = &self.content {
                self.row = hit.row;
                self.column = Span::raw(&text.lines[hit.row][..hit.columns.start])
                    .width()
                    .saturating_sub(12);
            } else {
                self.sheet = hit.sheet;
                self.row = hit.row.saturating_sub(1);
                self.column = hit.columns.start;
            }
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
        if !matches!(body, Body::Web) {
            match key.code {
                KeyCode::Char('/') => {
                    self.input = Some(Input::with(&self.query));
                    return Action::Stay;
                }
                KeyCode::Char('n' | 'N') if !self.matches.is_empty() => {
                    let count = self.matches.len();
                    let step = if key.code == KeyCode::Char('n') {
                        1
                    } else {
                        count - 1
                    };
                    self.found = (self.found + step) % count;
                    self.jump();
                    return Action::Stay;
                }
                _ => {}
            }
        }
        match body {
            Body::Web if key.code == KeyCode::Char('w') => return Action::Web,
            Body::Web => {}
            Body::Text(text) => match key.code {
                KeyCode::Left => self.column = self.column.saturating_sub(4),
                KeyCode::Right => self.column = (self.column + 4).min(usize::from(u16::MAX)),
                _ => self.scroll(key.code, text.lines.len()),
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
        let kind = match &self.content {
            Some((kind, Body::Text(text))) => text
                .language
                .map_or_else(|| messages.get(kind.label()), str::to_owned),
            Some((kind, _)) => messages.get(kind.label()),
            None => messages.get("preview.file"),
        };
        let title = format!("{} · {kind} · {}", self.name, human(self.size));
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
            Some((_, Body::Text(text))) => {
                self.render_text(frame, body, text, palette);
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
            let status = if self.matches.is_empty() {
                messages.get("preview.noMatches")
            } else {
                messages.fill(
                    "preview.match",
                    &[
                        ("current", &(self.found + 1).to_string()),
                        ("total", &self.matches.len().to_string()),
                    ],
                )
            };
            format!("{status}  ·  {}", messages.get(keys))
        } else {
            messages.get(keys)
        };
        frame.render_widget(Line::from(footer_text).fg(palette.accent_text), footer);
    }

    /// Draws only the visible lines, with a fixed gutter and highlights for matches.
    fn render_text(
        &self,
        frame: &mut Frame,
        area: Rect,
        source: &content::Text,
        palette: &Palette,
    ) {
        let lines = &source.lines;
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
            content.push(self.text_line(
                row,
                line,
                source.highlights.get(row).map_or(&[], Vec::as_slice),
                palette,
            ));
        }
        frame.render_widget(Paragraph::new(labels), numbers);
        frame.render_widget(
            Paragraph::new(content).scroll((0, u16::try_from(self.column).unwrap_or(u16::MAX))),
            text,
        );
    }

    /// Combines syntax colors and search highlights for one visible line.
    fn text_line<'a>(
        &self,
        row: usize,
        line: &'a str,
        highlights: &[syntax::Token],
        palette: &Palette,
    ) -> Line<'a> {
        let first = self.matches.partition_point(|hit| hit.row < row);
        let last = first + self.matches[first..].partition_point(|hit| hit.row == row);
        let matches = &self.matches[first..last];
        if (highlights.is_empty() || palette.plain) && matches.is_empty() {
            return Line::from(line);
        }
        let mut boundaries = vec![0, line.len()];
        let highlights = if palette.plain { &[] } else { highlights };
        boundaries.extend(highlights.iter().map(|token| token.start));
        for hit in matches {
            boundaries.extend([hit.columns.start, hit.columns.end]);
        }
        boundaries.sort_unstable();
        boundaries.dedup();
        let mut spans = Vec::new();
        let mut syntax = highlights.iter().peekable();
        let mut role = None;
        let mut matched = 0;
        for pair in boundaries.windows(2) {
            let start = pair[0];
            while let Some(entry) = syntax.next_if(|entry| entry.start <= start) {
                role = Some(entry.role);
            }
            while matches
                .get(matched)
                .is_some_and(|hit| hit.columns.end <= start)
            {
                matched += 1;
            }
            let mut span = Span::raw(&line[start..pair[1]]);
            if matches
                .get(matched)
                .is_some_and(|hit| hit.columns.start <= start)
            {
                span = span.style(selection(palette));
                if first + matched == self.found {
                    span = span.underlined();
                }
            } else if let Some(role) = role {
                span = span.fg(role.color(palette));
            }
            spans.push(span);
        }
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
        let header = Row::new(
            std::iter::once(Cell::from("")).chain(self.table_cells(header, 0, columns, palette)),
        )
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
                let cells = self.table_cells(row, index + 1, columns, palette);
                Row::new(std::iter::once(label).chain(cells))
            });
        frame.render_widget(
            Table::new(rows, widths).header(header).column_spacing(2),
            area,
        );
    }

    /// Styles visible table cells, including the header, by their full table coordinates.
    fn table_cells<'a>(
        &'a self,
        row: &'a [String],
        index: usize,
        count: usize,
        palette: &'a Palette,
    ) -> impl Iterator<Item = Cell<'a>> {
        let searching_sheet = self
            .matches
            .get(self.found)
            .is_some_and(|hit| hit.sheet == self.sheet);
        (self.column..self.column + count).map(move |column| {
            let cell = Cell::from(row.get(column).map_or("", String::as_str));
            let matched = self
                .matches
                .binary_search_by_key(&(self.sheet, index, column), |hit| {
                    (hit.sheet, hit.row, hit.columns.start)
                });
            match matched {
                Ok(found) if found == self.found => cell.style(selection(palette)),
                Ok(_) => cell.style(Style::new().fg(palette.accent_text).underlined()),
                Err(_) if !searching_sheet && index == self.row + 1 && column == self.column => {
                    cell.style(selection(palette))
                }
                Err(_) => cell,
            }
        })
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

/// Finds original byte ranges on character boundaries after Unicode lowercasing.
fn text_matches(line: &str, query: &str) -> Vec<Range<usize>> {
    let mut offsets = Vec::new();
    let mut folded = String::with_capacity(line.len());
    for (original, character) in line.char_indices() {
        offsets.push((folded.len(), original));
        folded.extend(character.to_lowercase());
    }
    offsets.push((folded.len(), line.len()));
    folded
        .match_indices(query)
        .filter_map(|(start, matched)| {
            let first = offsets
                .binary_search_by_key(&start, |&(folded, _)| folded)
                .ok()?;
            let end = start + matched.len();
            let last = offsets
                .binary_search_by_key(&end, |&(folded, _)| folded)
                .ok()?;
            Some(offsets[first].1..offsets[last].1)
        })
        .collect()
}

/// Adds one match per cell containing the folded query.
fn table_matches(hits: &mut Vec<Hit>, table: &content::Table, sheet: usize, query: &str) {
    for (row, cells) in table.rows.iter().enumerate() {
        for (column, cell) in cells.iter().enumerate() {
            let folded: String = cell.chars().flat_map(char::to_lowercase).collect();
            if folded.contains(query) {
                hits.push(Hit {
                    sheet,
                    row,
                    columns: column..column + 1,
                });
            }
        }
    }
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

#[cfg(test)]
mod tests {
    use super::*;

    /// Offsets still refer to the original text when lowercase uses more bytes.
    #[test]
    fn maps_lowercase_matches_to_original_bytes() {
        assert!(matches!(
            text_matches("aİB", "i\u{307}").as_slice(),
            [matched] if *matched == (1..3)
        ));
        assert!(matches!(
            text_matches("aİB", "b").as_slice(),
            [matched] if *matched == (3..4)
        ));
        assert_eq!(text_matches("aİB", "i"), Vec::<Range<usize>>::new());
        assert_eq!(text_matches("aİB", "\u{307}"), Vec::<Range<usize>>::new());
    }
}
