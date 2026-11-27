//! The connector schema projected into a scrollable site draft.

use std::collections::HashMap;

use ratatui::layout::Position;
use ratatui::widgets::Wrap;
use serde_json::json;
use transferly_client::models::connector_field::{Group, Kind};
use transferly_client::models::{
    Collection, ConnectorCondition, ConnectorDefault, ConnectorField, SiteDraftTest, SiteInput,
};

use super::{
    CallError, Connector, Draft, Frame, Input, KeyCode, KeyEvent, Line, Look, Paragraph, Rect,
    Site, SiteAction, Stylize, centered, error_line, field, footer_key, popup,
};
use crate::ui::widgets::popup_inside;

/// Groups ordered as in the web form.
const GROUPS: [Group; 6] = [
    Group::Connection,
    Group::Access,
    Group::Advanced,
    Group::Charset,
    Group::Proxy,
    Group::Jump,
];

/// The colors defined by the site input contract.
const COLORS: [&str; 9] = [
    "", "red", "orange", "yellow", "green", "teal", "blue", "violet", "pink",
];

/// The text fields, including site metadata alongside the schema fields.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
enum Field {
    Kind,
    Name,
    Directory,
    Collection,
    Color,
    Speed,
    Setting(String),
}

/// The draft, its values and the focused field.
#[derive(Debug, Clone, PartialEq)]
pub(super) struct Form {
    draft: Draft,
    connectors: Vec<Connector>,
    connector: usize,
    saved_secrets: Vec<String>,
    values: HashMap<Field, Input>,
    focus: usize,
    error: Option<(Field, String)>,
    status_error: Option<String>,
    /// Whether the latest test succeeded for this draft.
    pub(super) connected: bool,
}

impl Form {
    /// Starts with SFTP when available, or fills the fields of the saved site.
    pub(super) fn new(
        draft: Draft,
        site: Option<&Site>,
        catalog: &[Connector],
        look: &Look,
    ) -> Option<Self> {
        let connectors: Vec<_> = catalog
            .iter()
            .filter(|item| item.enabled || site.is_some_and(|site| site.connector == item.r#type))
            .cloned()
            .collect();
        let wanted = site.map_or("sftp", |site| site.connector.as_str());
        let connector = connectors
            .iter()
            .position(|item| item.r#type == wanted)
            .or_else(|| (site.is_none() && !connectors.is_empty()).then_some(0))?;
        let mut values = HashMap::new();
        let mut saved_secrets = Vec::new();
        if let Some(site) = site {
            let name = if matches!(draft, Draft::Duplicate(_)) {
                look.messages
                    .fill("siteView.copyName", &[("site", &site.name)])
            } else {
                site.name.clone()
            };
            values.insert(Field::Name, Input::with(&name));
            if let Some(dir) = &site.remote_dir {
                values.insert(Field::Directory, Input::with(dir));
            }
            if let Some(collection) = site.collection_id {
                values.insert(Field::Collection, Input::with(&collection.to_string()));
            }
            if let Some(color) = site.color {
                values.insert(Field::Color, Input::with(&color.to_string()));
            }
            if site.speed_limit_bps != 0 {
                values.insert(Field::Speed, Input::with(&site.speed_limit_bps.to_string()));
            }
            for (name, value) in &site.config {
                values.insert(Field::Setting(name.clone()), Input::with(value));
            }
            saved_secrets.clone_from(&site.secrets);
        }
        Some(Self {
            draft,
            connectors,
            connector,
            saved_secrets,
            values,
            focus: 0,
            error: None,
            status_error: None,
            connected: false,
        })
    }

    /// The schema of the chosen connector.
    fn schema(&self) -> &Connector {
        &self.connectors[self.connector]
    }

    /// A setting held in the draft, before applying defaults.
    fn value(&self, field: &Field) -> &str {
        self.values.get(field).map_or("", Input::value)
    }

    /// Resolves nonsecret fields in schema order, including fixed and default values used by conditions.
    fn config(&self) -> HashMap<String, String> {
        let mut config = HashMap::new();
        for field in &self.schema().fields {
            if secret(field.kind) || !applies(field, &config) {
                continue;
            }
            let raw = self.value(&Field::Setting(field.name.clone())).trim();
            let value = conditional(field.fixed.as_deref(), &config).unwrap_or_else(|| {
                if raw.is_empty() {
                    conditional(field.defaults.as_deref(), &config)
                        .or(field.default.as_deref())
                        .unwrap_or_default()
                } else {
                    raw
                }
            });
            if !value.is_empty() {
                config.insert(field.name.clone(), value.to_owned());
            }
        }
        config
    }

    /// The fields whose conditions hold, ordered by section.
    fn fields(&self, config: &HashMap<String, String>) -> Vec<Field> {
        let mut rows = vec![Field::Kind];
        for group in GROUPS {
            rows.extend(
                self.schema()
                    .fields
                    .iter()
                    .filter(|field| field.group == group && applies(field, config))
                    .map(|field| Field::Setting(field.name.clone())),
            );
            if group == Group::Access {
                rows.extend([
                    Field::Name,
                    Field::Directory,
                    Field::Collection,
                    Field::Color,
                    Field::Speed,
                ]);
            }
        }
        rows
    }

    /// Looks up the schema entry for a form row.
    fn setting(&self, field: &Field) -> Option<&ConnectorField> {
        match field {
            Field::Setting(name) => self
                .schema()
                .fields
                .iter()
                .find(|field| &field.name == name),
            _ => None,
        }
    }

    /// Lists choice values, with an empty option where the setting is optional.
    fn choices(&self, field: &Field, collections: &[Collection]) -> Vec<String> {
        match field {
            Field::Collection => std::iter::once(String::new())
                .chain(collections.iter().map(|item| item.id.to_string()))
                .collect(),
            Field::Color => COLORS.map(str::to_owned).to_vec(),
            _ => {
                let Some(setting) = self.setting(field) else {
                    return Vec::new();
                };
                match setting.kind {
                    Kind::Boolean => ["false", "true"].map(str::to_owned).to_vec(),
                    Kind::Choice => {
                        let mut choices = setting.choices.clone().unwrap_or_default();
                        if !setting.required {
                            choices.insert(0, String::new());
                        }
                        choices
                    }
                    _ => Vec::new(),
                }
            }
        }
    }

    /// Whether the schema locks this setting for the current configuration.
    fn read_only(&self, field: &Field, config: &HashMap<String, String>) -> bool {
        self.setting(field).is_some_and(|setting| {
            setting.kind == Kind::Oauth
                || conditional(setting.fixed.as_deref(), config).is_some()
                || self
                    .schema()
                    .fields
                    .iter()
                    .any(|other| other.account.as_ref() == Some(&setting.name))
        })
    }

    /// Handles focus navigation, choices, and text editing.
    pub(super) fn handle(&mut self, key: KeyEvent, collections: &[Collection]) {
        let config = self.config();
        let fields = self.fields(&config);
        let index = self.focus.min(fields.len() - 1);
        let forward = key.code == KeyCode::Right;
        match key.code {
            KeyCode::Tab if !key.modifiers.contains(super::KeyModifiers::SHIFT) => {
                self.focus = (index + 1) % fields.len();
            }
            KeyCode::BackTab | KeyCode::Tab => self.focus = previous(index, fields.len()),
            _ => {
                if fields[index] == Field::Kind {
                    if self.draft == Draft::New
                        && matches!(key.code, KeyCode::Left | KeyCode::Right)
                    {
                        let next = advance(self.connector, self.connectors.len(), forward);
                        if next != self.connector {
                            self.connector = next;
                            self.values
                                .retain(|name, _| !matches!(name, Field::Setting(_)));
                            self.clear_status();
                        }
                    }
                    return;
                }
                let field = &fields[index];
                if self.read_only(field, &config) {
                    return;
                }
                let choices = self.choices(field, collections);
                if choices.is_empty() {
                    self.values.entry(field.clone()).or_default().handle(key);
                } else if matches!(key.code, KeyCode::Left | KeyCode::Right) {
                    let current = self.shown_value(field, &config);
                    let index = choices
                        .iter()
                        .position(|choice| choice == current)
                        .unwrap_or(0);
                    let next = &choices[advance(index, choices.len(), forward)];
                    self.values.insert(field.clone(), Input::with(next));
                } else {
                    return;
                }
                self.clear_status();
            }
        }
    }

    /// Pastes a value, keeping the line breaks of a private key.
    pub(super) fn paste(&mut self, text: &str) {
        let config = self.config();
        let fields = self.fields(&config);
        let field = &fields[self.focus.min(fields.len() - 1)];
        if matches!(field, Field::Kind | Field::Collection | Field::Color)
            || self.read_only(field, &config)
            || self
                .setting(field)
                .is_some_and(|setting| matches!(setting.kind, Kind::Boolean | Kind::Choice))
        {
            return;
        }
        let private_key = self
            .setting(field)
            .is_some_and(|setting| setting.kind == Kind::PrivateKey);
        let input = self.values.entry(field.clone()).or_default();
        if private_key {
            input.insert_multiline(text);
        } else {
            input.insert(text);
        }
        self.clear_status();
    }

    /// Clears the result of a test or save when the draft changes.
    fn clear_status(&mut self) {
        self.error = None;
        self.status_error = None;
        self.connected = false;
    }

    /// Displays an error that belongs to the operation as a whole.
    pub(super) fn report_error(&mut self, message: String) {
        self.status_error = Some(message);
    }

    /// The value displayed for a field, including any default or fixed value.
    fn shown_value<'a>(&'a self, field: &Field, config: &'a HashMap<String, String>) -> &'a str {
        match field {
            Field::Setting(name) => config
                .get(name)
                .map_or_else(|| self.value(field), String::as_str),
            _ => self.value(field),
        }
    }

    /// Builds a save or test request from applicable settings and separately stored secrets.
    pub(super) fn submit(&mut self, test: bool, look: &Look) -> Option<(SiteAction, bool)> {
        self.clear_status();
        let name = self.value(&Field::Name).trim();
        if name.is_empty() {
            self.fail(Field::Name, look.messages.get("siteView.reason.required"));
            return None;
        }
        let speed = self.value(&Field::Speed).trim();
        let speed_limit_bps = if speed.is_empty() {
            0
        } else if let Some(number) = speed.parse::<i64>().ok().filter(|number| *number >= 0) {
            number
        } else {
            self.fail(Field::Speed, look.messages.get("siteView.speedError"));
            return None;
        };
        let config = self.config();
        let mut secrets = HashMap::new();
        for field in &self.schema().fields {
            if !secret(field.kind) || !applies(field, &config) {
                continue;
            }
            let row = Field::Setting(field.name.clone());
            let value = self.value(&row);
            if field.kind == Kind::Oauth && !self.saved_secrets.contains(&field.name) {
                self.fail(row, look.messages.get("siteView.reason.connect"));
                return None;
            }
            if !value.is_empty() {
                secrets.insert(field.name.clone(), value.to_owned());
            }
        }
        let mut site = SiteInput::new(name.to_owned(), self.schema().r#type.clone(), config);
        site.collection_id = self.value(&Field::Collection).parse().ok();
        site.speed_limit_bps = Some(speed_limit_bps);
        site.remote_dir = Some(self.value(&Field::Directory).trim().to_owned());
        site.color = Some(self.value(&Field::Color).to_owned());
        let needs_vault = !secrets.is_empty()
            || (test && !self.saved_secrets.is_empty())
            || matches!(self.draft, Draft::Duplicate(_));
        site.secrets = (!secrets.is_empty()).then_some(secrets);
        let source = match self.draft {
            Draft::New => None,
            Draft::Edit(id) | Draft::Duplicate(id) => Some(id),
        };
        let action = if test {
            SiteAction::Test(json!(SiteDraftTest {
                site_id: source,
                site
            }))
        } else {
            match self.draft {
                Draft::Edit(id) => SiteAction::Edit(id, json!(site)),
                Draft::New | Draft::Duplicate(_) => {
                    site.secrets_from = source;
                    SiteAction::Create(json!(site))
                }
            }
        };
        Some((action, needs_vault))
    }

    /// Opens the offending field and puts the refusal below it.
    pub(super) fn refused(&mut self, error: &CallError, look: &Look) -> bool {
        let Some(name) = error
            .params
            .get("field")
            .and_then(serde_json::Value::as_str)
        else {
            return false;
        };
        let field = Field::Setting(name.to_owned());
        if !self.fields(&self.config()).contains(&field) {
            return false;
        }
        let reason = error
            .params
            .get("reason")
            .and_then(serde_json::Value::as_str)
            .unwrap_or("choice");
        let key = format!("siteView.reason.{reason}");
        let setting = self.setting(&field);
        let min = setting
            .and_then(|item| item.min)
            .unwrap_or_default()
            .to_string();
        let max = setting
            .and_then(|item| {
                if reason == "length" {
                    item.max_length
                } else {
                    item.max
                }
            })
            .unwrap_or_default()
            .to_string();
        let message = look.messages.fill(&key, &[("min", &min), ("max", &max)]);
        let message = if message == key {
            look.messages.get("siteView.reason.choice")
        } else {
            message
        };
        self.fail(field, message);
        true
    }

    /// Focuses a field showing an error.
    fn fail(&mut self, field: Field, message: String) {
        self.focus = self
            .fields(&self.config())
            .iter()
            .position(|item| *item == field)
            .unwrap_or(0);
        self.error = Some((field, message));
    }

    /// Returns the rectangle of the popup and the number of fully visible fields.
    fn layout(area: Rect, field_count: usize, look: &Look) -> (Rect, Rect, usize) {
        let height =
            u16::try_from(field_count.saturating_mul(3).saturating_add(5)).unwrap_or(u16::MAX);
        let bounds = centered(area, 76, height.min(area.height.saturating_sub(2)));
        let inside = popup_inside(bounds, &look.palette);
        (
            bounds,
            inside,
            usize::from(inside.height.saturating_sub(3) / 3).max(1),
        )
    }

    /// Renders the form, keeping the focused row visible when the terminal is short.
    pub(super) fn render(
        &self,
        frame: &mut Frame,
        collections: &[Collection],
        look: &Look,
        busy: bool,
        active: bool,
    ) {
        let config = self.config();
        let fields = self.fields(&config);
        let m = &look.messages;
        let title = m.get(match self.draft {
            Draft::New => "siteView.new",
            Draft::Edit(_) => "siteView.edit",
            Draft::Duplicate(_) => "siteView.duplicate",
        });
        let (bounds, inside, visible) = Self::layout(frame.area(), fields.len(), look);
        let title = format!("{title} · {}", self.schema().name);
        popup(
            frame,
            bounds,
            &title,
            &m.get("siteView.formKeys"),
            &look.palette,
        );
        let focused = self.focus.min(fields.len() - 1);
        let first = focused.saturating_sub(visible - 1);
        let failing = self
            .error
            .as_ref()
            .filter(|(failed, _)| &fields[focused] == failed);
        for (offset, item) in fields.iter().skip(first).take(visible).enumerate() {
            let label = self.label(item, look);
            let (value, editable) = self.display(item, &config, collections, look);
            let y = inside.y + u16::try_from(offset).unwrap_or(0) * 3;
            let area = Rect::new(inside.x, y, inside.width, 3);
            let focused = first + offset == focused && active && !busy;
            field(
                frame,
                area,
                &look.palette,
                &label,
                &value,
                focused && editable,
            );
            if focused && !editable {
                frame.render_widget(super::panel(&label, true, &look.palette), area);
            }
            if let Some((failed, message)) = failing
                && item == failed
            {
                let line = Rect::new(inside.x + 1, y + 3, inside.width.saturating_sub(2), 1);
                frame.render_widget(error_line(message, &look.palette), line);
                break;
            }
        }
        let status = if busy {
            Some(Line::from(m.get("siteView.working")))
        } else if let Some(error) = &self.status_error {
            Some(error_line(error, &look.palette))
        } else if self.connected {
            Some(
                Line::from(m.get("siteView.connected"))
                    .fg(look.palette.ok)
                    .bold(),
            )
        } else if self
            .setting(&fields[focused])
            .is_some_and(|field| field.kind == Kind::PrivateKey)
        {
            Some(Line::from(m.get("siteView.pasteKey")))
        } else {
            None
        };
        if let Some(status) = status {
            let area = Rect::new(inside.x, inside.bottom().saturating_sub(2), inside.width, 2);
            frame.render_widget(Paragraph::new(status).wrap(Wrap { trim: true }), area);
        }
    }

    /// Translates site properties and schema field labels.
    fn label(&self, field: &Field, look: &Look) -> String {
        let key = match field {
            Field::Kind => "kind",
            Field::Name => "name",
            Field::Directory => "directory",
            Field::Collection => "collection",
            Field::Color => "color",
            Field::Speed => "speed",
            Field::Setting(name) => {
                let label = translated(look, "siteView.field", name);
                let group = self.setting(field).and_then(|setting| match setting.group {
                    Group::Connection | Group::Access => None,
                    Group::Advanced => Some("advanced"),
                    Group::Charset => Some("charset"),
                    Group::Proxy => Some("proxy"),
                    Group::Jump => Some("jump"),
                });
                return group.map_or_else(
                    || label.clone(),
                    |group| {
                        format!(
                            "{} / {label}",
                            look.messages.get(&format!("siteView.group.{group}"))
                        )
                    },
                );
            }
        };
        look.messages.get(&format!("siteView.{key}"))
    }

    /// Formats choices and masks secrets, with a hint when a saved secret is kept.
    fn display(
        &self,
        field: &Field,
        config: &HashMap<String, String>,
        collections: &[Collection],
        look: &Look,
    ) -> (Input, bool) {
        if *field == Field::Kind {
            let name = &self.schema().name;
            let label = if self.draft == Draft::New {
                format!("‹ {name} ›")
            } else {
                name.clone()
            };
            return (Input::with(&label), false);
        }
        let value = self.shown_value(field, config);
        let choice_label = match field {
            Field::Collection => Some(
                collections
                    .iter()
                    .find(|item| item.id.to_string() == value)
                    .map_or_else(
                        || look.messages.get("siteView.root"),
                        |item| item.name.clone(),
                    ),
            ),
            Field::Color => Some(translated(
                look,
                "siteView.choice.color",
                if value.is_empty() { "none" } else { value },
            )),
            Field::Setting(name) if !self.choices(field, collections).is_empty() => {
                Some(translated(look, &format!("siteView.choice.{name}"), value))
            }
            _ => None,
        };
        if let Some(label) = choice_label {
            return (Input::with(&format!("‹ {label} ›")), false);
        }
        if let Some(setting) = self.setting(field)
            && secret(setting.kind)
        {
            if value.is_empty() && self.saved_secrets.contains(&setting.name) {
                return (Input::with(&look.messages.get("siteView.keep")), false);
            }
            if setting.kind == Kind::Oauth {
                return (Input::with(&look.messages.get("siteView.oauth")), false);
            }
            return (
                self.values.get(field).cloned().unwrap_or_default().masked(),
                true,
            );
        }
        let input = self
            .values
            .get(field)
            .filter(|input| input.value() == value)
            .cloned()
            .unwrap_or_else(|| Input::with(value));
        (input, !self.read_only(field, config))
    }

    /// Focuses the clicked row or activates a footer button.
    pub(super) fn click(&mut self, area: Rect, point: Position, look: &Look) -> Option<KeyEvent> {
        let fields = self.fields(&self.config());
        let (bounds, inside, visible) = Self::layout(area, fields.len(), look);
        if let Some(action) = footer_key(
            bounds,
            point,
            &look.messages.get("siteView.formKeys"),
            &[
                KeyCode::Tab,
                KeyCode::Char('t'),
                KeyCode::Enter,
                KeyCode::Esc,
            ],
        ) {
            if action.code == KeyCode::Char('t') {
                return Some(KeyEvent::new(action.code, super::KeyModifiers::CONTROL));
            }
            return Some(action);
        }
        if inside.contains(point) {
            let offset = usize::from(point.y - inside.y) / 3;
            let focused = self.focus.min(fields.len() - 1);
            let first = focused.saturating_sub(visible - 1);
            let end = if self
                .error
                .as_ref()
                .is_some_and(|(field, _)| field == &fields[focused])
            {
                focused + 1
            } else {
                fields.len()
            };
            if offset < visible && first + offset < end {
                self.focus = first + offset;
            }
        }
        None
    }
}

/// The previous index, wrapping around at the first row.
fn previous(index: usize, count: usize) -> usize {
    index.checked_sub(1).unwrap_or(count - 1)
}

/// Advances or wraps to the previous choice.
fn advance(index: usize, count: usize, forward: bool) -> usize {
    if forward {
        (index + 1) % count
    } else {
        previous(index, count)
    }
}

/// Whether the schema keeps the field in the vault.
fn secret(kind: Kind) -> bool {
    matches!(kind, Kind::Secret | Kind::PrivateKey | Kind::Oauth)
}

/// A condition uses the settled value, or tests for any nonempty value.
fn holds(condition: &ConnectorCondition, config: &HashMap<String, String>) -> bool {
    let value = config.get(&condition.field).map_or("", String::as_str);
    condition
        .r#in
        .as_ref()
        .filter(|items| !items.is_empty())
        .map_or_else(
            || !value.is_empty(),
            |items| items.iter().any(|item| item == value),
        )
}

/// Whether a field applies to the current configuration.
fn applies(field: &ConnectorField, config: &HashMap<String, String>) -> bool {
    field
        .when
        .as_ref()
        .is_none_or(|condition| holds(condition, config))
}

/// The first matching fixed value or dependent default.
fn conditional<'a>(
    rules: Option<&'a [ConnectorDefault]>,
    config: &HashMap<String, String>,
) -> Option<&'a str> {
    rules?
        .iter()
        .find(|rule| holds(&rule.when, config))
        .map(|rule| rule.value.as_str())
}

/// Falls back to the schema's name or value when a newer server adds a field.
fn translated(look: &Look, prefix: &str, value: &str) -> String {
    let key = format!("{prefix}.{value}");
    let text = look.messages.get(&key);
    if text == key { value.to_owned() } else { text }
}
