//! The searchable site table, its forms, and the effects of site management.

mod form;

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::layout::{Margin, Position};
use ratatui::widgets::{Clear, HighlightSpacing, Row, Table, TableState};
use serde_json::{Value, json};
use transferly_client::models::{Connector, SiteMove};

use super::{
    Ask, Bar, Call, CallError, Frame, Home, Input, Line, Look, Overlay, Paragraph, Rect, Site,
    Style, Stylize, Uuid, centered, error_line, explain, field, highlight, panel, popup,
};
use crate::api::{HostKey, SiteAction};
use crate::ui::mouse::{MouseKind, first_row, footer_key, key, row};

use self::form::Form;

/// Actions presented in the table's shortcut bar.
const SHORTCUTS: [KeyCode; 8] = [
    KeyCode::Enter,
    KeyCode::Char('N'),
    KeyCode::Char('E'),
    KeyCode::Char('D'),
    KeyCode::Char('C'),
    KeyCode::Char('M'),
    KeyCode::Char('/'),
    KeyCode::Esc,
];

/// Which site draft to compose once the catalog arrives.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Draft {
    New,
    Edit(Uuid),
    Duplicate(Uuid),
}

/// What currently covers the site table.
#[derive(Debug, Clone, PartialEq)]
enum Dialog {
    Loading(Draft),
    Form(Box<Form>),
    Delete(Uuid),
    Move { site: Uuid, selected: usize },
}

/// An operation that can be suspended and retried by a shared popup.
#[derive(Debug, Clone, PartialEq, Eq)]
enum Pending {
    Running(SiteAction),
    Vault(SiteAction),
    HostKey(SiteAction),
    Trusting(SiteAction),
}

/// The table selection, search, and any site operation underway.
#[derive(Debug, Clone, Default, PartialEq)]
pub(super) struct View {
    selected: usize,
    search: Input,
    searching: bool,
    connectors: Option<Vec<Connector>>,
    dialog: Option<Dialog>,
    pending: Option<Pending>,
}

impl Home {
    /// Opens the management table over the explorer.
    pub(super) fn show_site_view(&mut self) -> Vec<Ask> {
        self.notice = None;
        self.site_view = Some(View::default());
        Vec::new()
    }

    /// Resolves the collection path from the root of the workspace.
    fn collection_name(&self, mut id: Option<Uuid>) -> String {
        let mut names = Vec::new();
        for _ in &self.collections {
            let Some(collection) = self.collections.iter().find(|item| Some(item.id) == id) else {
                break;
            };
            names.push(collection.name.as_str());
            id = collection.parent_id;
        }
        names.reverse();
        names.join(" / ")
    }

    /// Resolves a site by its identity.
    fn managed_site(&self, id: Uuid) -> Option<&Site> {
        self.sites.iter().find(|site| site.id == id)
    }

    /// Gets the display cells and applies the same search to drawing and selection.
    fn site_rows(&self, view: &View) -> Vec<(Uuid, [String; 5])> {
        let wanted = view.search.value().to_lowercase();
        self.sites
            .iter()
            .filter_map(|site| {
                let collection = self.collection_name(site.collection_id);
                let name = if collection.is_empty() {
                    site.name.clone()
                } else {
                    format!("{collection} / {}", site.name)
                };
                let value = |name| site.config.get(name).cloned().unwrap_or_default();
                let user = ["username", "access_key_id", "account_email", "account"]
                    .into_iter()
                    .find_map(|name| site.config.get(name))
                    .cloned()
                    .unwrap_or_default();
                let kind = view
                    .connectors
                    .as_ref()
                    .and_then(|items| items.iter().find(|item| item.r#type == site.connector))
                    .map_or_else(
                        || site.connector.to_ascii_uppercase(),
                        |item| item.name.clone(),
                    );
                let cells = [name, value("host"), value("port"), user, kind];
                cells
                    .iter()
                    .any(|cell| cell.to_lowercase().contains(&wanted))
                    .then_some((site.id, cells))
            })
            .collect()
    }

    /// Keeps the selection within the filtered table.
    fn selected_site(&self, view: &View) -> (usize, Option<Uuid>) {
        let rows = self.site_rows(view);
        let end = rows.len().saturating_sub(1);
        (end, rows.get(view.selected.min(end)).map(|(id, _)| *id))
    }

    /// Handles table or dialog keys while preserving the explorer's last focused side.
    pub(super) fn site_view_key(&mut self, key: KeyEvent, look: &Look) -> Vec<Ask> {
        let mut view = self.site_view.take().expect("the site table is active");
        if view.pending.is_some() {
            if key.code == KeyCode::Esc
                && matches!(view.pending, Some(Pending::Running(SiteAction::Test(_))))
            {
                view.dialog = None;
            }
            self.site_view = Some(view);
            return Vec::new();
        }
        let control = key.modifiers.contains(KeyModifiers::CONTROL);
        let mut asks = Vec::new();
        if let Some(dialog) = view.dialog.take() {
            if key.code != KeyCode::Esc {
                self.site_dialog_key(&mut view, dialog, key, look, &mut asks);
            }
        } else if view.searching {
            match key.code {
                KeyCode::Esc => {
                    view.search = Input::default();
                    view.selected = 0;
                    view.searching = false;
                }
                KeyCode::Enter => view.searching = false,
                _ if view.search.handle(key) => view.selected = 0,
                _ => {}
            }
        } else {
            let (end, selected) = self.selected_site(&view);
            view.selected = view.selected.min(end);
            match key.code {
                KeyCode::Esc => return Vec::new(),
                KeyCode::Enter if selected.is_some() => {
                    return self.open_site(selected.expect("a selected site"));
                }
                KeyCode::Up | KeyCode::Char('k') => view.selected = view.selected.saturating_sub(1),
                KeyCode::Down | KeyCode::Char('j') => view.selected = (view.selected + 1).min(end),
                KeyCode::Home | KeyCode::Char('g') => view.selected = 0,
                KeyCode::End | KeyCode::Char('G') => view.selected = end,
                KeyCode::Char('/') => view.searching = true,
                KeyCode::Char('n' | 'N') if !control => {
                    self.start_draft(&mut view, Draft::New, look, &mut asks);
                }
                _ => {
                    if let Some(site) = selected.filter(|_| !control) {
                        match key.code {
                            KeyCode::Char('d' | 'D') => view.dialog = Some(Dialog::Delete(site)),
                            KeyCode::Char('m' | 'M') => {
                                let collection =
                                    self.managed_site(site).and_then(|item| item.collection_id);
                                let selected = self
                                    .collections
                                    .iter()
                                    .position(|item| Some(item.id) == collection)
                                    .map_or(0, |index| index + 1);
                                view.dialog = Some(Dialog::Move { site, selected });
                            }
                            KeyCode::Char('e' | 'E') => {
                                self.start_draft(&mut view, Draft::Edit(site), look, &mut asks);
                            }
                            KeyCode::Char('c' | 'C') => {
                                self.start_draft(
                                    &mut view,
                                    Draft::Duplicate(site),
                                    look,
                                    &mut asks,
                                );
                            }
                            _ => {}
                        }
                    }
                }
            }
        }
        self.site_view = Some(view);
        asks
    }

    /// Handles a key in one of the table's dialogs.
    fn site_dialog_key(
        &mut self,
        view: &mut View,
        dialog: Dialog,
        key: KeyEvent,
        look: &Look,
        asks: &mut Vec<Ask>,
    ) {
        match dialog {
            Dialog::Delete(site) if matches!(key.code, KeyCode::Enter | KeyCode::Char('y')) => {
                view.dialog = Some(Dialog::Delete(site));
                self.submit_site_action(view, SiteAction::Delete(site), false, look, asks);
            }
            Dialog::Delete(_) if key.code == KeyCode::Char('n') => {}
            Dialog::Move { site, mut selected } => {
                match key.code {
                    KeyCode::Enter => {
                        let collection_id = selected
                            .checked_sub(1)
                            .and_then(|index| self.collections.get(index))
                            .map(|item| item.id);
                        let body = json!(SiteMove { collection_id });
                        self.submit_site_action(
                            view,
                            SiteAction::Move(site, body),
                            false,
                            look,
                            asks,
                        );
                    }
                    KeyCode::Up => selected = selected.saturating_sub(1),
                    KeyCode::Down => selected = (selected + 1).min(self.collections.len()),
                    _ => {}
                }
                view.dialog = Some(Dialog::Move { site, selected });
            }
            Dialog::Form(mut form) => {
                let test = matches!(key.code, KeyCode::Char('t' | 'T'))
                    && key.modifiers.contains(KeyModifiers::CONTROL);
                if test || key.code == KeyCode::Enter {
                    if let Some((action, needs_vault)) = form.submit(test, look) {
                        self.submit_site_action(view, action, needs_vault, look, asks);
                    }
                } else {
                    form.handle(key, &self.collections);
                }
                view.dialog = Some(Dialog::Form(form));
            }
            other => view.dialog = Some(other),
        }
    }

    /// Loads the schema on demand and holds the intended form while it arrives.
    fn start_draft(&mut self, view: &mut View, draft: Draft, look: &Look, asks: &mut Vec<Ask>) {
        self.notice = None;
        if let Some(connectors) = &view.connectors {
            view.dialog = self.draft_form(draft, connectors, look);
        } else {
            view.dialog = Some(Dialog::Loading(draft));
            asks.push(Ask::Call(Call::Connectors));
        }
    }

    /// Builds the form with the schema and any existing settings.
    fn draft_form(
        &mut self,
        draft: Draft,
        connectors: &[Connector],
        look: &Look,
    ) -> Option<Dialog> {
        let saved = match draft {
            Draft::New => None,
            Draft::Edit(id) | Draft::Duplicate(id) => Some(self.managed_site(id)?),
        };
        if let Some(form) = Form::new(draft, saved, connectors, look) {
            Some(Dialog::Form(Box::new(form)))
        } else {
            self.say(look.messages.get("siteView.noConnector"), true);
            None
        }
    }

    /// Caches a connector catalog and opens the requested form if still wanted.
    pub(super) fn site_connectors(
        &mut self,
        outcome: Result<Vec<Connector>, CallError>,
        look: &Look,
    ) -> Vec<Ask> {
        let Some(mut view) = self.site_view.take() else {
            return Vec::new();
        };
        let mut asks = Vec::new();
        match outcome {
            Ok(connectors) => {
                if let Some(Dialog::Loading(draft)) = view.dialog {
                    view.dialog = self.draft_form(draft, &connectors, look);
                }
                view.connectors = Some(connectors);
            }
            Err(error) => {
                if matches!(view.dialog, Some(Dialog::Loading(_))) {
                    view.dialog = None;
                    asks = self.trouble(&error, look);
                }
            }
        }
        self.site_view = Some(view);
        asks
    }

    /// Submits a write or test, unlocking first if the vault is known to be locked.
    fn submit_site_action(
        &mut self,
        view: &mut View,
        action: SiteAction,
        needs_vault: bool,
        look: &Look,
        asks: &mut Vec<Ask>,
    ) {
        let Some(workspace) = self.workspace else {
            return;
        };
        self.notice = None;
        if needs_vault && self.vault.as_ref().is_some_and(|vault| !vault.initialized) {
            self.site_notice(view, look.messages.get("siteView.vaultSetup"));
        } else if needs_vault && self.vault.as_ref().is_some_and(|vault| !vault.unlocked) {
            view.pending = Some(Pending::Vault(action));
            self.ask_vault(None);
        } else {
            view.pending = Some(Pending::Running(action.clone()));
            asks.push(Ask::Call(Call::Site(workspace, action)));
        }
    }

    /// Clears an action when the vault or host key is declined.
    pub(super) fn cancel_site_action(&mut self) {
        if let Some(view) = &mut self.site_view
            && matches!(view.pending, Some(Pending::Vault(_) | Pending::HostKey(_)))
        {
            view.pending = None;
        }
    }

    /// Resumes an action once the shared vault popup has unlocked the vault.
    pub(super) fn resume_site_action(&mut self) -> Vec<Ask> {
        let Some(view) = &mut self.site_view else {
            return Vec::new();
        };
        if let (Some(workspace), Some(Pending::Vault(action))) = (self.workspace, &view.pending) {
            let action = action.clone();
            view.pending = Some(Pending::Running(action.clone()));
            return vec![Ask::Call(Call::Site(workspace, action))];
        }
        Vec::new()
    }

    /// Sends a trust request without rerunning an explorer operation.
    #[rustfmt::skip]
    pub(super) fn trust_draft(&mut self, key: &HostKey) -> Option<Ask> {
        let view = self.site_view.as_mut()?;
        let workspace = self.workspace?;
        let Some(Pending::HostKey(action)) = &view.pending else { return None };
        view.pending = Some(Pending::Trusting(action.clone()));
        Some(Ask::Call(Call::TrustDraft(workspace, key.clone())))
    }

    /// Repeats the test after its host key is trusted.
    #[rustfmt::skip]
    pub(super) fn draft_trusted(
        &mut self,
        outcome: Result<(), &CallError>,
        look: &Look,
    ) -> Vec<Ask> {
        let Some(view) = &mut self.site_view else {
            return Vec::new();
        };
        let (Some(workspace), Some(Pending::Trusting(action))) =
            (self.workspace, view.pending.take())
        else {
            return Vec::new();
        };
        match outcome {
            Ok(()) => {
                view.pending = Some(Pending::Running(action.clone()));
                vec![Ask::Call(Call::Site(workspace, action))]
            }
            Err(error) if error.code.as_deref() == Some("unauthenticated") => {
                self.trouble(error, look)
            }
            Err(error) => {
                if let Some(Dialog::Form(form)) = &mut view.dialog { form.report_error(explain(error, look)) }
                Vec::new()
            }
        }
    }

    /// Handles the answer to a write or test and refreshes the tree after a write.
    #[rustfmt::skip]
    pub(super) fn site_reply(
        &mut self,
        workspace: Uuid,
        outcome: Result<(), &CallError>,
        look: &Look,
    ) -> Vec<Ask> {
        if self.workspace != Some(workspace) {
            return Vec::new();
        }
        let Some(mut view) = self.site_view.take() else {
            return Vec::new();
        };
        let mut asks = Vec::new();
        if let Some(Pending::Running(action)) = view.pending.take()
            && view.dialog.is_some()
        {
            match outcome {
                Ok(()) if matches!(action, SiteAction::Test(_)) => {
                    if let Some(Dialog::Form(form)) = &mut view.dialog { form.connected = true }
                }
                Ok(()) => {
                    if let SiteAction::Delete(site) = action {
                        self.sites.retain(|item| item.id != site);
                        for side in [super::Side::Left, super::Side::Right] {
                            if self.panel(side).source().and_then(super::Source::site) == Some(site)
                            {
                                self.panel_mut(side).close();
                            }
                        }
                    }
                    view.dialog = None;
                    asks.push(Ask::Call(Call::Tree(workspace)));
                }
                Err(error) => asks = self.site_failure(&mut view, action, error, look),
            }
        }
        self.site_view = Some(view);
        asks
    }

    /// Routes field validation, vault and host-key errors back to the draft.
    fn site_failure(
        &mut self,
        view: &mut View,
        action: SiteAction,
        error: &CallError,
        look: &Look,
    ) -> Vec<Ask> {
        match error.code.as_deref() {
            Some("invalid_config") => {
                if let Some(Dialog::Form(form)) = &mut view.dialog
                    && form.refused(error, look)
                {
                    return Vec::new();
                }
            }
            Some("vault_locked") => {
                if self.vault.as_ref().is_some_and(|vault| !vault.initialized) {
                    self.site_notice(view, look.messages.get("siteView.vaultSetup"));
                    return Vec::new();
                }
                view.pending = Some(Pending::Vault(action));
            }
            Some("host_key_unknown" | "host_key_changed") => {
                if let SiteAction::Test(body) = &action {
                    if body["site"]["config"]["jump_host"]
                        .as_str()
                        .is_some_and(|host| !host.is_empty())
                        && draft_peer(error).is_none()
                    {
                        self.site_notice(view, look.messages.get("siteView.jumpKey"));
                        return Vec::new();
                    }
                    if self.show_draft_key(body, error) {
                        view.pending = Some(Pending::HostKey(action));
                        return Vec::new();
                    }
                }
            }
            _ => {}
        }
        if let Some("unauthenticated" | "vault_locked" | "http_404") = error.code.as_deref() {
            self.trouble(error, look)
        } else {
            self.site_notice(view, explain(error, look));
            Vec::new()
        }
    }

    /// Places an operation error where the active form leaves room for it.
    fn site_notice(&mut self, view: &mut View, message: String) {
        if let Some(Dialog::Form(form)) = &mut view.dialog {
            form.report_error(message);
        } else {
            self.say(message, true);
        }
    }

    /// Opens the shared SSH key popup when the refusal identifies a key for the target.
    fn show_draft_key(&mut self, body: &Value, error: &CallError) -> bool {
        let config = &body["site"]["config"];
        let (Some(kind), Some(fingerprint)) = (
            error.params.get("keyType").and_then(Value::as_str),
            error.params.get("fingerprint").and_then(Value::as_str),
        ) else {
            return false;
        };
        let peer = draft_peer(error).or_else(|| {
            let host = config["host"].as_str()?;
            let port = config["port"]
                .as_str()
                .and_then(|port| port.parse().ok())
                .unwrap_or(22);
            Some((host, port))
        });
        let Some((host, port)) = peer else {
            return false;
        };
        let trusted = (error.code.as_deref() == Some("host_key_changed")).then(|| {
            error
                .params
                .get("trusted")
                .and_then(Value::as_array)
                .into_iter()
                .flatten()
                .filter_map(Value::as_str)
                .map(str::to_owned)
                .collect()
        });
        self.overlay = Some(Overlay::HostKey {
            side: self.last,
            site: body["site"]["name"].as_str().unwrap_or(host).to_owned(),
            key: HostKey {
                host: host.to_owned(),
                port,
                key_type: kind.to_owned(),
                fingerprint: fingerprint.to_owned(),
                mode: None,
            },
            trusted,
        });
        true
    }

    /// Pastes into the active search or editable form field.
    #[rustfmt::skip]
    pub(super) fn paste_site_view(&mut self, text: &str) {
        let Some(view) = self.site_view.as_mut().filter(|view| view.pending.is_none()) else { return };
        if view.searching {
            view.search.insert(text);
            view.selected = 0;
        } else if let Some(Dialog::Form(form)) = &mut view.dialog {
            form.paste(text);
        }
    }

    /// Finds the table and shortcut areas, including the search and header rows.
    fn site_table_areas(&self, area: Rect) -> [Rect; 5] {
        let [top, _, _, _, _, feedback, keys] = self.layout(area);
        let body = Rect::new(
            area.x,
            top.bottom(),
            area.width,
            feedback.y.saturating_sub(top.bottom()),
        );
        let inside = body.inner(Margin::new(1, 1));
        let search = Rect::new(inside.x, body.bottom() - 1, inside.width, 1);
        let rows = Rect::new(
            inside.x,
            inside.y + 1,
            inside.width,
            inside.height.saturating_sub(1),
        );
        let table = inside;
        [body, search, table, rows, keys]
    }

    /// Draws the site table, its keyboard shortcuts and the current dialog.
    pub(super) fn render_site_view(&self, frame: &mut Frame, look: &Look) {
        let Some(view) = &self.site_view else { return };
        let [body, search, table, rows, keys] = self.site_table_areas(frame.area());
        let m = &look.messages;
        let found = self.site_rows(view);
        let workspace = self.workspace().map_or("", |item| item.name.as_str());
        let (title, count) = (m.get("siteView.title"), found.len());
        frame.render_widget(Clear, body);
        let filter = m.fill("siteView.search", &[("query", view.search.value())]);
        let block = panel(
            &format!("{title} · {workspace} ({count})"),
            true,
            &look.palette,
        )
        .title_bottom(Line::from(format!(" {filter} ")).fg(look.palette.accent_text))
        .style(Style::new().bg(look.palette.bg).fg(look.palette.text));
        frame.render_widget(block, body);
        if view.searching {
            let before: String = view
                .search
                .value()
                .chars()
                .take(view.search.cursor())
                .collect();
            let prefix = m.fill("siteView.search", &[("query", &before)]);
            let column = u16::try_from(Line::from(prefix).width() + 1)
                .unwrap_or(u16::MAX)
                .min(search.width.saturating_sub(1));
            frame.set_cursor_position(Position::new(search.x + column, search.y));
        }
        let selected = view.selected.min(count.saturating_sub(1));
        let first = first_row(rows, selected);
        let header = Row::new(
            ["name", "host", "port", "user", "kind"]
                .map(|label| m.get(&format!("siteView.{label}"))),
        )
        .fg(look.palette.muted)
        .bold();
        let table_widget = Table::new(
            found
                .into_iter()
                .skip(first)
                .map(|(_, cells)| Row::new(cells)),
            [
                super::Constraint::Fill(3),
                super::Constraint::Fill(2),
                super::Constraint::Length(5),
                super::Constraint::Fill(1),
                super::Constraint::Fill(1),
            ],
        )
        .header(header)
        .highlight_spacing(HighlightSpacing::Always)
        .highlight_symbol("› ")
        .row_highlight_style(highlight(Line::default(), &look.palette).style);
        frame.render_stateful_widget(
            table_widget,
            table,
            &mut TableState::new().with_selected((count > 0).then_some(selected - first)),
        );
        if count == 0 {
            frame.render_widget(
                Line::from(m.get("siteView.empty")).fg(look.palette.muted),
                rows,
            );
        }
        Self::site_bar(keys, look).render(frame, keys, look);
        let feedback = self.layout(frame.area())[5];
        self.render_feedback(frame, feedback, look);
        self.render_site_dialog(frame, view, look);
    }

    /// Draws the active dialog above the table.
    fn render_site_dialog(&self, frame: &mut Frame, view: &View, look: &Look) {
        let m = &look.messages;
        let palette = &look.palette;
        match &view.dialog {
            Some(Dialog::Loading(_)) => {
                let inside = popup(
                    frame,
                    site_popup(frame.area()),
                    &m.get("siteView.title"),
                    &m.get("siteView.cancel"),
                    palette,
                );
                frame.render_widget(Paragraph::new(m.get("siteView.loading")), inside);
            }
            Some(Dialog::Form(form)) => form.render(
                frame,
                &self.collections,
                look,
                view.pending.is_some(),
                self.overlay.is_none(),
            ),
            Some(Dialog::Delete(id)) => {
                if let Some(site) = self.managed_site(*id) {
                    let inside = popup(
                        frame,
                        site_popup(frame.area()),
                        &m.get("siteView.delete"),
                        &m.get("siteView.deleteKeys"),
                        palette,
                    );
                    let warning = m.fill("siteView.deleteNamed", &[("site", &site.name)]);
                    frame.render_widget(
                        Paragraph::new(error_line(&warning, palette))
                            .wrap(super::Wrap { trim: true }),
                        inside,
                    );
                }
            }
            Some(Dialog::Move { selected, .. }) => {
                let inside = popup(
                    frame,
                    move_popup(frame.area(), self.collections.len()),
                    &m.get("siteView.move"),
                    &m.get("siteView.moveKeys"),
                    palette,
                );
                let rows = std::iter::once(m.get("siteView.root"))
                    .chain(
                        self.collections
                            .iter()
                            .map(|item| self.collection_name(Some(item.id))),
                    )
                    .enumerate()
                    .skip(first_row(inside, *selected))
                    .map(|(index, label)| {
                        let line = Line::from(label);
                        if index == *selected {
                            highlight(line, palette)
                        } else {
                            line
                        }
                    })
                    .collect::<Vec<_>>();
                frame.render_widget(Paragraph::new(rows), inside);
            }
            None => {}
        }
    }

    /// Returns the visible shortcuts used for both drawing and mouse input.
    fn site_bar(area: Rect, look: &Look) -> Bar {
        Bar::shortcuts(
            &look.messages.get("siteView.keys"),
            &SHORTCUTS,
            usize::from(area.width),
            look,
        )
    }

    /// Applies a mouse action to the table or the active dialog.
    pub(super) fn site_view_mouse(
        &mut self,
        area: Rect,
        point: Position,
        kind: MouseKind,
        double: bool,
        look: &Look,
    ) -> (Vec<Ask>, bool) {
        let [_, search, _, rows, keys] = self.site_table_areas(area);
        let mut view = self.site_view.take().expect("the site table is active");
        let mut action = None;
        let mut selected = false;
        if view.pending.is_none() {
            if view.dialog.is_some() {
                action = self.site_dialog_click(area, point, kind, &mut view, look);
            } else if let Some(wheel) = kind.wheel() {
                if rows.contains(point) && !view.searching {
                    action = Some(wheel);
                }
            } else if kind == MouseKind::Left {
                if search.contains(point) {
                    action = Some(key(KeyCode::Char('/')));
                } else if keys.contains(point) {
                    action = Self::site_bar(keys, look).key_at(point.x - keys.x).map(key);
                } else {
                    let (end, _) = self.selected_site(&view);
                    let first = first_row(rows, view.selected.min(end));
                    if let Some(index) = row(rows, point, first, self.site_rows(&view).len()) {
                        view.selected = index;
                        view.searching = false;
                        selected = true;
                        action = double.then(|| key(KeyCode::Enter));
                    }
                }
            }
        }
        self.site_view = Some(view);
        (
            action.map_or_else(Vec::new, |action| self.site_view_key(action, look)),
            selected,
        )
    }

    /// Focuses a clicked field or collection, or activates a confirmation footer.
    fn site_dialog_click(
        &self,
        area: Rect,
        point: Position,
        kind: MouseKind,
        view: &mut View,
        look: &Look,
    ) -> Option<KeyEvent> {
        if kind != MouseKind::Left {
            return None;
        }
        match view.dialog.as_mut()? {
            Dialog::Form(form) => form.click(area, point, look),
            Dialog::Move { selected, .. } => {
                let bounds = move_popup(area, self.collections.len());
                let inside = super::super::widgets::popup_inside(bounds, &look.palette);
                if let Some(index) = row(
                    inside,
                    point,
                    first_row(inside, *selected),
                    self.collections.len() + 1,
                ) {
                    *selected = index;
                }
                footer_key(
                    bounds,
                    point,
                    &look.messages.get("siteView.moveKeys"),
                    &[KeyCode::Enter, KeyCode::Esc],
                )
            }
            Dialog::Delete(_) => footer_key(
                site_popup(area),
                point,
                &look.messages.get("siteView.deleteKeys"),
                &[KeyCode::Enter, KeyCode::Esc],
            ),
            Dialog::Loading(_) => None,
        }
    }
}

/// Reads the peer address supplied with a rejected key by newer servers.
fn draft_peer(error: &CallError) -> Option<(&str, u16)> {
    let host = error.params.get("host")?.as_str()?;
    let port = u16::try_from(error.params.get("port")?.as_u64()?).ok()?;
    (!host.is_empty() && port > 0).then_some((host, port))
}

/// The confirmation and loading popup bounds.
fn site_popup(area: Rect) -> Rect {
    centered(area, 70, 7)
}

/// The collection picker bounds.
fn move_popup(area: Rect, count: usize) -> Rect {
    centered(area, 58, u16::try_from(count.min(15)).unwrap_or(15) + 3)
}
