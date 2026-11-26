//! The state of the interface and how events change it (docs/design/terminal-client.md, Screens 1 and 2). Handling an event is pure: it
//! returns the effects to run, such as probing a server or opening the browser, and the runtime feeds their results back as events.

use std::cell::Cell;
use std::time::{Duration, Instant};

use crossterm::event::{KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::Frame;
use ratatui::layout::{Constraint, Layout, Position, Rect};
use ratatui::style::Stylize;
use ratatui::text::Line;
use ratatui::widgets::{Paragraph, Wrap};
use url::Url;

use super::explorer::ListingTicket;
use super::home::{Ask, CallError, Home, Reply, Session, Side, explain};
use super::input::Input;
use super::look::Look;
use super::mouse::MouseKind;
use super::theme::{ColorMode, Palette};
use super::widgets::{background, card, error_line, field, highlight};
use crate::config::{self, Profile, Settings};
use crate::i18n::{Language, Messages};
use crate::login::{Answer, Pkce};

/// Frames of the spinner that shows something is waiting.
pub(super) const SPINNER: [&str; 10] = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

/// Why signing in failed, as the runtime reports it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Failure {
    /// The server refused with a stable code.
    Refused(String),
    /// The server could not be reached or answered something else.
    Unreachable(String),
    /// The session started but its token could not be kept.
    NotKept(String),
}

/// What happens to the interface.
#[derive(Debug, Clone, PartialEq)]
pub enum Event {
    /// A key was pressed.
    Key(KeyEvent),
    /// A mouse button or wheel action.
    Mouse {
        /// The action at the pointer.
        kind: MouseKind,
        /// The horizontal position in terminal cells.
        column: u16,
        /// The vertical position in terminal cells.
        row: u16,
        /// When the terminal delivered the action.
        at: Instant,
    },
    /// Text was pasted into the terminal.
    Paste(String),
    /// Time passed, for the spinner.
    Tick,
    /// The probe of a server ended: it is a Transferly server, or why not.
    Probed(Result<(), String>),
    /// The listener on 127.0.0.1 started, with its redirect address, or could not.
    Listening(Result<String, String>),
    /// The address of the approval page of a request: a short link, or the full address on an older server.
    Shortened {
        /// The state of the request.
        state: String,
        /// The redirect of the request, if it has one.
        redirect: Option<String>,
        /// The address.
        url: String,
    },
    /// The browser opened the approval page, or not.
    BrowserOpened(bool),
    /// The browser answered through the listener, or waiting failed.
    Answered(Result<Answer, String>),
    /// The session started and its token was kept; the account brings its language and theme.
    SignedIn {
        /// Origin of the server.
        server: String,
        /// Email of the account.
        email: String,
        /// Language of the account.
        locale: String,
        /// Theme of the account.
        theme: String,
        /// Token of the session.
        token: String,
    },
    /// The server answered a call.
    Replied(Reply),
    /// A folder listing ended, with the ticket of the request that started it.
    ListingReply {
        /// The panel that requested the listing.
        side: Side,
        /// The identity of that listing.
        ticket: ListingTicket,
        /// The result to give the panel if it is still waiting.
        reply: Reply,
    },
    /// Signing out ended, or why it failed.
    LoggedOut(Result<(), CallError>),
    /// The settings of this computer were kept, or why not.
    Saved(Result<(), String>),
    /// Signing in failed.
    SignInFailed(Failure),
}

/// What the runtime must do for the interface.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Effect {
    /// Check that a server is a Transferly server.
    Probe(Url),
    /// Start the listener on 127.0.0.1 and wait for an answer with this state.
    Listen {
        /// The state the answer must carry.
        state: String,
    },
    /// Stop the listener.
    StopListening,
    /// Get the address of the approval page of a request, a short link when the server has them.
    Shorten {
        /// The server.
        server: Url,
        /// The PKCE challenge.
        challenge: String,
        /// The state the callback returns.
        state: String,
        /// The redirect to the listener, or none to paste the code.
        redirect: Option<String>,
        /// The name of this computer.
        host: String,
    },
    /// Open an address in the browser.
    OpenBrowser(String),
    /// Copy text to the clipboard of the terminal.
    Copy(String),
    /// Exchange a code for a session, keep its token and record the sign-in.
    Exchange {
        /// The server.
        server: Url,
        /// The code from the approval page.
        code: String,
        /// The PKCE verifier.
        verifier: String,
        /// The redirect of the approval, if it had one.
        redirect: Option<String>,
    },
    /// Call the server in a session.
    Call(Session, super::home::Call),
    /// End a session on its server and forget its token.
    Logout(Session),
    /// Listen to the event stream of a session, in place of any other.
    Stream(Session),
    /// Stop listening to the event stream.
    StopStream,
    /// Move the bytes of an upload or a download, then list a panel again.
    Transfer(Session, super::transfers::Job, super::home::Side),
    /// Stop moving the bytes of a transfer.
    Abort(uuid::Uuid),
    /// Work with the files of the local disk for a panel.
    Local {
        /// The panel.
        side: super::home::Side,
        /// What to do.
        op: super::explorer::Op,
    },
    /// Keep the settings of this computer.
    SaveSettings(Settings),
    /// Change terminal mouse capture.
    Mouse(bool),
    /// Leave the interface.
    Quit,
}

/// The screen the interface shows.
#[derive(Debug, Clone, PartialEq)]
pub enum Screen {
    /// The address of the server, with the profiles used before.
    Server {
        /// The address being typed.
        input: Input,
        /// The profile chosen with the arrows, if any.
        selected: Option<usize>,
        /// Whether the server is being checked.
        checking: bool,
        /// What went wrong last.
        error: Option<String>,
    },
    /// Waiting for the approval in the browser.
    Approval {
        /// The approval address.
        url: String,
        /// Whether the browser opened it.
        opened: bool,
        /// A short notice, such as that the address was copied.
        notice: Option<String>,
        /// Whether the code is being exchanged.
        exchanging: bool,
    },
    /// Pasting the code shown by the approval page.
    Paste {
        /// The approval address, without a redirect.
        url: String,
        /// The code being pasted.
        input: Input,
        /// Whether the code is being exchanged.
        exchanging: bool,
        /// What went wrong last.
        error: Option<String>,
    },
    /// Signed in: the main screen.
    Main(Box<Home>),
}

/// A session kept on this computer: the server, the account and the token.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Kept {
    /// Origin of the server.
    pub server: String,
    /// Email of the account.
    pub email: String,
    /// Token of the session.
    pub token: String,
}

/// What the interface starts with.
pub struct Setup {
    /// The language before signing in.
    pub language: Language,
    /// How this terminal shows colors.
    pub mode: ColorMode,
    /// The theme to wear before the account says which.
    pub theme: String,
    /// The settings of this computer.
    pub settings: Settings,
    /// The servers signed in to before.
    pub profiles: Vec<Profile>,
    /// The name of this computer.
    pub host: String,
    /// Whether a browser can open here.
    pub browser_possible: bool,
    /// The session kept for the current server, if any.
    pub signed_in: Option<Kept>,
}

/// The interface.
pub struct App {
    screen: Screen,
    look: Look,
    profiles: Vec<Profile>,
    host: String,
    browser_possible: bool,
    server: Option<Url>,
    pkce: Pkce,
    redirect: Option<String>,
    spinner: usize,
    pending: Vec<Effect>,
    area: Cell<Rect>,
    click: Option<(Position, Instant)>,
}

impl App {
    /// The interface on its first screen: the main screen of a kept session, or asking for the server with the last one used filled
    /// in.
    #[must_use]
    pub fn new(setup: Setup) -> Self {
        let screen = Screen::Server {
            input: Input::with(
                setup
                    .profiles
                    .first()
                    .map_or("", |profile| profile.server.as_str()),
            ),
            selected: None,
            checking: false,
            error: None,
        };
        let mut app = Self {
            screen,
            look: Look::new(setup.language, &setup.theme, setup.mode, setup.settings),
            profiles: setup.profiles,
            host: setup.host,
            browser_possible: setup.browser_possible,
            server: None,
            pkce: Pkce::new(),
            redirect: None,
            spinner: 0,
            pending: Vec::new(),
            area: Cell::default(),
            click: None,
        };
        if let Some(kept) = setup.signed_in {
            app.pending = app.enter(&kept.server, &kept.email, kept.token);
        }
        app
    }

    /// What the interface must do as it starts, such as loading the main screen of a kept session.
    pub fn start(&mut self) -> Vec<Effect> {
        std::mem::take(&mut self.pending)
    }

    /// Opens the main screen of a session.
    fn enter(&mut self, server: &str, email: &str, token: String) -> Vec<Effect> {
        let Ok(url) = Url::parse(server) else {
            self.back_to_server(None);
            return Vec::new();
        };
        self.server = Some(url.clone());
        let session = Session { server: url, token };
        let (home, asks) = Home::new(session.clone(), email, self.look.settings.hidden_files);
        self.screen = Screen::Main(Box::new(home));
        self.asks(&session, asks)
    }

    /// What the runtime must do for what the main screen asks.
    fn asks(&mut self, session: &Session, asks: Vec<Ask>) -> Vec<Effect> {
        let mut effects = Vec::new();
        for ask in asks {
            match ask {
                Ask::Call(call) => effects.push(Effect::Call(session.clone(), call)),
                Ask::Local { side, op } => effects.push(Effect::Local { side, op }),
                Ask::Stream => effects.push(Effect::Stream(session.clone())),
                Ask::Transfer(job, side) => {
                    effects.push(Effect::Transfer(session.clone(), job, side));
                }
                Ask::Abort(id) => effects.push(Effect::Abort(id)),
                Ask::Browse(url) => effects.push(Effect::OpenBrowser(url)),
                Ask::CopyPath(path) => effects.push(Effect::Copy(path)),
                Ask::Mouse(enabled) => effects.push(Effect::Mouse(enabled)),
                Ask::Save => effects.push(Effect::SaveSettings(self.look.settings.clone())),
                Ask::Logout => effects.push(Effect::Logout(session.clone())),
                Ask::Leave(message) => {
                    self.back_to_server(message);
                    effects.push(Effect::StopStream);
                    return effects;
                }
                Ask::Quit => effects.push(Effect::Quit),
            }
        }
        effects
    }

    /// How the interface looks and speaks.
    #[must_use]
    pub fn look(&self) -> &Look {
        &self.look
    }

    /// The screen shown.
    #[must_use]
    pub fn screen(&self) -> &Screen {
        &self.screen
    }

    /// The ticket to attach to a listing reply from this panel.
    pub(super) fn listing_ticket(&self, side: Side) -> Option<ListingTicket> {
        match &self.screen {
            Screen::Main(home) => home.panel(side).listing_ticket(),
            _ => None,
        }
    }

    /// The messages in the language shown.
    #[must_use]
    pub fn messages(&self) -> &Messages {
        &self.look.messages
    }

    /// The colors the interface paints with.
    #[must_use]
    pub fn palette(&self) -> &Palette {
        &self.look.palette
    }

    /// Goes back to asking for the server, with the server tried last and what went wrong.
    fn back_to_server(&mut self, error: Option<String>) {
        self.screen = Screen::Server {
            input: Input::with(&self.server_text()),
            selected: None,
            checking: false,
            error,
        };
    }

    /// Shows the screen to paste the code, with the approval address without a redirect.
    fn show_paste(&mut self) -> Effect {
        self.redirect = None;
        self.screen = Screen::Paste {
            url: String::new(),
            input: Input::default(),
            exchanging: false,
            error: None,
        };
        self.shorten()
    }

    /// Asks the server for the address of the approval page of the current request, short when it can be.
    fn shorten(&self) -> Effect {
        Effect::Shorten {
            server: self.probed_server(),
            challenge: self.pkce.challenge.clone(),
            state: self.pkce.state.clone(),
            redirect: self.redirect.clone(),
            host: self.host.clone(),
        }
    }

    /// The address of the approval page arrived: the screen of the same request shows it, and the approval opens it in the browser.
    fn shortened(
        &mut self,
        state: &str,
        redirect: Option<&String>,
        address: String,
    ) -> Vec<Effect> {
        if state != self.pkce.state || redirect != self.redirect.as_ref() {
            return Vec::new();
        }
        match &mut self.screen {
            Screen::Approval { url, .. } => {
                url.clone_from(&address);
                vec![Effect::OpenBrowser(address)]
            }
            Screen::Paste { url, .. } => {
                *url = address;
                Vec::new()
            }
            _ => Vec::new(),
        }
    }

    /// The message of a failed sign-in.
    fn explain(&self, failure: &Failure) -> String {
        match failure {
            Failure::Refused(code)
                if ["invalid_grant", "cli_login_disabled", "http_404"].contains(&code.as_str()) =>
            {
                self.look.messages.get(&format!("exchange.{code}"))
            }
            Failure::Refused(code) => explain(
                &CallError {
                    code: Some(code.clone()),
                    ..Default::default()
                },
                &self.look,
            ),
            Failure::Unreachable(_) => self.look.messages.get("exchange.unreachable"),
            Failure::NotKept(reason) => self
                .look
                .messages
                .fill("exchange.notKept", &[("reason", reason)]),
        }
    }

    /// Changes the interface for an event and returns what the runtime must do.
    pub fn handle(&mut self, event: Event) -> Vec<Effect> {
        if matches!(event, Event::Key(_) | Event::Paste(_)) {
            self.click = None;
        }
        match event {
            Event::Mouse {
                kind,
                column,
                row,
                at,
            } => self.mouse(kind, Position::new(column, row), at),
            Event::Key(key) if key.kind != KeyEventKind::Press => Vec::new(),
            Event::Key(key)
                if key.modifiers.contains(KeyModifiers::CONTROL)
                    && key.code == KeyCode::Char('c')
                    && !matches!(
                        &self.screen,
                        Screen::Main(home) if home.focused_listing().is_some()
                    ) =>
            {
                vec![Effect::StopListening, Effect::Quit]
            }
            Event::Key(key) => self.key(key),
            Event::Paste(text) => {
                self.paste(&text);
                Vec::new()
            }
            Event::Tick => {
                self.spinner = (self.spinner + 1) % SPINNER.len();
                if let Screen::Main(home) = &mut self.screen {
                    home.tick();
                }
                Vec::new()
            }
            Event::Probed(outcome) => self.probed(outcome.is_ok()),
            Event::Listening(outcome) => self.listening(outcome),
            Event::Shortened {
                state,
                redirect,
                url,
            } => self.shortened(&state, redirect.as_ref(), url),
            Event::BrowserOpened(opened) => {
                match &mut self.screen {
                    Screen::Approval { opened: shown, .. } => *shown = opened,
                    Screen::Main(home) if !opened => {
                        home.say(self.look.messages.get("main.noBrowser"), true);
                    }
                    _ => {}
                }
                Vec::new()
            }
            Event::Answered(outcome) => self.answered(outcome),
            Event::SignedIn {
                server,
                email,
                locale,
                theme,
                token,
            } => {
                self.look.speak(&locale);
                self.look.wear(&theme);
                self.enter(&server, &email, token)
            }
            Event::ListingReply { side, ticket, .. }
                if self.listing_ticket(side).as_ref() != Some(&ticket) =>
            {
                Vec::new()
            }
            Event::Replied(reply) | Event::ListingReply { reply, .. } => match &mut self.screen {
                Screen::Main(home) => {
                    let session = home.session().clone();
                    let asks = home.reply(reply, &mut self.look);
                    self.asks(&session, asks)
                }
                _ => Vec::new(),
            },
            Event::LoggedOut(outcome) => self.logged_out(outcome),
            Event::Saved(outcome) => {
                if let (Err(reason), Screen::Main(home)) = (outcome, &mut self.screen) {
                    home.say(
                        self.look
                            .messages
                            .fill("main.notSaved", &[("reason", &reason)]),
                        true,
                    );
                }
                Vec::new()
            }
            Event::SignInFailed(failure) => {
                self.failed(&failure);
                Vec::new()
            }
        }
    }

    /// Signing out ended: back to the address of the server, or the reason on the status bar.
    fn logged_out(&mut self, outcome: Result<(), CallError>) -> Vec<Effect> {
        match (outcome, &mut self.screen) {
            (Ok(()), _) => {
                let message = self
                    .look
                    .messages
                    .fill("main.signedOut", &[("server", &self.server_text())]);
                self.back_to_server(Some(message));
                return vec![Effect::StopStream];
            }
            (Err(error), Screen::Main(home)) => {
                let reason = explain(&error, &self.look);
                home.say(
                    self.look
                        .messages
                        .fill("main.notSignedOut", &[("reason", &reason)]),
                    true,
                );
            }
            (Err(_), _) => {}
        }
        Vec::new()
    }

    /// The server tried last, as its origin.
    fn server_text(&self) -> String {
        self.server
            .as_ref()
            .map_or_else(String::new, |server| server.origin().ascii_serialization())
    }

    /// The server checked last.
    ///
    /// # Panics
    ///
    /// No server was checked, which the order of the screens rules out.
    fn probed_server(&self) -> Url {
        self.server.clone().expect("a probed server")
    }

    /// Puts pasted text into the field being typed in.
    fn paste(&mut self, text: &str) {
        match &mut self.screen {
            Screen::Server {
                input, selected, ..
            } => {
                input.insert(text);
                *selected = None;
            }
            Screen::Paste { input, .. } => input.insert(text),
            Screen::Main(home) => home.paste(text),
            Screen::Approval { .. } => {}
        }
    }

    /// The server was checked: with a browser the listener starts, without one the code is pasted.
    fn probed(&mut self, reachable: bool) -> Vec<Effect> {
        if !reachable {
            let error = self
                .look
                .messages
                .fill("server.unreachable", &[("server", &self.server_text())]);
            self.back_to_server(Some(error));
            return Vec::new();
        }
        self.pkce = Pkce::new();
        if self.browser_possible {
            return vec![Effect::Listen {
                state: self.pkce.state.clone(),
            }];
        }
        vec![self.show_paste()]
    }

    /// The listener started, so the browser opens the approval page; without a listener the code is pasted.
    fn listening(&mut self, outcome: Result<String, String>) -> Vec<Effect> {
        let Ok(redirect) = outcome else {
            return vec![self.show_paste()];
        };
        self.redirect = Some(redirect);
        self.screen = Screen::Approval {
            url: String::new(),
            opened: false,
            notice: None,
            exchanging: false,
        };
        vec![self.shorten()]
    }

    /// The browser answered: an approval is exchanged, a denial or a failed wait goes back to the server.
    fn answered(&mut self, outcome: Result<Answer, String>) -> Vec<Effect> {
        let error = match outcome {
            Ok(Answer::Approved(code)) => {
                if let Screen::Approval { exchanging, .. } = &mut self.screen {
                    *exchanging = true;
                }
                return vec![Effect::Exchange {
                    server: self.probed_server(),
                    code,
                    verifier: self.pkce.verifier.clone(),
                    redirect: self.redirect.clone(),
                }];
            }
            Ok(Answer::Denied) => self.look.messages.get("approval.denied"),
            Err(reason) => self
                .look
                .messages
                .fill("approval.failed", &[("reason", &reason)]),
        };
        self.back_to_server(Some(error));
        Vec::new()
    }

    /// Signing in failed: a pasted code stays to be fixed, anything else goes back to the server.
    fn failed(&mut self, failure: &Failure) {
        let error = self.explain(failure);
        if let Screen::Paste {
            exchanging,
            error: shown,
            ..
        } = &mut self.screen
        {
            *exchanging = false;
            *shown = Some(error);
        } else {
            self.back_to_server(Some(error));
        }
    }

    /// Handles a pressed key on the current screen.
    fn key(&mut self, key: KeyEvent) -> Vec<Effect> {
        match &mut self.screen {
            Screen::Server {
                input,
                selected,
                checking,
                error,
            } => {
                if *checking {
                    return Vec::new();
                }
                match key.code {
                    KeyCode::Esc => return vec![Effect::Quit],
                    KeyCode::Down if !self.profiles.is_empty() => {
                        *selected = Some(
                            selected.map_or(0, |index| (index + 1).min(self.profiles.len() - 1)),
                        );
                    }
                    KeyCode::Up => *selected = selected.and_then(|index| index.checked_sub(1)),
                    KeyCode::Enter => {
                        let typed = selected.map_or_else(
                            || input.value().to_owned(),
                            |index| self.profiles[index].server.clone(),
                        );
                        match config::server_origin(&typed) {
                            Ok(server) => {
                                *input = Input::with(&server.origin().ascii_serialization());
                                *selected = None;
                                *checking = true;
                                *error = None;
                                self.server = Some(server.clone());
                                return vec![Effect::Probe(server)];
                            }
                            Err(_) => *error = Some(self.look.messages.get("server.invalid")),
                        }
                    }
                    _ => {
                        if input.handle(key) {
                            *selected = None;
                        }
                    }
                }
                Vec::new()
            }
            Screen::Approval {
                url,
                notice,
                exchanging,
                ..
            } => match key.code {
                _ if *exchanging => Vec::new(),
                KeyCode::Char('o') if !url.is_empty() => vec![Effect::OpenBrowser(url.clone())],
                KeyCode::Char('c') if !url.is_empty() => {
                    *notice = Some(self.look.messages.get("approval.copied"));
                    vec![Effect::Copy(url.clone())]
                }
                KeyCode::Char('p') => vec![Effect::StopListening, self.show_paste()],
                KeyCode::Esc => {
                    self.back_to_server(None);
                    vec![Effect::StopListening]
                }
                _ => Vec::new(),
            },
            Screen::Paste {
                input,
                exchanging,
                error,
                ..
            } => {
                if *exchanging {
                    return Vec::new();
                }
                match key.code {
                    KeyCode::Esc => self.back_to_server(None),
                    KeyCode::Enter if !input.value().trim().is_empty() => {
                        *exchanging = true;
                        *error = None;
                        let code = input.value().trim().to_owned();
                        return vec![Effect::Exchange {
                            server: self.probed_server(),
                            code,
                            verifier: self.pkce.verifier.clone(),
                            redirect: None,
                        }];
                    }
                    _ => {
                        input.handle(key);
                    }
                }
                Vec::new()
            }
            Screen::Main(home) => {
                let session = home.session().clone();
                let asks = home.key(key, &mut self.look);
                self.asks(&session, asks)
            }
        }
    }

    /// Recognizes consecutive left clicks at the same cell and routes them to the main screen.
    fn mouse(&mut self, kind: MouseKind, position: Position, at: Instant) -> Vec<Effect> {
        let previous = self.click.take();
        if !self.look.settings.mouse {
            return Vec::new();
        }
        let Screen::Main(home) = &mut self.screen else {
            return Vec::new();
        };
        let double = kind == MouseKind::Left
            && previous.is_some_and(|(last, time)| {
                last == position && at.saturating_duration_since(time) <= Duration::from_millis(400)
            });
        let (asks, selected) = home.mouse(self.area.get(), position, kind, double, &mut self.look);
        if kind == MouseKind::Left && selected && !double {
            self.click = Some((position, at));
        }
        let session = home.session().clone();
        self.asks(&session, asks)
    }

    /// Draws the current screen.
    pub fn render(&self, frame: &mut Frame) {
        self.area.set(frame.area());
        background(frame, &self.look.palette);
        match &self.screen {
            Screen::Server {
                input,
                selected,
                checking,
                error,
            } => self.render_server(frame, input, *selected, *checking, error.as_deref()),
            Screen::Approval {
                url,
                opened,
                notice,
                exchanging,
            } => self.render_approval(frame, url, *opened, notice.as_deref(), *exchanging),
            Screen::Paste {
                url,
                input,
                exchanging,
                error,
            } => self.render_paste(frame, url, input, *exchanging, error.as_deref()),
            Screen::Main(home) => home.render(frame, &self.look, self.spinner()),
        }
    }

    /// The card of a sign-in screen, with the title and keys of its messages; returns its inside.
    fn card(
        &self,
        frame: &mut Frame,
        area: ratatui::layout::Rect,
        screen: &str,
    ) -> ratatui::layout::Rect {
        let m = &self.look.messages;
        let (name, tagline) = (m.get("app.name"), m.get("app.tagline"));
        card(
            frame,
            area,
            &self.look.palette,
            (&name, &tagline),
            &m.get(&format!("{screen}.title")),
            &m.get(&format!("{screen}.keys")),
        )
    }

    /// The frame of the spinner.
    fn spinner(&self) -> &'static str {
        SPINNER[self.spinner]
    }

    /// Draws the address of the server, what is happening with it, and the profiles used before.
    fn render_server(
        &self,
        frame: &mut Frame,
        input: &Input,
        selected: Option<usize>,
        checking: bool,
        error: Option<&str>,
    ) {
        let (m, palette) = (&self.look.messages, &self.look.palette);
        let inside = self.card(frame, frame.area(), "server");
        let [address, status, list] = Layout::vertical([
            Constraint::Length(3),
            Constraint::Length(2),
            Constraint::Fill(1),
        ])
        .areas(inside);
        field(
            frame,
            address,
            palette,
            &m.get("server.field"),
            input,
            selected.is_none() && !checking,
        );
        let status_line = if checking {
            Line::from(format!(
                "{} {}",
                self.spinner(),
                m.fill("server.checking", &[("server", input.value())])
            ))
            .bold()
        } else {
            error.map_or_else(Line::default, |text| error_line(text, palette))
        };
        frame.render_widget(
            Paragraph::new(status_line).wrap(Wrap { trim: true }),
            status,
        );
        if self.profiles.is_empty() {
            return;
        }
        let mut lines = vec![Line::from(m.get("server.used")).fg(palette.muted)];
        for (index, profile) in self.profiles.iter().enumerate() {
            let line = Line::from(format!(
                "  {:<36}{}",
                profile.server,
                profile.account.as_deref().unwrap_or("")
            ));
            lines.push(if selected == Some(index) {
                highlight(line, palette)
            } else {
                line
            });
        }
        frame.render_widget(Paragraph::new(lines), list);
    }

    /// Draws the wait for the approval in the browser.
    fn render_approval(
        &self,
        frame: &mut Frame,
        url: &str,
        opened: bool,
        notice: Option<&str>,
        exchanging: bool,
    ) {
        let (m, palette) = (&self.look.messages, &self.look.palette);
        let inside = self.card(frame, frame.area(), "approval");
        let intro = m.fill(
            if opened {
                "approval.opened"
            } else {
                "approval.notOpened"
            },
            &[("host", &self.host)],
        );
        let waiting = m.get(if exchanging {
            "signed.title"
        } else {
            "approval.waiting"
        });
        let mut lines = vec![
            Line::from(intro),
            Line::default(),
            Line::from(url.to_owned()).fg(palette.info).bold(),
            Line::default(),
            Line::from(format!("{} {waiting}", self.spinner())).bold(),
            Line::default(),
            Line::from(m.get("approval.noBrowser")).fg(palette.muted),
        ];
        if let Some(text) = notice {
            lines.push(Line::from(text.to_owned()).fg(palette.ok));
        }
        frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: false }), inside);
    }

    /// Draws the field to paste the code, with the address to open elsewhere.
    fn render_paste(
        &self,
        frame: &mut Frame,
        url: &str,
        input: &Input,
        exchanging: bool,
        error: Option<&str>,
    ) {
        let (m, palette) = (&self.look.messages, &self.look.palette);
        let inside = self.card(frame, frame.area(), "paste");
        let [text, code, note] = Layout::vertical([
            Constraint::Length(6),
            Constraint::Length(3),
            Constraint::Fill(1),
        ])
        .areas(inside);
        let lines = vec![
            Line::from(m.get("paste.intro")),
            Line::from(url.to_owned()).fg(palette.info).bold(),
            Line::from(m.get("paste.then")),
        ];
        frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: false }), text);
        field(
            frame,
            code,
            palette,
            &m.get("paste.field"),
            input,
            !exchanging,
        );
        let mut notes = vec![Line::from(m.get("paste.note")).fg(palette.muted)];
        if exchanging {
            notes.push(Line::from(self.spinner()).bold());
        }
        if let Some(text) = error {
            notes.push(error_line(text, palette));
        }
        frame.render_widget(Paragraph::new(notes).wrap(Wrap { trim: true }), note);
    }
}
