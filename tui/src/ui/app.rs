//! The state of the interface and how events change it (docs/design/terminal-client.md, Screens 1 and 2). Handling an event is pure: it
//! returns the effects to run, such as probing a server or opening the browser, and the runtime feeds their results back as events.

use crossterm::event::{KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::Frame;
use ratatui::layout::{Constraint, Layout};
use ratatui::style::{Modifier, Style, Stylize};
use ratatui::text::Line;
use ratatui::widgets::{Paragraph, Wrap};
use url::Url;

use super::input::Input;
use super::theme::{self, ColorMode, Palette};
use super::widgets::{background, card, error_line, field};
use crate::config::{self, Profile};
use crate::i18n::{Language, Messages};
use crate::login::{self, Answer, Pkce};

/// Frames of the spinner that shows something is waiting.
const SPINNER: [&str; 10] = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

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
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Event {
    /// A key was pressed.
    Key(KeyEvent),
    /// Text was pasted into the terminal.
    Paste(String),
    /// Time passed, for the spinner.
    Tick,
    /// The probe of a server ended: it is a Transferly server, or why not.
    Probed(Result<(), String>),
    /// The listener on 127.0.0.1 started, with its redirect address, or could not.
    Listening(Result<String, String>),
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
    },
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
    /// Leave the interface.
    Quit,
}

/// The screen the interface shows.
#[derive(Debug, Clone, PartialEq, Eq)]
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
    /// Signed in.
    Signed {
        /// Origin of the server.
        server: String,
        /// Email of the account.
        email: String,
    },
}

/// What the interface starts with.
pub struct Setup {
    /// The language before signing in.
    pub language: Language,
    /// How this terminal shows colors.
    pub mode: ColorMode,
    /// The theme to wear before the account says which.
    pub theme: String,
    /// The servers signed in to before.
    pub profiles: Vec<Profile>,
    /// The name of this computer.
    pub host: String,
    /// Whether a browser can open here.
    pub browser_possible: bool,
    /// The server and account already signed in to, if any.
    pub signed_in: Option<(String, String)>,
}

/// The interface.
pub struct App {
    screen: Screen,
    messages: Messages,
    mode: ColorMode,
    palette: Palette,
    profiles: Vec<Profile>,
    host: String,
    browser_possible: bool,
    server: Option<Url>,
    pkce: Pkce,
    redirect: Option<String>,
    spinner: usize,
}

impl App {
    /// The interface on its first screen: signed in, or asking for the server with the last one used filled in.
    #[must_use]
    pub fn new(setup: Setup) -> Self {
        let screen = match setup.signed_in {
            Some((server, email)) => Screen::Signed { server, email },
            None => Screen::Server {
                input: Input::with(
                    setup
                        .profiles
                        .first()
                        .map_or("", |profile| profile.server.as_str()),
                ),
                selected: None,
                checking: false,
                error: None,
            },
        };
        Self {
            screen,
            messages: Messages::new(setup.language),
            mode: setup.mode,
            palette: Palette::new(&theme::find(&setup.theme), setup.mode),
            profiles: setup.profiles,
            host: setup.host,
            browser_possible: setup.browser_possible,
            server: None,
            pkce: Pkce::new(),
            redirect: None,
            spinner: 0,
        }
    }

    /// The screen shown.
    #[must_use]
    pub fn screen(&self) -> &Screen {
        &self.screen
    }

    /// The messages in the language shown.
    #[must_use]
    pub fn messages(&self) -> &Messages {
        &self.messages
    }

    /// The colors the interface paints with.
    #[must_use]
    pub fn palette(&self) -> &Palette {
        &self.palette
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
    fn show_paste(&mut self) {
        let server = self.probed_server();
        let url = login::authorize_url(&server, &self.pkce, None, &self.host).to_string();
        self.redirect = None;
        self.screen = Screen::Paste {
            url,
            input: Input::default(),
            exchanging: false,
            error: None,
        };
    }

    /// The message of a failed sign-in.
    fn explain(&self, failure: &Failure) -> String {
        match failure {
            Failure::Refused(code)
                if ["invalid_grant", "cli_login_disabled", "http_404"].contains(&code.as_str()) =>
            {
                self.messages.get(&format!("exchange.{code}"))
            }
            Failure::Refused(reason) | Failure::Unreachable(reason) => {
                self.messages.fill("exchange.other", &[("reason", reason)])
            }
            Failure::NotKept(reason) => self
                .messages
                .fill("exchange.notKept", &[("reason", reason)]),
        }
    }

    /// Changes the interface for an event and returns what the runtime must do.
    pub fn handle(&mut self, event: Event) -> Vec<Effect> {
        match event {
            Event::Key(key) if key.kind != KeyEventKind::Press => Vec::new(),
            Event::Key(key)
                if key.modifiers.contains(KeyModifiers::CONTROL)
                    && key.code == KeyCode::Char('c') =>
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
                Vec::new()
            }
            Event::Probed(outcome) => self.probed(outcome.is_ok()),
            Event::Listening(outcome) => self.listening(outcome),
            Event::BrowserOpened(opened) => {
                if let Screen::Approval { opened: shown, .. } = &mut self.screen {
                    *shown = opened;
                }
                Vec::new()
            }
            Event::Answered(outcome) => self.answered(outcome),
            Event::SignedIn {
                server,
                email,
                locale,
                theme,
            } => {
                self.messages = Messages::new(Language::from_tag(&locale));
                self.palette = Palette::new(&theme::find(&theme), self.mode);
                self.screen = Screen::Signed { server, email };
                Vec::new()
            }
            Event::SignInFailed(failure) => {
                self.failed(&failure);
                Vec::new()
            }
        }
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
            Screen::Approval { .. } | Screen::Signed { .. } => {}
        }
    }

    /// The server was checked: with a browser the listener starts, without one the code is pasted.
    fn probed(&mut self, reachable: bool) -> Vec<Effect> {
        if !reachable {
            let error = self
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
        self.show_paste();
        Vec::new()
    }

    /// The listener started, so the browser opens the approval page; without a listener the code is pasted.
    fn listening(&mut self, outcome: Result<String, String>) -> Vec<Effect> {
        let Ok(redirect) = outcome else {
            self.show_paste();
            return Vec::new();
        };
        let url = login::authorize_url(
            &self.probed_server(),
            &self.pkce,
            Some(&redirect),
            &self.host,
        )
        .to_string();
        self.redirect = Some(redirect);
        self.screen = Screen::Approval {
            url: url.clone(),
            opened: false,
            notice: None,
            exchanging: false,
        };
        vec![Effect::OpenBrowser(url)]
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
            Ok(Answer::Denied) => self.messages.get("approval.denied"),
            Err(reason) => self
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
                            Err(_) => *error = Some(self.messages.get("server.invalid")),
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
                KeyCode::Char('o') => vec![Effect::OpenBrowser(url.clone())],
                KeyCode::Char('c') => {
                    *notice = Some(self.messages.get("approval.copied"));
                    vec![Effect::Copy(url.clone())]
                }
                KeyCode::Char('p') => {
                    self.show_paste();
                    vec![Effect::StopListening]
                }
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
            Screen::Signed { .. } => match key.code {
                KeyCode::Char('q') => vec![Effect::Quit],
                _ => Vec::new(),
            },
        }
    }

    /// Draws the current screen.
    pub fn render(&self, frame: &mut Frame) {
        background(frame, &self.palette);
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
            Screen::Signed { server, email } => {
                let inside = self.card(frame, "signed");
                let m = &self.messages;
                let lines = vec![
                    Line::from(m.fill("signed.body", &[("server", server), ("email", email)])),
                    Line::default(),
                    Line::from(m.get("signed.next")).fg(self.palette.muted),
                ];
                frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: true }), inside);
            }
        }
    }

    /// The card of a sign-in screen, with the title and keys of its messages; returns its inside.
    fn card(&self, frame: &mut Frame, screen: &str) -> ratatui::layout::Rect {
        let m = &self.messages;
        let (name, tagline) = (m.get("app.name"), m.get("app.tagline"));
        card(
            frame,
            &self.palette,
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
        let (m, palette) = (&self.messages, &self.palette);
        let inside = self.card(frame, "server");
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
                selected_line(line, palette)
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
        let (m, palette) = (&self.messages, &self.palette);
        let inside = self.card(frame, "approval");
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
            Line::from(url.to_owned())
                .fg(palette.info)
                .add_modifier(Modifier::UNDERLINED),
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
        let (m, palette) = (&self.messages, &self.palette);
        let inside = self.card(frame, "paste");
        let [text, code, note] = Layout::vertical([
            Constraint::Length(6),
            Constraint::Length(3),
            Constraint::Fill(1),
        ])
        .areas(inside);
        let lines = vec![
            Line::from(m.get("paste.intro")),
            Line::from(url.to_owned())
                .fg(palette.info)
                .add_modifier(Modifier::UNDERLINED),
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

/// A line marked as the selected one: on the raised background and bold, or reversed when there is no color.
fn selected_line(line: Line<'static>, palette: &Palette) -> Line<'static> {
    let style = if palette.plain {
        Style::new().add_modifier(Modifier::REVERSED)
    } else {
        Style::new()
            .bg(palette.raised)
            .fg(palette.accent_text)
            .add_modifier(Modifier::BOLD)
    };
    line.style(style)
}
