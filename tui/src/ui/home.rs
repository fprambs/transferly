//! The main screen (docs/design/terminal-client.md, Layout, Commands, Settings and Screens 3, 4, 8, 11 and 12): the tabs, the sites of
//! the workspace as a tree, the explorer, the status bar, and over them the command line of `/`, the settings of `/config`, the lists
//! to choose a theme, a language or a workspace, the help and the vault. Like the rest of the interface it is pure: keys and the replies
//! of the server change it and it returns the calls to make.

use std::collections::HashSet;

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::Frame;
use ratatui::layout::{Constraint, Layout, Rect};
use ratatui::style::{Modifier, Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Paragraph, Wrap};
use transferly_client::models::{Collection, Site, User, VaultStatus, Workspace, workspace};
use url::Url;
use uuid::Uuid;

use super::explorer::{Entry, Op, Panel, PanelAsk, Source};
use super::input::Input;
use super::look::Look;
use super::theme::{self, Palette};
use super::widgets::{centered, error_line, field, highlight, panel, popup};
use crate::api::{HostKey, TrustMode};
use crate::config::{Colors, EnterOnFile};
use crate::i18n::Language;

/// The version of this client.
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// The commands of the command line, in the order they are listed.
pub const COMMANDS: [&str; 9] = [
    "config",
    "theme",
    "language",
    "workspace",
    "server",
    "web",
    "keys",
    "logout",
    "quit",
];

/// The rows of `/config`, in order.
const SETTINGS: [&str; 8] = [
    "language",
    "theme",
    "timezone",
    "colors",
    "enterOnFile",
    "hiddenFiles",
    "confirmQuit",
    "checkUpdates",
];

/// The width of the sites panel.
const SITES_WIDTH: u16 = 28;

/// Below this width only the focused panel is shown.
const NARROW: u16 = 100;

/// The server signed in to and the token of the session.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Session {
    /// The server.
    pub server: Url,
    /// The token of the session.
    pub token: String,
}

/// A call to the server.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Call {
    /// The account.
    Me,
    /// The version of the server.
    Version,
    /// The workspaces of the account.
    Workspaces,
    /// The collections and sites of a workspace.
    Tree(Uuid),
    /// The state of the vault.
    Vault,
    /// Unlock the vault with the master password.
    Unlock(String),
    /// Change the language or the theme of the account.
    Preferences {
        /// The new language, if it changes.
        locale: Option<String>,
        /// The new theme, if it changes.
        theme: Option<String>,
    },
    /// Trust the key of a server and try the panel again.
    Trust {
        /// The workspace of the site.
        workspace: Uuid,
        /// The panel to try again.
        side: Side,
        /// The key.
        key: HostKey,
    },
    /// Work with the files of a site in a panel.
    Files {
        /// The workspace of the site.
        workspace: Uuid,
        /// The site.
        site: Uuid,
        /// The panel that asked.
        side: Side,
        /// What to do.
        op: Op,
    },
}

/// A panel of the explorer.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    /// The left panel, which starts on the local disk.
    Left,
    /// The right panel, where sites open.
    Right,
}

/// Why a call failed: the stable code of a refusal, or none when the server could not be reached, and what happened.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct CallError {
    /// The stable code, such as `vault_locked` or `http_404`.
    pub code: Option<String>,
    /// What happened, in words.
    pub message: String,
    /// What the refusal says about itself, such as the fingerprint of a key, by name.
    pub params: std::collections::HashMap<String, serde_json::Value>,
}

/// The answer of the server to a call.
#[derive(Debug, Clone, PartialEq)]
pub enum Reply {
    /// The account.
    Me(Result<User, CallError>),
    /// The version of the server.
    Version(Result<String, CallError>),
    /// The workspaces of the account.
    Workspaces(Result<Vec<Workspace>, CallError>),
    /// The collections and sites of a workspace.
    Tree {
        /// The workspace asked for.
        workspace: Uuid,
        /// Its collections and sites.
        outcome: Result<(Vec<Collection>, Vec<Site>), CallError>,
    },
    /// The state of the vault.
    Vault(Result<VaultStatus, CallError>),
    /// Whether the vault was unlocked.
    Unlocked(Result<(), CallError>),
    /// The account after changing its preferences.
    Preferences(Result<User, CallError>),
    /// The home folder of what a panel shows.
    Homed {
        /// The panel.
        side: Side,
        /// The folder.
        outcome: Result<String, CallError>,
    },
    /// The entries of a folder of a panel.
    Listed {
        /// The panel.
        side: Side,
        /// The folder.
        path: String,
        /// Its entries.
        outcome: Result<Vec<Entry>, CallError>,
    },
    /// The key of a server was trusted, or why not.
    Trusted {
        /// The panel to try again.
        side: Side,
        /// How it went.
        outcome: Result<(), CallError>,
    },
    /// A change of the files of a panel ended.
    Changed {
        /// The panel.
        side: Side,
        /// How it went.
        outcome: Result<(), CallError>,
    },
}

/// What the main screen asks of the interface.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Ask {
    /// Call the server.
    Call(Call),
    /// Work with the files of the local disk in a panel.
    Local {
        /// The panel that asked.
        side: Side,
        /// What to do.
        op: Op,
    },
    /// Open an address in the browser.
    Browse(String),
    /// Keep the settings of this computer.
    Save,
    /// Sign this terminal out.
    Logout,
    /// Go back to the address of the server, with what to say there.
    Leave(Option<String>),
    /// Close the client.
    Quit,
}

/// The panel with the focus.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Focus {
    /// The sites of the workspace.
    Sites,
    /// The left panel of the explorer.
    Left,
    /// The right panel of the explorer.
    Right,
}

/// What a list chooses.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Choice {
    /// A theme, previewed while choosing.
    Theme,
    /// A language.
    Language,
    /// A workspace.
    Workspace,
    /// What a panel shows: the local disk or a site.
    Source(Side),
}

/// What is shown over the screen.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Overlay {
    /// The command line, with the matching command chosen.
    Commands {
        /// What is typed after `/`.
        input: Input,
        /// The chosen match.
        selected: usize,
    },
    /// The settings.
    Settings {
        /// The chosen row.
        selected: usize,
    },
    /// A list to choose from.
    Choose {
        /// What it chooses.
        what: Choice,
        /// The chosen row.
        selected: usize,
        /// The theme worn before previewing, to go back to.
        worn: String,
    },
    /// Every key of the screen.
    Help,
    /// The key of a server to trust, the first time or because it changed.
    HostKey {
        /// The panel that connects.
        side: Side,
        /// The name of the site.
        site: String,
        /// The key the server shows.
        key: HostKey,
        /// The fingerprints trusted until now, when it changed.
        trusted: Option<Vec<String>>,
    },
    /// The master password of the vault.
    Vault {
        /// The password being typed.
        input: Input,
        /// Whether it is being checked.
        busy: bool,
        /// What went wrong last.
        error: Option<String>,
        /// The site to open once unlocked.
        site: Option<Uuid>,
    },
}

/// A short message on the status bar.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Notice {
    /// What it says.
    pub text: String,
    /// Whether something went wrong.
    pub error: bool,
}

/// A row of the sites panel.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Row {
    /// A collection, open or closed.
    Collection {
        /// Its position in the collections.
        index: usize,
        /// How deep it is.
        depth: usize,
        /// Whether its contents are shown.
        open: bool,
    },
    /// A site.
    Site {
        /// Its position in the sites.
        index: usize,
        /// How deep it is.
        depth: usize,
    },
}

/// The main screen.
#[derive(Debug, Clone, PartialEq)]
pub struct Home {
    session: Session,
    email: String,
    locale: String,
    theme: String,
    timezone: String,
    server_version: Option<String>,
    workspaces: Vec<Workspace>,
    workspace: Option<Uuid>,
    collections: Vec<Collection>,
    sites: Vec<Site>,
    closed: HashSet<Uuid>,
    cursor: usize,
    loading: bool,
    vault: Option<VaultStatus>,
    left: Panel,
    right: Panel,
    last: Side,
    sites_shown: bool,
    focus: Focus,
    overlay: Option<Overlay>,
    notice: Option<Notice>,
}

impl Home {
    /// The main screen of a session, loading the account, the version of the server, the workspaces and the vault.
    #[must_use]
    pub fn new(session: Session, email: &str, hidden: bool) -> (Self, Vec<Ask>) {
        let mut home = Self {
            session,
            email: email.to_owned(),
            locale: String::new(),
            theme: String::new(),
            timezone: String::new(),
            server_version: None,
            workspaces: Vec::new(),
            workspace: None,
            collections: Vec::new(),
            sites: Vec::new(),
            closed: HashSet::new(),
            cursor: 0,
            loading: true,
            vault: None,
            left: Panel::new(hidden),
            right: Panel::new(hidden),
            last: Side::Right,
            sites_shown: true,
            focus: Focus::Sites,
            overlay: None,
            notice: None,
        };
        let mut asks: Vec<Ask> = [Call::Me, Call::Version, Call::Workspaces, Call::Vault]
            .into_iter()
            .map(Ask::Call)
            .collect();
        let op = home.left.open(Source::Local);
        asks.push(Ask::Local {
            side: Side::Left,
            op,
        });
        (home, asks)
    }

    /// The session of the screen.
    #[must_use]
    pub fn session(&self) -> &Session {
        &self.session
    }

    /// What is shown over the screen.
    #[must_use]
    pub fn overlay(&self) -> Option<&Overlay> {
        self.overlay.as_ref()
    }

    /// The message on the status bar.
    #[must_use]
    pub fn notice(&self) -> Option<&Notice> {
        self.notice.as_ref()
    }

    /// The site open in the explorer.
    #[must_use]
    pub fn opened(&self) -> Option<&Site> {
        [&self.right, &self.left]
            .into_iter()
            .find_map(|panel| match panel.source() {
                Some(Source::Site { id, .. }) => self.sites.iter().find(|site| site.id == *id),
                _ => None,
            })
    }

    /// A panel of the explorer.
    #[must_use]
    pub fn panel(&self, side: Side) -> &Panel {
        match side {
            Side::Left => &self.left,
            Side::Right => &self.right,
        }
    }

    /// A panel of the explorer, to change.
    fn panel_mut(&mut self, side: Side) -> &mut Panel {
        match side {
            Side::Left => &mut self.left,
            Side::Right => &mut self.right,
        }
    }

    /// The workspace shown.
    #[must_use]
    pub fn workspace(&self) -> Option<&Workspace> {
        self.workspace
            .and_then(|id| self.workspaces.iter().find(|workspace| workspace.id == id))
    }

    /// The panel with the focus.
    #[must_use]
    pub fn focus(&self) -> Focus {
        self.focus
    }

    /// Says something on the status bar.
    pub fn say(&mut self, text: String, error: bool) {
        self.notice = Some(Notice { text, error });
    }

    /// Puts pasted text into the field being typed in: the command line or the master password.
    pub fn paste(&mut self, text: &str) {
        if let Some(Overlay::Commands { input, .. } | Overlay::Vault { input, .. }) =
            &mut self.overlay
        {
            input.insert(text);
        }
    }

    /// Handles a reply of the server.
    pub fn reply(&mut self, reply: Reply, look: &mut Look) -> Vec<Ask> {
        match reply {
            Reply::Me(Ok(user)) | Reply::Preferences(Ok(user)) => {
                self.account(&user, look);
                Vec::new()
            }
            Reply::Version(Ok(version)) => {
                self.server_version = Some(version);
                Vec::new()
            }
            Reply::Workspaces(Ok(workspaces)) => self.workspaces(workspaces),
            Reply::Tree { workspace, outcome } => match outcome {
                Ok((collections, sites)) if Some(workspace) == self.workspace => {
                    self.collections = collections;
                    self.sites = sites;
                    self.loading = false;
                    self.cursor = self.cursor.min(self.rows().len().saturating_sub(1));
                    Vec::new()
                }
                Ok(_) => Vec::new(),
                Err(error) => {
                    self.loading = false;
                    self.trouble(&error, look)
                }
            },
            Reply::Vault(Ok(status)) => {
                self.vault = Some(status);
                Vec::new()
            }
            Reply::Unlocked(outcome) => self.unlocked(outcome, look),
            Reply::Preferences(Err(error)) => {
                look.wear(&self.theme);
                look.speak(&self.locale);
                self.trouble(&error, look)
            }
            Reply::Homed { side, outcome } => {
                let outcome = self.file_outcome(side, outcome, look);
                let (text, mut asks) = outcome;
                let more = self.panel_mut(side).homed(text);
                asks.extend(self.panel_asks(side, more));
                asks
            }
            Reply::Listed {
                side,
                path,
                outcome,
            } => {
                let (outcome, asks) = self.file_outcome(side, outcome, look);
                self.panel_mut(side).listed(&path, outcome);
                asks
            }
            Reply::Trusted { side, outcome } => {
                let (outcome, mut asks) = self.file_outcome(side, outcome, look);
                let more = match outcome {
                    Ok(()) => self.panel_mut(side).retry(),
                    Err(reason) => self.panel_mut(side).changed(Err(reason)),
                };
                asks.extend(self.panel_asks(side, more));
                asks
            }
            Reply::Changed { side, outcome } => {
                let (outcome, mut asks) = self.file_outcome(side, outcome, look);
                let more = self.panel_mut(side).changed(outcome);
                asks.extend(self.panel_asks(side, more));
                asks
            }
            Reply::Me(Err(error))
            | Reply::Version(Err(error))
            | Reply::Workspaces(Err(error))
            | Reply::Vault(Err(error)) => self.trouble(&error, look),
        }
    }

    /// The account as the server says it: its email, and the language and theme it wears.
    fn account(&mut self, user: &User, look: &mut Look) {
        self.email.clone_from(&user.email);
        self.locale.clone_from(&user.locale);
        self.theme.clone_from(&user.theme);
        self.timezone.clone_from(&user.timezone);
        look.speak(&user.locale);
        look.wear(&user.theme);
    }

    /// The workspaces arrived: the one shown stays, otherwise the personal one opens.
    fn workspaces(&mut self, workspaces: Vec<Workspace>) -> Vec<Ask> {
        self.workspaces = workspaces;
        let kept = self
            .workspace
            .filter(|id| self.workspaces.iter().any(|workspace| workspace.id == *id));
        let chosen = kept.or_else(|| {
            self.workspaces
                .iter()
                .find(|workspace| workspace.kind == workspace::Kind::Personal)
                .or(self.workspaces.first())
                .map(|workspace| workspace.id)
        });
        if let Some(id) = chosen {
            self.show_workspace(id)
        } else {
            self.loading = false;
            Vec::new()
        }
    }

    /// Shows a workspace, loading its sites.
    fn show_workspace(&mut self, id: Uuid) -> Vec<Ask> {
        self.workspace = Some(id);
        self.collections.clear();
        self.sites.clear();
        self.closed.clear();
        self.cursor = 0;
        for side in [Side::Left, Side::Right] {
            if matches!(self.panel(side).source(), Some(Source::Site { .. })) {
                self.panel_mut(side).close();
            }
        }
        self.loading = true;
        vec![Ask::Call(Call::Tree(id))]
    }

    /// The vault answered the master password.
    fn unlocked(&mut self, outcome: Result<(), CallError>, look: &Look) -> Vec<Ask> {
        let Some(Overlay::Vault {
            busy, error, site, ..
        }) = &mut self.overlay
        else {
            return Vec::new();
        };
        *busy = false;
        match outcome {
            Ok(()) => {
                let site = *site;
                self.overlay = None;
                self.vault = Some(VaultStatus {
                    initialized: true,
                    unlocked: true,
                });
                self.say(look.messages.get("vault.unlocked"), false);
                if let Some(site) = site {
                    return self.open_site(site, self.last);
                }
            }
            Err(failure) => {
                *error = Some(match failure.code.as_deref() {
                    Some("invalid_master_password") => look.messages.get("vault.wrong"),
                    Some("rate_limited") => look.messages.get("vault.limited"),
                    _ => failure.message,
                });
            }
        }
        Vec::new()
    }

    /// A call failed: an ended session goes back to signing in, a locked vault asks for its password, an old server says so, and
    /// anything else is said on the status bar.
    fn trouble(&mut self, error: &CallError, look: &Look) -> Vec<Ask> {
        let m = &look.messages;
        match error.code.as_deref() {
            Some("unauthenticated") => vec![Ask::Leave(Some(m.get("main.ended")))],
            Some("vault_locked") => {
                self.vault = Some(VaultStatus {
                    initialized: true,
                    unlocked: false,
                });
                self.ask_vault(None);
                Vec::new()
            }
            Some("http_404") => {
                self.say(m.get("main.tooOld"), true);
                Vec::new()
            }
            _ => {
                self.say(error.message.clone(), true);
                Vec::new()
            }
        }
    }

    /// The outcome of a call on the files of a panel, with its failure in words: an ended session goes back to signing in, and a locked
    /// vault asks for its password and opens the site again once unlocked.
    fn file_outcome<T>(
        &mut self,
        side: Side,
        outcome: Result<T, CallError>,
        look: &Look,
    ) -> (Result<T, String>, Vec<Ask>) {
        let mut asks = Vec::new();
        let outcome = outcome.map_err(|error| {
            let (text, more) = self.file_failure(side, &error, look);
            asks = more;
            text
        });
        (outcome, asks)
    }

    /// A failure on the files of a panel in words, with what the main screen must do about it.
    fn file_failure(&mut self, side: Side, error: &CallError, look: &Look) -> (String, Vec<Ask>) {
        let m = &look.messages;
        match error.code.as_deref() {
            Some("unauthenticated") => (
                m.get("main.ended"),
                vec![Ask::Leave(Some(m.get("main.ended")))],
            ),
            Some("vault_locked") => {
                let site = self.panel(side).source().and_then(Source::site);
                self.vault = Some(VaultStatus {
                    initialized: true,
                    unlocked: false,
                });
                self.last = side;
                self.ask_vault(site);
                (m.get("errors.vault_locked"), Vec::new())
            }
            Some(code @ ("host_key_unknown" | "host_key_changed")) => {
                self.ask_trust(side, error, code == "host_key_changed");
                (explain(error, look), Vec::new())
            }
            _ => (explain(error, look), Vec::new()),
        }
    }

    /// Asks to trust the key a server showed, with the host and port of the site of the panel.
    fn ask_trust(&mut self, side: Side, error: &CallError, changed: bool) {
        let site = self.panel(side).source().and_then(Source::site);
        let Some(site) = self.sites.iter().find(|known| Some(known.id) == site) else {
            return;
        };
        let text = |name: &str| {
            error
                .params
                .get(name)
                .and_then(serde_json::Value::as_str)
                .unwrap_or_default()
                .to_owned()
        };
        let setting = |name: &str| site.config.get(name).cloned().unwrap_or_default();
        let key = HostKey {
            host: setting("host"),
            port: setting("port").parse().unwrap_or(22),
            key_type: text("keyType"),
            fingerprint: text("fingerprint"),
            mode: None,
        };
        let trusted = changed.then(|| {
            error
                .params
                .get("trusted")
                .and_then(serde_json::Value::as_array)
                .into_iter()
                .flatten()
                .filter_map(serde_json::Value::as_str)
                .map(str::to_owned)
                .collect()
        });
        self.overlay = Some(Overlay::HostKey {
            side,
            site: site.name.clone(),
            key,
            trusted,
        });
    }

    /// Asks for the master password, to open a site afterwards.
    fn ask_vault(&mut self, site: Option<Uuid>) {
        self.overlay = Some(Overlay::Vault {
            input: Input::default(),
            busy: false,
            error: None,
            site,
        });
    }

    /// The rows of the sites panel: collections with their sub-collections and sites, then the sites outside any collection.
    fn rows(&self) -> Vec<Row> {
        let mut rows = Vec::new();
        self.branch(None, 0, &mut rows);
        rows
    }

    /// Adds the collections and sites inside a collection, or at the top.
    fn branch(&self, parent: Option<Uuid>, depth: usize, rows: &mut Vec<Row>) {
        for (index, collection) in self.collections.iter().enumerate() {
            if collection.parent_id != parent {
                continue;
            }
            let open = !self.closed.contains(&collection.id);
            rows.push(Row::Collection { index, depth, open });
            if open {
                self.branch(Some(collection.id), depth + 1, rows);
            }
        }
        for (index, site) in self.sites.iter().enumerate() {
            if site.collection_id == parent {
                rows.push(Row::Site { index, depth });
            }
        }
    }

    /// Handles a pressed key.
    pub fn key(&mut self, key: KeyEvent, look: &mut Look) -> Vec<Ask> {
        if let Some(overlay) = self.overlay.take() {
            return self.overlay_key(key, overlay, look);
        }
        if let Some(side) = self
            .focused_side()
            .filter(|side| self.panel(*side).captures())
        {
            return self.explorer_key(side, key);
        }
        let control = key.modifiers.contains(KeyModifiers::CONTROL);
        match key.code {
            KeyCode::Char('?') | KeyCode::F(1) => self.overlay = Some(Overlay::Help),
            KeyCode::Char('/') => {
                self.notice = None;
                self.overlay = Some(Overlay::Commands {
                    input: Input::default(),
                    selected: 0,
                });
            }
            KeyCode::Char('q') | KeyCode::F(10) => return vec![Ask::Quit],
            KeyCode::Char('b') if control => {
                self.sites_shown = !self.sites_shown;
                if self.sites_shown {
                    self.focus = Focus::Sites;
                } else if self.focus == Focus::Sites {
                    self.focus = Self::focus_of(self.last);
                }
            }
            KeyCode::Tab => self.cycle(true),
            KeyCode::BackTab => self.cycle(false),
            _ => {
                return match self.focused_side() {
                    None => self.sites_key(key, look),
                    Some(side) => self.explorer_key(side, key),
                };
            }
        }
        Vec::new()
    }

    /// The focus of a panel of the explorer.
    fn focus_of(side: Side) -> Focus {
        match side {
            Side::Left => Focus::Left,
            Side::Right => Focus::Right,
        }
    }

    /// The panel of the explorer with the focus, if one has it.
    fn focused_side(&self) -> Option<Side> {
        match self.focus {
            Focus::Left => Some(Side::Left),
            Focus::Right => Some(Side::Right),
            Focus::Sites => None,
        }
    }

    /// Moves the focus to the next or previous panel: the sites, when shown, then the left and the right panel.
    fn cycle(&mut self, forward: bool) {
        let mut order = vec![Focus::Left, Focus::Right];
        if self.sites_shown {
            order.insert(0, Focus::Sites);
        }
        let at = order
            .iter()
            .position(|focus| *focus == self.focus)
            .unwrap_or(0);
        let next = if forward {
            (at + 1) % order.len()
        } else {
            (at + order.len() - 1) % order.len()
        };
        self.focus = order[next];
        if let Some(side) = self.focused_side() {
            self.last = side;
        }
    }

    /// Handles a key on the focused panel of the explorer.
    fn explorer_key(&mut self, side: Side, key: KeyEvent) -> Vec<Ask> {
        let asks = self.panel_mut(side).key(key);
        self.panel_asks(side, asks)
    }

    /// What the main screen does for what a panel asks: its files go to the local disk or to the server, and choosing what it shows
    /// opens the list of sources.
    fn panel_asks(&mut self, side: Side, asks: Vec<PanelAsk>) -> Vec<Ask> {
        let mut out = Vec::new();
        for ask in asks {
            match ask {
                PanelAsk::Choose => {
                    let worn = String::new();
                    self.overlay = Some(Overlay::Choose {
                        what: Choice::Source(side),
                        selected: 0,
                        worn,
                    });
                }
                PanelAsk::Op(Op::Disconnect) => {
                    out.extend(self.files(side, Op::Disconnect));
                    self.panel_mut(side).close();
                }
                PanelAsk::Op(op) => out.extend(self.files(side, op)),
            }
        }
        out
    }

    /// Where an operation on the files of a panel goes: the local disk, or the site it shows in the workspace.
    fn files(&self, side: Side, op: Op) -> Option<Ask> {
        let source = self.panel(side).source()?;
        let Some(site) = source.site() else {
            return Some(Ask::Local { side, op });
        };
        let workspace = self.workspace?;
        Some(Ask::Call(Call::Files {
            workspace,
            site,
            side,
            op,
        }))
    }

    /// Handles a key on the sites panel.
    fn sites_key(&mut self, key: KeyEvent, look: &mut Look) -> Vec<Ask> {
        let rows = self.rows();
        let last = rows.len().saturating_sub(1);
        match key.code {
            KeyCode::Down | KeyCode::Char('j') => self.cursor = (self.cursor + 1).min(last),
            KeyCode::Up | KeyCode::Char('k') => self.cursor = self.cursor.saturating_sub(1),
            KeyCode::Home | KeyCode::Char('g') => self.cursor = 0,
            KeyCode::End | KeyCode::Char('G') => self.cursor = last,
            KeyCode::Char('w') => return self.choose(Choice::Workspace, look),
            KeyCode::Right | KeyCode::Char('l' | 'h') | KeyCode::Left | KeyCode::Enter => {
                match rows.get(self.cursor) {
                    Some(Row::Collection { index, open, .. }) => {
                        let id = self.collections[*index].id;
                        let opening = match key.code {
                            KeyCode::Right | KeyCode::Char('l') => true,
                            KeyCode::Enter => !open,
                            _ => false,
                        };
                        if opening {
                            self.closed.remove(&id);
                        } else {
                            self.closed.insert(id);
                        }
                    }
                    Some(Row::Site { index, .. }) if key.code == KeyCode::Enter => {
                        let id = self.sites[*index].id;
                        return self.open_site(id, self.last);
                    }
                    _ => {}
                }
            }
            _ => {}
        }
        Vec::new()
    }

    /// Opens a site in a panel of the explorer and gives it the focus, first asking for the master password when its credentials are in
    /// a locked vault.
    fn open_site(&mut self, id: Uuid, side: Side) -> Vec<Ask> {
        let Some(site) = self.sites.iter().find(|site| site.id == id) else {
            return Vec::new();
        };
        let locked = self
            .vault
            .as_ref()
            .is_some_and(|vault| vault.initialized && !vault.unlocked);
        if locked && !site.secrets.is_empty() {
            self.last = side;
            self.ask_vault(Some(site.id));
            return Vec::new();
        }
        let source = Source::Site {
            id,
            name: site.name.clone(),
        };
        self.last = side;
        self.focus = Self::focus_of(side);
        let op = self.panel_mut(side).open(source);
        self.files(side, op).into_iter().collect()
    }

    /// Handles a key on what is shown over the screen.
    fn overlay_key(&mut self, key: KeyEvent, overlay: Overlay, look: &mut Look) -> Vec<Ask> {
        match overlay {
            Overlay::Help => Vec::new(),
            Overlay::HostKey {
                side,
                site,
                key: mut shown,
                trusted,
            } => {
                shown.mode = match (key.code, &trusted) {
                    (KeyCode::Char('y') | KeyCode::Enter, None) => None,
                    (KeyCode::Char('a'), Some(_)) => Some(TrustMode::Add),
                    (KeyCode::Char('r'), Some(_)) => Some(TrustMode::Replace),
                    (KeyCode::Char('n') | KeyCode::Esc, _) => return Vec::new(),
                    _ => {
                        self.overlay = Some(Overlay::HostKey {
                            side,
                            site,
                            key: shown,
                            trusted,
                        });
                        return Vec::new();
                    }
                };
                self.workspace
                    .map(|workspace| {
                        Ask::Call(Call::Trust {
                            workspace,
                            side,
                            key: shown,
                        })
                    })
                    .into_iter()
                    .collect()
            }
            Overlay::Commands { input, selected } => self.commands_key(key, input, selected, look),
            Overlay::Settings { selected } => self.settings_key(key, selected, look),
            Overlay::Choose {
                what,
                selected,
                worn,
            } => self.choose_key(key, what, selected, worn, look),
            Overlay::Vault {
                mut input,
                busy,
                error,
                site,
            } => {
                if busy {
                    self.overlay = Some(Overlay::Vault {
                        input,
                        busy,
                        error,
                        site,
                    });
                    return Vec::new();
                }
                match key.code {
                    KeyCode::Esc => Vec::new(),
                    KeyCode::Enter if !input.value().is_empty() => {
                        let password = input.value().to_owned();
                        self.overlay = Some(Overlay::Vault {
                            input,
                            busy: true,
                            error: None,
                            site,
                        });
                        vec![Ask::Call(Call::Unlock(password))]
                    }
                    _ => {
                        input.handle(key);
                        self.overlay = Some(Overlay::Vault {
                            input,
                            busy,
                            error,
                            site,
                        });
                        Vec::new()
                    }
                }
            }
        }
    }

    /// The commands whose name starts with what is typed before a space.
    fn matches(input: &Input) -> Vec<&'static str> {
        let typed = input.value().split(' ').next().unwrap_or("");
        COMMANDS
            .into_iter()
            .filter(|name| name.starts_with(typed))
            .collect()
    }

    /// Handles a key on the command line.
    fn commands_key(
        &mut self,
        key: KeyEvent,
        mut input: Input,
        mut selected: usize,
        look: &mut Look,
    ) -> Vec<Ask> {
        let matches = Self::matches(&input);
        match key.code {
            KeyCode::Esc => return Vec::new(),
            KeyCode::Backspace if input.value().is_empty() => return Vec::new(),
            KeyCode::Down => selected = (selected + 1).min(matches.len().saturating_sub(1)),
            KeyCode::Up => selected = selected.saturating_sub(1),
            KeyCode::Tab => {
                if let Some(name) = matches.get(selected) {
                    input = Input::with(&format!("{name} "));
                    selected = 0;
                }
            }
            KeyCode::Enter => {
                let typed = input.value().trim().to_owned();
                let (name, value) = typed.split_once(' ').unwrap_or((&typed, ""));
                let command = if COMMANDS.contains(&name) {
                    Some(name)
                } else {
                    matches.get(selected).copied()
                };
                return if let Some(command) = command {
                    self.run(command, value.trim(), look)
                } else {
                    self.say(
                        look.messages.fill("commands.unknown", &[("name", name)]),
                        true,
                    );
                    Vec::new()
                };
            }
            _ => {
                if input.handle(key) {
                    selected = 0;
                }
            }
        }
        self.overlay = Some(Overlay::Commands { input, selected });
        Vec::new()
    }

    /// Runs a command of the command line, with the value typed after it.
    fn run(&mut self, command: &str, value: &str, look: &mut Look) -> Vec<Ask> {
        match command {
            "config" => {
                self.overlay = Some(Overlay::Settings { selected: 0 });
                Vec::new()
            }
            "theme" if value.is_empty() => self.choose(Choice::Theme, look),
            "theme" => {
                let wanted = value.to_lowercase();
                match theme::all()
                    .into_iter()
                    .find(|theme| theme.id == wanted || theme.name.to_lowercase() == wanted)
                {
                    Some(theme) => Self::set_theme(&theme.id, look),
                    None => self.nothing_called(value, look),
                }
            }
            "language" if value.is_empty() => self.choose(Choice::Language, look),
            "language" => match value.to_lowercase().as_str() {
                "en" | "english" | "inglés" | "ingles" => {
                    Self::set_language(Language::English, look)
                }
                "es" | "español" | "espanol" | "spanish" => {
                    Self::set_language(Language::Spanish, look)
                }
                _ => self.nothing_called(value, look),
            },
            "workspace" if value.is_empty() => self.choose(Choice::Workspace, look),
            "workspace" => {
                let wanted = value.to_lowercase();
                match self
                    .workspaces
                    .iter()
                    .find(|workspace| workspace.name.to_lowercase() == wanted)
                {
                    Some(workspace) => self.show_workspace(workspace.id),
                    None => self.nothing_called(value, look),
                }
            }
            "server" => vec![Ask::Leave(None)],
            "web" => vec![Ask::Browse(format!(
                "{}/",
                self.session.server.origin().ascii_serialization()
            ))],
            "keys" => {
                self.overlay = Some(Overlay::Help);
                Vec::new()
            }
            "logout" => {
                self.say(look.messages.get("main.signingOut"), false);
                vec![Ask::Logout]
            }
            _ => vec![Ask::Quit],
        }
    }

    /// Says that nothing goes by a name.
    fn nothing_called(&mut self, value: &str, look: &Look) -> Vec<Ask> {
        self.say(
            look.messages.fill("commands.noMatch", &[("value", value)]),
            true,
        );
        Vec::new()
    }

    /// The names of what a list chooses from, with the one in use.
    fn choices(&self, what: Choice, look: &Look) -> (Vec<String>, usize) {
        match what {
            Choice::Theme => {
                let themes = theme::all();
                let current = themes
                    .iter()
                    .position(|theme| theme.id == look.theme)
                    .unwrap_or(0);
                (
                    themes.into_iter().map(|theme| theme.name).collect(),
                    current,
                )
            }
            Choice::Language => (
                vec!["English".to_owned(), "Español".to_owned()],
                usize::from(look.messages.language() == Language::Spanish),
            ),
            Choice::Workspace => (
                self.workspaces
                    .iter()
                    .map(|workspace| workspace.name.clone())
                    .collect(),
                self.workspaces
                    .iter()
                    .position(|workspace| Some(workspace.id) == self.workspace)
                    .unwrap_or(0),
            ),
            Choice::Source(side) => {
                let mut names = vec![look.messages.get("main.local")];
                names.extend(self.sites.iter().map(|site| site.name.clone()));
                (names, self.source_index(side))
            }
        }
    }

    /// The row of the list of sources that a panel shows: the local disk first, then the sites.
    fn source_index(&self, side: Side) -> usize {
        let site = self.panel(side).source().and_then(Source::site);
        self.sites
            .iter()
            .position(|known| Some(known.id) == site)
            .map_or(0, |at| at + 1)
    }

    /// Opens a list on the value in use.
    fn choose(&mut self, what: Choice, look: &Look) -> Vec<Ask> {
        let (_, selected) = self.choices(what, look);
        self.overlay = Some(Overlay::Choose {
            what,
            selected,
            worn: look.theme.clone(),
        });
        Vec::new()
    }

    /// Handles a key on a list: themes are worn while moving, Enter keeps the choice, Esc goes back to what was worn.
    fn choose_key(
        &mut self,
        key: KeyEvent,
        what: Choice,
        mut selected: usize,
        worn: String,
        look: &mut Look,
    ) -> Vec<Ask> {
        let (names, _) = self.choices(what, look);
        let last = names.len().saturating_sub(1);
        match key.code {
            KeyCode::Esc => {
                look.wear(&worn);
                return Vec::new();
            }
            KeyCode::Enter if !names.is_empty() => {
                look.wear(&worn);
                return match what {
                    Choice::Theme => Self::set_theme(&theme::all()[selected].id, look),
                    Choice::Language => Self::set_language(
                        if selected == 0 {
                            Language::English
                        } else {
                            Language::Spanish
                        },
                        look,
                    ),
                    Choice::Workspace => self.show_workspace(self.workspaces[selected].id),
                    Choice::Source(side) if selected == 0 => {
                        self.focus = Self::focus_of(side);
                        self.last = side;
                        let op = self.panel_mut(side).open(Source::Local);
                        self.files(side, op).into_iter().collect()
                    }
                    Choice::Source(side) => self.open_site(self.sites[selected - 1].id, side),
                };
            }
            KeyCode::Down | KeyCode::Char('j') => selected = (selected + 1).min(last),
            KeyCode::Up | KeyCode::Char('k') => selected = selected.saturating_sub(1),
            _ => {}
        }
        if what == Choice::Theme {
            look.wear(&theme::all()[selected].id);
        }
        self.overlay = Some(Overlay::Choose {
            what,
            selected,
            worn,
        });
        Vec::new()
    }

    /// Wears a theme and keeps it in the account.
    fn set_theme(id: &str, look: &mut Look) -> Vec<Ask> {
        look.wear(id);
        vec![Ask::Call(Call::Preferences {
            locale: None,
            theme: Some(id.to_owned()),
        })]
    }

    /// Speaks a language and keeps it in the account.
    fn set_language(language: Language, look: &mut Look) -> Vec<Ask> {
        look.speak(language.tag());
        vec![Ask::Call(Call::Preferences {
            locale: Some(language.tag().to_owned()),
            theme: None,
        })]
    }

    /// Handles a key on the settings: ← → change the value in place, Enter opens the list of the language and the theme.
    fn settings_key(&mut self, key: KeyEvent, mut selected: usize, look: &mut Look) -> Vec<Ask> {
        let forward = match key.code {
            KeyCode::Esc => return Vec::new(),
            KeyCode::Down | KeyCode::Char('j') => {
                selected = (selected + 1).min(SETTINGS.len() - 1);
                None
            }
            KeyCode::Up | KeyCode::Char('k') => {
                selected = selected.saturating_sub(1);
                None
            }
            KeyCode::Enter if selected < 2 => {
                return self.choose([Choice::Language, Choice::Theme][selected], look);
            }
            KeyCode::Right | KeyCode::Char('l' | ' ') | KeyCode::Enter => Some(true),
            KeyCode::Left | KeyCode::Char('h') => Some(false),
            _ => None,
        };
        self.overlay = Some(Overlay::Settings { selected });
        forward.map_or_else(Vec::new, |forward| {
            self.change(SETTINGS[selected], forward, look)
        })
    }

    /// Changes a setting to its next or previous value.
    fn change(&mut self, setting: &str, forward: bool, look: &mut Look) -> Vec<Ask> {
        let settings = &mut look.settings;
        match setting {
            "language" => {
                let next = if look.messages.language() == Language::English {
                    Language::Spanish
                } else {
                    Language::English
                };
                return Self::set_language(next, look);
            }
            "theme" => {
                let themes = theme::all();
                let at = themes
                    .iter()
                    .position(|theme| theme.id == look.theme)
                    .unwrap_or(0);
                let next = if forward {
                    (at + 1) % themes.len()
                } else {
                    (at + themes.len() - 1) % themes.len()
                };
                return Self::set_theme(&themes[next].id, look);
            }
            "timezone" => {
                self.say(look.messages.get("settings.timezoneWeb"), false);
                return Vec::new();
            }
            "colors" => {
                settings.colors = match settings.colors {
                    Colors::Theme => Colors::Terminal,
                    Colors::Terminal => Colors::Theme,
                };
                let worn = look.theme.clone();
                look.wear(&worn);
            }
            "enterOnFile" => {
                settings.enter_on_file = match settings.enter_on_file {
                    EnterOnFile::Copy => EnterOnFile::Details,
                    EnterOnFile::Details => EnterOnFile::Copy,
                };
            }
            "hiddenFiles" => settings.hidden_files = !settings.hidden_files,
            "confirmQuit" => settings.confirm_quit = !settings.confirm_quit,
            _ => settings.check_updates = !settings.check_updates,
        }
        vec![Ask::Save]
    }

    /// Whether the server and this client differ in their major or minor version, when both say one.
    fn mismatched(&self) -> Option<&str> {
        let server = self.server_version.as_deref()?;
        let minor = |version: &str| {
            let mut parts = version.trim_start_matches('v').split('.');
            let major: u32 = parts.next()?.parse().ok()?;
            let minor: u32 = parts.next()?.parse().ok()?;
            Some((major, minor))
        };
        (minor(server)? != minor(VERSION)?).then_some(server)
    }

    /// Draws the screen.
    pub fn render(&self, frame: &mut Frame, look: &Look, spinner: &str) {
        let m = &look.messages;
        let area = frame.area();
        if area.width < 60 || area.height < 16 {
            let text = Paragraph::new(m.get("main.small"))
                .wrap(Wrap { trim: true })
                .centered();
            frame.render_widget(text, centered(area, area.width, 3));
            return;
        }
        let [top, body, status] = Layout::vertical([
            Constraint::Length(1),
            Constraint::Fill(1),
            Constraint::Length(1),
        ])
        .areas(area);
        self.render_tabs(frame, top, look);
        let zone = jiff::tz::TimeZone::get(&self.timezone).unwrap_or(jiff::tz::TimeZone::UTC);
        if area.width >= NARROW {
            let sites_width = if self.sites_shown { SITES_WIDTH } else { 0 };
            let [sites, left, right] = Layout::horizontal([
                Constraint::Length(sites_width),
                Constraint::Fill(1),
                Constraint::Fill(1),
            ])
            .areas(body);
            if self.sites_shown {
                self.render_sites(frame, sites, look, spinner);
            }
            self.left
                .render(frame, left, look, self.focus == Focus::Left, &zone);
            self.right
                .render(frame, right, look, self.focus == Focus::Right, &zone);
        } else {
            match self.focus {
                Focus::Sites => self.render_sites(frame, body, look, spinner),
                Focus::Left => self.left.render(frame, body, look, true, &zone),
                Focus::Right => self.right.render(frame, body, look, true, &zone),
            }
        }
        match &self.overlay {
            Some(Overlay::Commands { input, selected }) => {
                Self::render_commands(frame, status, input, *selected, look);
            }
            overlay => {
                self.render_status(frame, status, look);
                match overlay {
                    Some(Overlay::Settings { selected }) => {
                        self.render_settings(frame, *selected, look);
                    }
                    Some(Overlay::Choose { what, selected, .. }) => {
                        self.render_choose(frame, *what, *selected, look);
                    }
                    Some(Overlay::Help) => render_help(frame, look),
                    Some(Overlay::HostKey {
                        site, key, trusted, ..
                    }) => render_host_key(frame, site, key, trusted.as_deref(), look),
                    Some(Overlay::Vault {
                        input,
                        busy,
                        error,
                        site,
                    }) => self.render_vault(
                        frame,
                        input,
                        *busy,
                        error.as_deref(),
                        *site,
                        look,
                        spinner,
                    ),
                    _ => {}
                }
            }
        }
    }

    /// The line of tabs: the one tab, with what its explorer shows, and the version on the right.
    fn render_tabs(&self, frame: &mut Frame, area: Rect, look: &Look) {
        let palette = &look.palette;
        let title = self
            .opened()
            .map_or_else(|| look.messages.get("main.local"), |site| site.name.clone());
        let tab = Span::styled(format!(" 1 {title} "), active(palette));
        let version = format!("transferly-tui {VERSION} ");
        let [left, right] = Layout::horizontal([
            Constraint::Fill(1),
            Constraint::Length(u16::try_from(version.chars().count()).unwrap_or(u16::MAX)),
        ])
        .areas(area);
        frame.render_widget(
            Paragraph::new(Line::from(tab)).style(Style::new().bg(palette.surface)),
            left,
        );
        frame.render_widget(
            Paragraph::new(Line::from(version).fg(palette.muted))
                .style(Style::new().bg(palette.surface)),
            right,
        );
    }

    /// The sites panel: the collections and sites of the workspace, with a dot on the open site.
    fn render_sites(&self, frame: &mut Frame, area: Rect, look: &Look, spinner: &str) {
        let (m, palette) = (&look.messages, &look.palette);
        let title = self
            .workspace()
            .map_or_else(|| m.get("main.sites"), |workspace| workspace.name.clone());
        let block = panel(&title, self.focus == Focus::Sites, palette);
        let inside = block.inner(area);
        frame.render_widget(block, area);
        if self.loading {
            frame.render_widget(
                Paragraph::new(format!("{spinner} {}", m.get("main.loading"))),
                inside,
            );
            return;
        }
        let rows = self.rows();
        if rows.is_empty() {
            frame.render_widget(
                Paragraph::new(Line::from(m.get("main.noSites")).fg(palette.muted))
                    .wrap(Wrap { trim: true }),
                inside,
            );
            return;
        }
        let height = usize::from(inside.height.max(1));
        let skip = self.cursor.saturating_sub(height - 1);
        let width = usize::from(inside.width);
        let lines: Vec<Line> = rows
            .iter()
            .enumerate()
            .skip(skip)
            .take(height)
            .map(|(at, row)| {
                let text = match *row {
                    Row::Collection { index, depth, open } => format!(
                        "{}{} {}",
                        "  ".repeat(depth),
                        if open { "▾" } else { "▸" },
                        self.collections[index].name
                    ),
                    Row::Site { index, depth } => {
                        let site = &self.sites[index];
                        let dot = if [Side::Left, Side::Right].iter().any(|side| matches!(self.panel(*side).source(), Some(Source::Site { id, .. }) if *id == site.id)) {
                            "●"
                        } else {
                            "·"
                        };
                        format!("{}{dot} {}", "  ".repeat(depth), site.name)
                    }
                };
                let text: String = text.chars().take(width).collect();
                let mut line = Line::from(format!("{text:<width$}"));
                if matches!(row, Row::Collection { .. }) {
                    line = line.add_modifier(Modifier::BOLD);
                }
                if at == self.cursor && self.focus == Focus::Sites {
                    highlight(line, palette)
                } else {
                    line
                }
            })
            .collect();
        frame.render_widget(Paragraph::new(lines), inside);
    }

    /// The status bar: the server, the account, the workspace, the vault and a notice, with the keys of the focused panel on the right.
    fn render_status(&self, frame: &mut Frame, area: Rect, look: &Look) {
        let (m, palette) = (&look.messages, &look.palette);
        let vault = match &self.vault {
            Some(VaultStatus {
                initialized: false, ..
            }) => m.get("main.vaultNone"),
            Some(VaultStatus { unlocked: true, .. }) => m.get("main.vaultOpen"),
            Some(_) => m.get("main.vaultLocked"),
            None => String::new(),
        };
        let server = self
            .session
            .server
            .host_str()
            .unwrap_or_default()
            .to_owned();
        let workspace = self
            .workspace()
            .map_or_default(|workspace| workspace.name.clone());
        let mut spans: Vec<Span> = vec![Span::raw(" ")];
        let parts = [server, self.email.clone(), workspace, vault];
        for (at, part) in parts
            .into_iter()
            .filter(|part| !part.is_empty())
            .enumerate()
        {
            if at > 0 {
                spans.push(Span::raw(" · ").fg(palette.faint));
            }
            spans.push(Span::raw(part));
        }
        if let Some(server) = self.mismatched() {
            spans.push(Span::raw(" · ").fg(palette.faint));
            spans.push(
                Span::raw(m.fill("main.versions", &[("server", server), ("client", VERSION)]))
                    .fg(palette.warn),
            );
        }
        if let Some(notice) = &self.notice {
            spans.push(Span::raw(" · ").fg(palette.faint));
            spans.push(if notice.error {
                Span::styled(notice.text.clone(), error_line("", palette).style)
            } else {
                Span::raw(notice.text.clone()).fg(palette.ok)
            });
        }
        let keys = m.get(match self.focus {
            Focus::Sites => "main.keysSites",
            Focus::Left | Focus::Right => "main.keysExplorer",
        });
        let used: usize = spans.iter().map(Span::width).sum();
        let room = usize::from(area.width).saturating_sub(used + 2);
        let keys = if keys.chars().count() <= room {
            keys
        } else {
            String::new()
        };
        let [left, right] = Layout::horizontal([
            Constraint::Fill(1),
            Constraint::Length(u16::try_from(keys.chars().count() + 1).unwrap_or(u16::MAX)),
        ])
        .areas(area);
        let bar = Style::new().bg(palette.surface);
        frame.render_widget(Paragraph::new(Line::from(spans)).style(bar), left);
        frame.render_widget(
            Paragraph::new(Line::from(keys).fg(palette.muted)).style(bar),
            right,
        );
    }

    /// The command line in place of the status bar, with the matching commands above it.
    fn render_commands(frame: &mut Frame, area: Rect, input: &Input, selected: usize, look: &Look) {
        let (m, palette) = (&look.messages, &look.palette);
        let line = Line::from(vec![
            Span::raw("/").fg(palette.accent).bold(),
            Span::raw(input.value().to_owned()),
        ]);
        frame.render_widget(
            Paragraph::new(line).style(Style::new().bg(palette.surface)),
            area,
        );
        let column = u16::try_from(input.cursor() + 1).unwrap_or(u16::MAX);
        frame.set_cursor_position((area.x.saturating_add(column), area.y));
        let matches = Self::matches(input);
        if matches.is_empty() {
            return;
        }
        let height = u16::try_from(matches.len()).unwrap_or(u16::MAX) + 2;
        let width = 66.min(area.width);
        let list = Rect::new(area.x, area.y.saturating_sub(height), width, height);
        let inside = popup(
            frame,
            list,
            &m.get("commands.title"),
            &m.get("commands.hint"),
            palette,
        );
        let lines: Vec<Line> = matches
            .iter()
            .enumerate()
            .map(|(at, name)| {
                let line = Line::from(vec![
                    Span::raw(format!("/{name:<12}")).bold(),
                    Span::raw(m.get(&format!("commands.{name}"))).fg(palette.muted),
                ]);
                if at == selected {
                    highlight(line, palette)
                } else {
                    line
                }
            })
            .collect();
        frame.render_widget(Paragraph::new(lines), inside);
    }

    /// The settings with their values and where each lives.
    fn render_settings(&self, frame: &mut Frame, selected: usize, look: &Look) {
        let (m, palette) = (&look.messages, &look.palette);
        let area = centered(
            frame.area(),
            78,
            u16::try_from(SETTINGS.len()).unwrap_or(0) + 6,
        );
        let inside = popup(
            frame,
            area,
            &m.get("settings.title"),
            &m.get("settings.keys"),
            palette,
        );
        let settings = &look.settings;
        let yes_no = |value: bool| m.get(if value { "settings.yes" } else { "settings.no" });
        let mut lines = Vec::new();
        for (at, setting) in SETTINGS.iter().enumerate() {
            let value = match *setting {
                "language" => match look.messages.language() {
                    Language::English => "English".to_owned(),
                    Language::Spanish => "Español".to_owned(),
                },
                "theme" => theme::find(&look.theme).name,
                "timezone" => self.timezone.clone(),
                "colors" => m.get(match settings.colors {
                    Colors::Theme => "settings.colorsTheme",
                    Colors::Terminal => "settings.colorsTerminal",
                }),
                "enterOnFile" => m.get(match settings.enter_on_file {
                    EnterOnFile::Copy => "settings.enterCopy",
                    EnterOnFile::Details => "settings.enterDetails",
                }),
                "hiddenFiles" => m.get(if settings.hidden_files {
                    "settings.shown"
                } else {
                    "settings.hidden"
                }),
                "confirmQuit" => yes_no(settings.confirm_quit),
                _ => m.get(if settings.check_updates {
                    "settings.daily"
                } else {
                    "settings.never"
                }),
            };
            let place = m.get(if at < 3 {
                "settings.account"
            } else {
                "settings.computer"
            });
            let line = Line::from(vec![
                Span::raw(format!("{:<30}", m.get(&format!("settings.{setting}")))),
                Span::raw(format!("‹ {value:<22} ›")).bold(),
                Span::raw(format!("  {place}")).fg(palette.muted),
            ]);
            lines.push(if at == selected {
                highlight(line, palette)
            } else {
                line
            });
        }
        lines.push(Line::default());
        lines.push(Line::from(m.get("settings.note")).fg(palette.muted));
        frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: false }), inside);
    }

    /// A list to choose a theme, a language or a workspace, with a check on the one in use.
    fn render_choose(&self, frame: &mut Frame, what: Choice, selected: usize, look: &Look) {
        let (m, palette) = (&look.messages, &look.palette);
        let (names, _) = self.choices(what, look);
        let current = match what {
            Choice::Theme => theme::all().iter().position(|theme| theme.id == self.theme),
            Choice::Language => Some(usize::from(self.locale.starts_with("es"))),
            Choice::Workspace => self
                .workspaces
                .iter()
                .position(|workspace| Some(workspace.id) == self.workspace),
            Choice::Source(side) => Some(self.source_index(side)),
        };
        let title = m.get(match what {
            Choice::Theme => "choose.theme",
            Choice::Language => "choose.language",
            Choice::Workspace => "choose.workspace",
            Choice::Source(_) => "choose.source",
        });
        let height = u16::try_from(names.len())
            .unwrap_or(u16::MAX)
            .saturating_add(2)
            .min(18);
        let area = centered(frame.area(), 44, height);
        let inside = popup(frame, area, &title, &m.get("choose.keys"), palette);
        let visible = usize::from(inside.height.max(1));
        let skip = selected.saturating_sub(visible - 1);
        let lines: Vec<Line> = names
            .iter()
            .enumerate()
            .skip(skip)
            .take(visible)
            .map(|(at, name)| {
                let check = if Some(at) == current { "✓" } else { " " };
                let line = Line::from(format!(" {check} {name}"));
                if at == selected {
                    highlight(line, palette)
                } else {
                    line
                }
            })
            .collect();
        frame.render_widget(Paragraph::new(lines), inside);
    }

    /// The popup asking for the master password.
    #[allow(
        clippy::too_many_arguments,
        reason = "the parts of the popup, as the overlay holds them"
    )]
    fn render_vault(
        &self,
        frame: &mut Frame,
        input: &Input,
        busy: bool,
        error: Option<&str>,
        site: Option<Uuid>,
        look: &Look,
        spinner: &str,
    ) {
        let (m, palette) = (&look.messages, &look.palette);
        let area = centered(frame.area(), 66, 12);
        let inside = popup(
            frame,
            area,
            &m.get("vault.title"),
            &m.get("vault.keys"),
            palette,
        );
        let [text, password, status] = Layout::vertical([
            Constraint::Length(3),
            Constraint::Length(3),
            Constraint::Fill(1),
        ])
        .areas(inside);
        let intro = match site.and_then(|id| self.sites.iter().find(|site| site.id == id)) {
            Some(site) => m.fill("vault.forSite", &[("site", &site.name)]),
            None => m.get("vault.intro"),
        };
        frame.render_widget(Paragraph::new(intro).wrap(Wrap { trim: true }), text);
        field(
            frame,
            password,
            palette,
            &m.get("vault.field"),
            &input.masked(),
            !busy,
        );
        let line = if busy {
            Line::from(format!("{spinner} {}", m.get("vault.checking"))).bold()
        } else {
            error.map_or_else(Line::default, |text| error_line(text, palette))
        };
        frame.render_widget(Paragraph::new(line).wrap(Wrap { trim: true }), status);
    }
}

/// The style of the active tab: the accent background, or reversed without color.
fn active(palette: &Palette) -> Style {
    if palette.plain {
        Style::new().add_modifier(Modifier::REVERSED | Modifier::BOLD)
    } else {
        Style::new()
            .bg(palette.accent)
            .fg(palette.ink)
            .add_modifier(Modifier::BOLD)
    }
}

/// The keys of the help, grouped as the design groups them: the message of each group and its keys with the message of their action.
const HELP: [(&str, &[(&str, &str)]); 4] = [
    (
        "help.everywhere",
        &[
            ("?  F1", "help.help"),
            ("/", "help.commands"),
            ("Ctrl+B", "help.sites"),
            ("Tab", "help.focus"),
            ("q  F10  Ctrl+C", "help.quit"),
        ],
    ),
    (
        "help.sitesPanel",
        &[
            ("↑ ↓  j k", "help.move"),
            ("g G  Home End", "help.ends"),
            ("→ l  ← h", "help.collections"),
            ("Enter", "help.open"),
            ("w", "help.workspace"),
        ],
    ),
    (
        "help.explorer",
        &[
            ("Enter ← Backspace", "help.openUp"),
            ("Space  Ctrl+A", "help.mark"),
            ("n  F2  Del", "help.manage"),
            ("f  .  s  S", "help.filterKeys"),
            ("r  Ctrl+G", "help.refresh"),
            ("o  Ctrl+D", "help.show"),
        ],
    ),
    (
        "help.popups",
        &[
            ("Enter", "help.confirm"),
            ("Esc", "help.back"),
            ("← →", "help.change"),
        ],
    ),
];

/// Every key of the screen.
fn render_help(frame: &mut Frame, look: &Look) {
    let (m, palette) = (&look.messages, &look.palette);
    let rows: usize = HELP.iter().map(|(_, keys)| keys.len() + 2).sum();
    let area = centered(
        frame.area(),
        70,
        u16::try_from(rows).unwrap_or(u16::MAX).saturating_add(2),
    );
    let inside = popup(
        frame,
        area,
        &m.get("help.title"),
        &m.get("help.keys"),
        palette,
    );
    let mut lines = Vec::new();
    for (group, keys) in HELP {
        lines.push(Line::from(m.get(group)).fg(palette.accent_text).bold());
        for (key, action) in keys {
            lines.push(Line::from(vec![
                Span::raw(format!("  {key:<18}")).bold(),
                Span::raw(m.get(action)),
            ]));
        }
        lines.push(Line::default());
    }
    frame.render_widget(Paragraph::new(lines), inside);
}

/// A failure in words: the message of its stable code when the catalog has one, or what happened.
fn explain(error: &CallError, look: &Look) -> String {
    let Some(code) = &error.code else {
        return error.message.clone();
    };
    let key = format!("errors.{code}");
    let text = look.messages.get(&key);
    if text == key {
        error.message.clone()
    } else {
        text
    }
}

/// The key of a server to trust: the site, the server, the type and fingerprint of the key, and when it changed the fingerprints
/// trusted until now, in the danger color.
fn render_host_key(
    frame: &mut Frame,
    site: &str,
    key: &HostKey,
    trusted: Option<&[String]>,
    look: &Look,
) {
    let (m, palette) = (&look.messages, &look.palette);
    let rows = trusted.map_or(0, <[String]>::len);
    let height = u16::try_from(rows)
        .unwrap_or(0)
        .saturating_add(if trusted.is_some() { 13 } else { 11 });
    let area = centered(frame.area(), 78, height);
    let (title, keys, intro) = if trusted.is_some() {
        (
            "hostKey.changedTitle",
            "hostKey.changedKeys",
            "hostKey.changedIntro",
        )
    } else {
        ("hostKey.title", "hostKey.keys", "hostKey.intro")
    };
    let inside = popup(frame, area, &m.get(title), &m.get(keys), palette);
    let intro = m.fill(intro, &[("site", site)]);
    let mut lines = vec![
        if trusted.is_some() {
            error_line(&intro, palette)
        } else {
            Line::from(intro)
        },
        Line::default(),
        Line::from(vec![
            Span::raw(format!("{:<14}", m.get("hostKey.server"))).fg(palette.muted),
            Span::raw(format!("{}:{}", key.host, key.port)),
        ]),
        Line::from(vec![
            Span::raw(format!("{:<14}", m.get("hostKey.type"))).fg(palette.muted),
            Span::raw(key.key_type.clone()),
        ]),
        Line::from(vec![
            Span::raw(format!("{:<14}", m.get("hostKey.fingerprint"))).fg(palette.muted),
            Span::raw(key.fingerprint.clone()).bold(),
        ]),
    ];
    if let Some(trusted) = trusted {
        lines.push(Line::default());
        lines.push(Line::from(m.get("hostKey.trusted")).fg(palette.muted));
        lines.extend(
            trusted
                .iter()
                .map(|fingerprint| Line::from(format!("  {fingerprint}"))),
        );
    }
    lines.push(Line::default());
    lines.push(Line::from(m.get("hostKey.check")).fg(palette.muted));
    frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: false }), inside);
}
