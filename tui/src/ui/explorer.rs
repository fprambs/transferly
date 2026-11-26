//! A panel of the explorer (docs/design/terminal-client.md, Explorer panels): the local disk or a site, a folder listed as a table of
//! name, size and date, moved through with the arrows and vim keys, sorted, filtered, with marks, and the forms to create a folder,
//! rename, go to a path and the confirmation to delete. Like the rest of the interface it is pure: keys and listings change it, and it
//! returns what to do with the files, which the main screen sends to the disk or to the server.

use std::collections::HashSet;
use std::sync::Arc;

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use jiff::Timestamp;
use jiff::tz::TimeZone;
use ratatui::Frame;
use ratatui::layout::{Constraint, Layout, Position, Rect};
use ratatui::style::{Modifier, Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Paragraph, Wrap};
use uuid::Uuid;

use super::app::SPINNER;
use super::bars::Bar;
use super::input::Input;
use super::look::Look;
use super::mouse::{first_row, footer_key, row};
use super::widgets::{centered, error_line, field, highlight, panel, popup, visible_name};

/// How many rows Page Up and Page Down move.
const PAGE: usize = 10;

/// At 120 ms per tick, the cancellation hint appears after about three seconds.
const CANCEL_AFTER: usize = 25;

/// What a panel shows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Source {
    /// The disk of this computer.
    Local,
    /// A site of the workspace.
    Site {
        /// The site.
        id: Uuid,
        /// Its name.
        name: String,
    },
}

impl Source {
    /// The site shown, if it is one.
    #[must_use]
    pub fn site(&self) -> Option<Uuid> {
        if let Self::Site { id, .. } = self {
            Some(*id)
        } else {
            None
        }
    }
}

/// What an entry is.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    /// A folder.
    Folder,
    /// A file.
    File,
    /// A symbolic link.
    Link,
    /// Anything else: a device, a socket.
    Other,
}

/// An entry of a folder.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    /// Its name.
    pub name: String,
    /// What it is.
    pub kind: Kind,
    /// Its size in bytes.
    pub size: u64,
    /// When it was last changed, if known.
    pub modified: Option<Timestamp>,
}

/// What the rows are sorted by.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Sort {
    /// The name, ignoring case.
    Name,
    /// The size.
    Size,
    /// The date of the last change.
    Date,
}

/// What to do with the files of a panel.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Op {
    /// List a folder.
    List(String),
    /// Find the folder to start in.
    Home,
    /// Create a folder.
    Mkdir(String),
    /// Rename or move an entry.
    Rename {
        /// Its path.
        from: String,
        /// Its new path.
        to: String,
    },
    /// Delete entries, each with whether it is a folder, deleted with everything in it.
    Delete(Vec<(String, bool)>),
    /// Close the connection to the site.
    Disconnect,
}

/// What a panel asks of the main screen.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PanelAsk {
    /// Do something with its files.
    Op(Op),
    /// Enter on a file: copy it to the other panel, or show its details, as the settings say.
    Activate(Entry),
}

/// A form over a panel.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Form {
    /// The name of a new folder.
    Mkdir(Input),
    /// The new name of an entry.
    Rename {
        /// The entry.
        from: String,
        /// Its new name.
        input: Input,
    },
    /// A path to go to.
    Goto(Input),
}

/// A row of the panel: the folder above, or an entry.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Row<'a> {
    /// The folder above.
    Up,
    /// An entry.
    Entry(&'a Entry),
}

/// Identifies one listing, including when the same path is requested again.
#[derive(Debug, Clone)]
pub struct ListingTicket(Arc<()>);

impl PartialEq for ListingTicket {
    fn eq(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.0, &other.0)
    }
}

impl Eq for ListingTicket {}

/// A folder being listed and the time its loading box has been visible.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Pending {
    path: String,
    ticks: usize,
    ticket: ListingTicket,
}

/// A panel of the explorer.
#[derive(Debug, Clone, PartialEq, Eq)]
#[allow(
    clippy::struct_excessive_bools,
    reason = "independent states of the panel: filtering, order, hidden files, loading"
)]
pub struct Panel {
    source: Option<Source>,
    path: String,
    pending: Option<Pending>,
    select: Option<String>,
    entries: Vec<Entry>,
    cursor: usize,
    marks: HashSet<String>,
    filter: Input,
    filtering: bool,
    sort: Sort,
    descending: bool,
    hidden: bool,
    loading: bool,
    error: Option<String>,
    form: Option<Form>,
    confirm: Option<Vec<(String, bool)>>,
}

impl Panel {
    /// An empty panel, showing hidden files or not.
    #[must_use]
    pub fn new(hidden: bool) -> Self {
        Self {
            source: None,
            path: String::new(),
            pending: None,
            select: None,
            entries: Vec::new(),
            cursor: 0,
            marks: HashSet::new(),
            filter: Input::default(),
            filtering: false,
            sort: Sort::Name,
            descending: false,
            hidden,
            loading: false,
            error: None,
            form: None,
            confirm: None,
        }
    }

    /// What the panel shows.
    #[must_use]
    pub fn source(&self) -> Option<&Source> {
        self.source.as_ref()
    }

    /// The folder listed.
    #[must_use]
    pub fn path(&self) -> &str {
        &self.path
    }

    /// Whether a form, a confirmation or the filter takes the keys.
    #[must_use]
    pub fn captures(&self) -> bool {
        !self.listing() && (self.form.is_some() || self.confirm.is_some() || self.filtering)
    }

    /// Whether a folder listing is pending.
    pub(super) fn listing(&self) -> bool {
        self.pending.is_some()
    }

    /// The identity of the pending listing, for matching its reply.
    pub(super) fn listing_ticket(&self) -> Option<ListingTicket> {
        self.pending.as_ref().map(|pending| pending.ticket.clone())
    }

    /// Advances the spinner and the delay before showing the cancellation hint.
    pub(super) fn tick(&mut self) {
        if let Some(pending) = &mut self.pending {
            pending.ticks += 1;
            if pending.ticks == CANCEL_AFTER + SPINNER.len() {
                pending.ticks = CANCEL_AFTER;
            }
        }
    }

    /// Clears the marks.
    pub fn clear_marks(&mut self) {
        self.marks.clear();
    }

    /// The entries of the folder listed.
    #[must_use]
    pub fn entries(&self) -> &[Entry] {
        &self.entries
    }

    /// What an action works on: the marked entries, or the one under the cursor.
    #[must_use]
    pub fn selection(&self) -> Vec<Entry> {
        if self.marks.is_empty() {
            return self.current().into_iter().collect();
        }
        self.entries
            .iter()
            .filter(|entry| self.marks.contains(&entry.name))
            .cloned()
            .collect()
    }

    /// The names of the marked entries.
    #[must_use]
    pub fn marked(&self) -> Vec<String> {
        let mut names: Vec<String> = self.marks.iter().cloned().collect();
        names.sort();
        names
    }

    /// Shows a source, starting in its home folder.
    pub fn open(&mut self, source: Source) -> Op {
        *self = Self {
            source: Some(source),
            loading: true,
            ..Self::new(self.hidden)
        };
        Op::Home
    }

    /// Shows nothing.
    pub fn close(&mut self) {
        *self = Self::new(self.hidden);
    }

    /// Lists a folder, keeping the current one in view until it arrives.
    fn go(&mut self, path: String) -> Vec<PanelAsk> {
        self.pending = Some(Pending {
            path: path.clone(),
            ticks: 0,
            ticket: ListingTicket(Arc::new(())),
        });
        self.loading = true;
        self.error = None;
        vec![PanelAsk::Op(Op::List(path))]
    }

    /// The home folder arrived, or why not.
    pub fn homed(&mut self, outcome: Result<String, String>) -> Vec<PanelAsk> {
        match outcome {
            Ok(path) => self.go(path),
            Err(reason) => {
                self.loading = false;
                self.error = Some(reason);
                Vec::new()
            }
        }
    }

    /// A folder was listed, or why not; an answer for a folder no longer asked for is dropped.
    pub fn listed(&mut self, path: &str, outcome: Result<Vec<Entry>, String>) {
        if self.pending.as_ref().map(|pending| pending.path.as_str()) != Some(path) {
            return;
        }
        self.pending = None;
        self.loading = false;
        match outcome {
            Ok(entries) => {
                if self.path != path {
                    self.marks.clear();
                    self.filter = Input::default();
                }
                path.clone_into(&mut self.path);
                self.entries = entries;
                let wanted = self.select.take();
                let rows = self.rows();
                self.cursor = rows
                    .iter()
                    .position(|row| matches!(row, Row::Entry(entry) if Some(&entry.name) == wanted.as_ref()))
                    .unwrap_or(0)
                    .min(rows.len().saturating_sub(1));
            }
            Err(reason) => self.error = Some(reason),
        }
    }

    /// Tries again what failed: the folder in view, or the home folder when none was listed yet.
    pub fn retry(&mut self) -> Vec<PanelAsk> {
        if !self.path.is_empty() {
            return self.go(self.path.clone());
        }
        self.loading = true;
        self.error = None;
        vec![PanelAsk::Op(Op::Home)]
    }

    /// A change of the files ended: the folder is listed again, or the reason is shown.
    pub fn changed(&mut self, outcome: Result<(), String>) -> Vec<PanelAsk> {
        match outcome {
            Ok(()) => self.go(self.path.clone()),
            Err(reason) => {
                self.loading = false;
                self.error = Some(reason);
                Vec::new()
            }
        }
    }

    /// Whether the source is the disk of this computer, whose paths follow the system.
    fn local(&self) -> bool {
        self.source == Some(Source::Local)
    }

    /// The path of an entry of the folder.
    #[must_use]
    pub fn join(&self, name: &str) -> String {
        if self.local() {
            std::path::Path::new(&self.path)
                .join(name)
                .to_string_lossy()
                .into_owned()
        } else if self.path.ends_with('/') {
            format!("{}{name}", self.path)
        } else {
            format!("{}/{name}", self.path)
        }
    }

    /// The folder above, if there is one.
    fn parent(&self) -> Option<String> {
        if self.local() {
            std::path::Path::new(&self.path)
                .parent()
                .map(|parent| parent.to_string_lossy().into_owned())
        } else if self.path == "/" || self.path.is_empty() {
            None
        } else {
            let trimmed = self.path.trim_end_matches('/');
            Some(trimmed.rfind('/').map_or("/".to_owned(), |at| {
                if at == 0 {
                    "/".to_owned()
                } else {
                    trimmed[..at].to_owned()
                }
            }))
        }
    }

    /// The rows in view: the folder above, then the entries the filter and the hidden files let through, folders first, sorted.
    fn rows(&self) -> Vec<Row<'_>> {
        let filter = self.filter.value().to_lowercase();
        let mut entries: Vec<&Entry> = self
            .entries
            .iter()
            .filter(|entry| self.hidden || !entry.name.starts_with('.'))
            .filter(|entry| filter.is_empty() || entry.name.to_lowercase().contains(&filter))
            .collect();
        entries.sort_by(|a, b| {
            let order = match self.sort {
                Sort::Name => a.name.to_lowercase().cmp(&b.name.to_lowercase()),
                Sort::Size => a.size.cmp(&b.size),
                Sort::Date => a.modified.cmp(&b.modified),
            };
            let order = if self.descending {
                order.reverse()
            } else {
                order
            };
            let order = order.then_with(|| a.name.to_lowercase().cmp(&b.name.to_lowercase()));
            (b.kind == Kind::Folder)
                .cmp(&(a.kind == Kind::Folder))
                .then(order)
        });
        let mut rows = Vec::with_capacity(entries.len() + 1);
        if self.parent().is_some() && self.filter.value().is_empty() {
            rows.push(Row::Up);
        }
        rows.extend(entries.into_iter().map(Row::Entry));
        rows
    }

    /// The entry under the cursor, if it is one.
    pub(super) fn current(&self) -> Option<Entry> {
        match self.rows().get(self.cursor) {
            Some(Row::Entry(entry)) => Some((*entry).clone()),
            _ => None,
        }
    }

    /// Goes up one folder, keeping the folder left under the cursor.
    fn up(&mut self) -> Vec<PanelAsk> {
        let Some(parent) = self.parent() else {
            return Vec::new();
        };
        self.select = if self.local() {
            std::path::Path::new(&self.path)
                .file_name()
                .map(|name| name.to_string_lossy().into_owned())
        } else {
            self.path
                .trim_end_matches('/')
                .rsplit('/')
                .next()
                .map(str::to_owned)
        };
        self.go(parent)
    }

    /// Handles a pressed key.
    pub fn key(&mut self, key: KeyEvent) -> Vec<PanelAsk> {
        if self.listing() {
            if key.code == KeyCode::Char('d')
                && key.modifiers.contains(KeyModifiers::CONTROL)
                && !self.local()
            {
                self.pending = None;
                self.select = None;
                self.loading = false;
                return vec![PanelAsk::Op(Op::Disconnect)];
            }
            if key.code == KeyCode::Esc {
                self.pending = None;
                self.select = None;
                self.loading = false;
            }
            return Vec::new();
        }
        if let Some(form) = self.form.take() {
            return self.form_key(key, form);
        }
        if let Some(paths) = self.confirm.take() {
            return match key.code {
                KeyCode::Char('y') | KeyCode::Enter => {
                    self.loading = true;
                    self.marks.clear();
                    vec![PanelAsk::Op(Op::Delete(paths))]
                }
                KeyCode::Char('n') | KeyCode::Esc => Vec::new(),
                _ => {
                    self.confirm = Some(paths);
                    Vec::new()
                }
            };
        }
        if self.filtering {
            self.filter_key(key);
            return Vec::new();
        }
        if self.source.is_none() {
            return Vec::new();
        }
        self.browse_key(key)
    }

    /// Puts pasted text into the field of a form, or into the filter being typed.
    pub fn paste(&mut self, text: &str) {
        if self.listing() {
            return;
        }
        match &mut self.form {
            Some(Form::Mkdir(input) | Form::Rename { input, .. } | Form::Goto(input)) => {
                input.insert(text);
            }
            None if self.filtering => self.filter.insert(text),
            None => {}
        }
    }

    /// Handles a key while typing the filter: Enter keeps it, Esc clears it.
    fn filter_key(&mut self, key: KeyEvent) {
        match key.code {
            KeyCode::Enter => self.filtering = false,
            KeyCode::Esc => {
                self.filtering = false;
                self.filter = Input::default();
            }
            _ => {
                self.filter.handle(key);
            }
        }
        self.cursor = 0;
    }

    /// Handles a key of a form: Enter does what it asks, Esc gives up.
    fn form_key(&mut self, key: KeyEvent, form: Form) -> Vec<PanelAsk> {
        match (key.code, form) {
            (KeyCode::Esc, _) => Vec::new(),
            (KeyCode::Enter, Form::Mkdir(input)) if !input.value().trim().is_empty() => {
                let name = input.value().trim().to_owned();
                self.select = Some(name.clone());
                self.loading = true;
                vec![PanelAsk::Op(Op::Mkdir(self.join(&name)))]
            }
            (KeyCode::Enter, Form::Rename { from, input }) if !input.value().trim().is_empty() => {
                let name = input.value().trim().to_owned();
                if name == from {
                    return Vec::new();
                }
                self.select = Some(name.clone());
                self.loading = true;
                vec![PanelAsk::Op(Op::Rename {
                    from: self.join(&from),
                    to: self.join(&name),
                })]
            }
            (KeyCode::Enter, Form::Goto(input)) if !input.value().trim().is_empty() => {
                self.go(input.value().trim().to_owned())
            }
            (_, mut form) => {
                match &mut form {
                    Form::Mkdir(input) | Form::Rename { input, .. } | Form::Goto(input) => {
                        input.handle(key);
                    }
                }
                self.form = Some(form);
                Vec::new()
            }
        }
    }

    /// Handles a key while browsing.
    fn browse_key(&mut self, key: KeyEvent) -> Vec<PanelAsk> {
        let control = key.modifiers.contains(KeyModifiers::CONTROL);
        let last = self.rows().len().saturating_sub(1);
        match key.code {
            KeyCode::Char('g') if control => self.form = Some(Form::Goto(Input::with(&self.path))),
            KeyCode::Char('d') if control => {
                if self.local() {
                    return Vec::new();
                }
                return vec![PanelAsk::Op(Op::Disconnect)];
            }
            KeyCode::Char('a') if control => {
                let names: Vec<String> = self
                    .rows()
                    .iter()
                    .filter_map(|row| match row {
                        Row::Entry(entry) => Some(entry.name.clone()),
                        Row::Up => None,
                    })
                    .collect();
                self.marks.extend(names);
            }
            KeyCode::Down | KeyCode::Char('j') => self.cursor = (self.cursor + 1).min(last),
            KeyCode::Up | KeyCode::Char('k') => self.cursor = self.cursor.saturating_sub(1),
            KeyCode::PageDown => self.cursor = (self.cursor + PAGE).min(last),
            KeyCode::PageUp => self.cursor = self.cursor.saturating_sub(PAGE),
            KeyCode::Home | KeyCode::Char('g') => self.cursor = 0,
            KeyCode::End | KeyCode::Char('G') => self.cursor = last,
            KeyCode::Enter | KeyCode::Right | KeyCode::Char('l') => return self.enter(),
            KeyCode::Backspace | KeyCode::Left | KeyCode::Char('h') => return self.up(),
            KeyCode::Char(' ') | KeyCode::Insert => {
                if let Some(entry) = self.current()
                    && !self.marks.remove(&entry.name)
                {
                    self.marks.insert(entry.name);
                }
                self.cursor = (self.cursor + 1).min(last);
            }
            KeyCode::Esc if !self.marks.is_empty() => self.marks.clear(),
            KeyCode::Esc => self.filter = Input::default(),
            KeyCode::F(7) | KeyCode::Char('n') => self.form = Some(Form::Mkdir(Input::default())),
            KeyCode::F(2 | 6) => {
                if let Some(entry) = self.current() {
                    self.form = Some(Form::Rename {
                        input: Input::with(&entry.name),
                        from: entry.name,
                    });
                }
            }
            KeyCode::Delete | KeyCode::F(8) => self.ask_delete(),
            KeyCode::Char('f') => self.filtering = true,
            KeyCode::Char('.') => self.hidden = !self.hidden,
            KeyCode::Char('s') => {
                self.sort = match self.sort {
                    Sort::Name => Sort::Size,
                    Sort::Size => Sort::Date,
                    Sort::Date => Sort::Name,
                };
            }
            KeyCode::Char('S') => self.descending = !self.descending,
            KeyCode::Char('r') => return self.go(self.path.clone()),
            _ => {}
        }
        Vec::new()
    }

    /// Enter on a row: the folder above or a folder opens; files are copied in the next version.
    fn enter(&mut self) -> Vec<PanelAsk> {
        match self.rows().get(self.cursor).copied() {
            Some(Row::Up) => self.up(),
            Some(Row::Entry(entry)) if entry.kind == Kind::Folder || entry.kind == Kind::Link => {
                let path = self.join(&entry.name);
                self.go(path)
            }
            Some(Row::Entry(entry)) => vec![PanelAsk::Activate(entry.clone())],
            None => Vec::new(),
        }
    }

    /// Asks to delete the marked entries, or the one under the cursor.
    fn ask_delete(&mut self) {
        let names: Vec<String> = if self.marks.is_empty() {
            self.current().map(|entry| entry.name).into_iter().collect()
        } else {
            self.marked()
        };
        let paths: Vec<(String, bool)> = names
            .iter()
            .filter_map(|name| self.entries.iter().find(|entry| &entry.name == name))
            .map(|entry| (self.join(&entry.name), entry.kind == Kind::Folder))
            .collect();
        if !paths.is_empty() {
            self.confirm = Some(paths);
        }
    }

    /// Draws the source, path, and rows, with a loading box, form, or confirmation when active.
    pub fn render(
        &self,
        frame: &mut Frame,
        area: Rect,
        look: &Look,
        focused: bool,
        zone: &TimeZone,
    ) {
        let (m, palette) = (&look.messages, &look.palette);
        let title = self.title(look);
        let block = panel(&title, focused, palette);
        let inside = block.inner(area);
        frame.render_widget(block, area);
        let (settings, controls) = self.settings_bar(area, look);
        if !self.path.is_empty() {
            let path = Rect::new(
                area.x.saturating_add(1),
                controls.y,
                controls.x.saturating_sub(area.x.saturating_add(2)),
                controls.height,
            );
            frame.render_widget(
                Line::from(format!(" {} ", visible_name(&self.path))).fg(palette.muted),
                path,
            );
        }
        settings.render(frame, controls, look);
        if self.source.is_none() {
            frame.render_widget(
                Paragraph::new(Line::from(m.get("explorer.choose")).fg(palette.muted))
                    .wrap(Wrap { trim: true }),
                inside,
            );
            return;
        }
        let status = self.status_line(look);
        let [rows_area, status_area] = self.rows_layout(inside);
        self.render_rows(frame, rows_area, look, zone);
        if let Some(line) = status {
            frame.render_widget(Paragraph::new(line), status_area);
        }
        if let Some(pending) = &self.pending {
            Self::render_loading(frame, inside, look, pending);
            return;
        }
        if let Some(form) = &self.form {
            render_form(frame, area, form, look);
        }
        if let Some(paths) = &self.confirm {
            render_confirm(frame, area, paths, look);
        }
    }

    /// Draws the progress box above the dimmed file list.
    fn render_loading(frame: &mut Frame, area: Rect, look: &Look, pending: &Pending) {
        let path = visible_name(&pending.path);
        let message = look
            .messages
            .fill("explorer.loadingPath", &[("path", &path)]);
        let spinner = SPINNER[pending.ticks % SPINNER.len()];
        let line = Line::from(format!("{spinner} {message}")).bold().centered();
        let hint = look.messages.get("explorer.cancelLoading");
        let width = line.width().max(Line::from(hint.as_str()).width()) + 4;
        let hint = if pending.ticks >= CANCEL_AFTER {
            hint.as_str()
        } else {
            ""
        };
        let width = u16::try_from(width).unwrap_or(u16::MAX).min(60);
        let area = centered(area, width, 5);
        let inside = popup(frame, area, "", hint, &look.palette);
        frame.render_widget(Paragraph::new(line).wrap(Wrap { trim: false }), inside);
    }

    /// The title of this panel, also used to name it as a copy destination.
    pub(super) fn title(&self, look: &Look) -> String {
        match &self.source {
            Some(Source::Local) => look.messages.get("main.local"),
            Some(Source::Site { name, .. }) => name.clone(),
            None => look.messages.get("explorer.empty"),
        }
    }

    /// The screen row of the cursor, using the same scroll offset as the file list.
    pub(super) fn cursor_row(&self, area: Rect, look: &Look) -> u16 {
        let [rows, _] = self.rows_layout(panel("", false, &look.palette).inner(area));
        let offset = self.cursor - first_row(rows, self.cursor);
        rows.y + u16::try_from(offset).expect("the cursor is within the visible rows")
    }

    /// The controls and their right-aligned area on the bottom border.
    pub(super) fn settings_bar(&self, area: Rect, look: &Look) -> (Bar, Rect) {
        let messages = &look.messages;
        let sort = messages.get(match self.sort {
            Sort::Name => "explorer.name",
            Sort::Size => "details.size",
            Sort::Date => "explorer.sortDate",
        });
        let arrow = if self.descending { " ↓" } else { " ↑" };
        let filter = self.filter.value();
        let mut bar = Bar::default();
        bar.push(
            KeyCode::Char('s'),
            vec![
                Span::raw(format!(" {} ", messages.get("explorer.sort"))).fg(look.palette.muted),
                Bar::chip(sort, true, look),
            ],
        );
        bar.push(
            KeyCode::Char('S'),
            vec![Bar::chip(arrow.to_owned(), true, look)],
        );
        bar.separate(" · ", look);
        bar.setting(
            '.',
            &messages.get("explorer.hidden"),
            &messages.get(if self.hidden {
                "settings.yes"
            } else {
                "settings.no"
            }),
            self.hidden,
            look,
        );
        bar.separate(" · ", look);
        bar.setting(
            'f',
            &messages.get("explorer.filterLabel"),
            if filter.is_empty() { "—" } else { filter },
            !filter.is_empty() || self.filtering,
            look,
        );
        bar.separate(" ", look);
        let path_width = Line::from(visible_name(&self.path)).width() + 2;
        let reserved = u16::try_from(path_width.min(usize::from(area.width / 4)))
            .expect("the path fits in the terminal");
        let width = u16::try_from(bar.width())
            .unwrap_or(u16::MAX)
            .min(area.width.saturating_sub(3 + reserved));
        let controls = Rect::new(
            area.right().saturating_sub(width + 1),
            area.bottom().saturating_sub(1),
            width,
            area.height.min(1),
        );
        (bar, controls)
    }

    /// The file rows and the optional status line below them.
    fn rows_layout(&self, inside: Rect) -> [Rect; 2] {
        let status = self.error.is_some() || self.filtering || !self.filter.value().is_empty();
        Layout::vertical([Constraint::Fill(1), Constraint::Length(u16::from(status))]).areas(inside)
    }

    /// Selects the visible file row under the pointer within this panel.
    pub(super) fn select_at(&mut self, area: Rect, position: Position, look: &Look) -> bool {
        if self.listing() {
            return false;
        }
        let inside = panel("", false, &look.palette).inner(area);
        let [rows, _] = self.rows_layout(inside);
        if let Some(index) = row(
            rows,
            position,
            first_row(rows, self.cursor),
            self.rows().len(),
        ) {
            self.cursor = index;
            return true;
        }
        false
    }

    /// The delete confirmation action clicked in the footer.
    pub(super) fn confirmation_at(
        &self,
        area: Rect,
        position: Position,
        look: &Look,
    ) -> Option<KeyEvent> {
        let paths = self.confirm.as_ref()?;
        footer_key(
            confirmation_area(area, paths.len()),
            position,
            &look.messages.get("explorer.deleteKeys"),
            &[KeyCode::Enter, KeyCode::Esc],
        )
    }

    /// The line under the rows: the filter being typed or kept, or what went wrong.
    fn status_line(&self, look: &Look) -> Option<Line<'static>> {
        let (m, palette) = (&look.messages, &look.palette);
        if let Some(reason) = &self.error {
            return Some(error_line(reason, palette));
        }
        if self.filtering || !self.filter.value().is_empty() {
            let cursor = if self.filtering { "▏" } else { "" };
            return Some(Line::from(vec![
                Span::raw(m.get("explorer.filter")).fg(palette.muted),
                Span::raw(format!(" {}{cursor}", self.filter.value())),
            ]));
        }
        None
    }

    /// The rows: a mark, the name, the size and the date, the selected row highlighted when the panel has the focus.
    fn render_rows(&self, frame: &mut Frame, area: Rect, look: &Look, zone: &TimeZone) {
        let palette = &look.palette;
        let rows = self.rows();
        let style = if self.listing() {
            Style::new().add_modifier(Modifier::DIM)
        } else {
            Style::new()
        };
        if rows.is_empty() {
            let text = look.messages.get(if self.loading && !self.listing() {
                "main.loading"
            } else {
                "explorer.nothing"
            });
            frame.render_widget(
                Paragraph::new(Line::from(text).fg(palette.muted)).style(style),
                area,
            );
            return;
        }
        let height = usize::from(area.height.max(1));
        let skip = first_row(area, self.cursor);
        let width = usize::from(area.width);
        let name_width = width.saturating_sub(2 + 10 + 17 + 2).max(4);
        let lines: Vec<Line> = rows
            .iter()
            .enumerate()
            .skip(skip)
            .take(height)
            .map(|(at, row)| {
                let (mark, name, size, date, folder) = match row {
                    Row::Up => (" ", "../".to_owned(), String::new(), String::new(), true),
                    Row::Entry(entry) => {
                        let folder = entry.kind == Kind::Folder;
                        let mut name = visible_name(&entry.name);
                        if folder {
                            name.push('/');
                        }
                        let size = if folder {
                            String::new()
                        } else {
                            human(entry.size)
                        };
                        let date = entry.modified.map_or_else(String::new, |at| {
                            at.to_zoned_datetime(zone.clone())
                                .strftime("%Y-%m-%d %H:%M")
                                .to_string()
                        });
                        (
                            if self.marks.contains(&entry.name) {
                                "✓"
                            } else {
                                " "
                            },
                            name,
                            size,
                            date,
                            folder,
                        )
                    }
                };
                let name: String = name.chars().take(name_width).collect();
                let text = format!("{mark} {name:<name_width$} {size:>10} {date:>17}");
                let mut line = Line::from(text);
                if folder {
                    line = line.add_modifier(Modifier::BOLD);
                }
                if mark == "✓" {
                    line = line.fg(palette.accent_text);
                }
                if at == self.cursor {
                    highlight(line, palette)
                } else {
                    line
                }
            })
            .collect();
        frame.render_widget(Paragraph::new(lines).style(style), area);
    }
}

/// A size in binary units, such as `1.4 MiB`.
#[must_use]
pub fn human(size: u64) -> String {
    const UNITS: [&str; 6] = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
    if size < 1024 {
        return format!("{size} B");
    }
    #[allow(
        clippy::cast_precision_loss,
        reason = "sizes are shown with one decimal"
    )]
    let mut value = size as f64;
    let mut unit = 0;
    while value >= 1024.0 && unit < UNITS.len() - 1 {
        value /= 1024.0;
        unit += 1;
    }
    format!("{value:.1} {}", UNITS[unit])
}

/// A form over a panel: its title, its field and its keys.
fn render_form(frame: &mut Frame, area: Rect, form: &Form, look: &Look) {
    let (m, palette) = (&look.messages, &look.palette);
    let (title, label, input) = match form {
        Form::Mkdir(input) => (m.get("explorer.mkdirTitle"), m.get("explorer.name"), input),
        Form::Rename { input, .. } => (
            m.get("explorer.renameTitle"),
            m.get("explorer.newName"),
            input,
        ),
        Form::Goto(input) => (m.get("explorer.gotoTitle"), m.get("explorer.path"), input),
    };
    let box_area = centered(area, 60, 5);
    let inside = popup(
        frame,
        box_area,
        &title,
        &m.get("explorer.formKeys"),
        palette,
    );
    field(frame, inside, palette, &label, input, true);
}

/// The confirmation to delete, in the danger color, naming what goes.
fn render_confirm(frame: &mut Frame, area: Rect, paths: &[(String, bool)], look: &Look) {
    let (m, palette) = (&look.messages, &look.palette);
    let shown = paths.len().min(6);
    let box_area = confirmation_area(area, paths.len());
    let inside = popup(
        frame,
        box_area,
        &m.get("explorer.deleteTitle"),
        &m.get("explorer.deleteKeys"),
        palette,
    );
    let mut lines = vec![error_line(
        &m.fill(
            "explorer.deleteBody",
            &[("count", &paths.len().to_string())],
        ),
        palette,
    )];
    for (path, folder) in paths.iter().take(shown) {
        lines.push(Line::from(format!(
            "  {}{}",
            visible_name(path),
            if *folder { "/" } else { "" }
        )));
    }
    if paths.len() > shown {
        lines.push(
            Line::from(m.fill(
                "explorer.more",
                &[("count", &(paths.len() - shown).to_string())],
            ))
            .fg(palette.muted),
        );
    }
    lines.push(Line::from(m.get("explorer.undone")).fg(palette.muted));
    frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: false }), inside);
}

/// The delete confirmation, with up to six entries listed.
fn confirmation_area(area: Rect, count: usize) -> Rect {
    centered(area, 64, u16::try_from(count.min(6)).unwrap_or(6) + 7)
}
