//! The interface of `transferly-tui`: its state and screens ([`app`]), the loop that runs it ([`runtime`]), its colors ([`theme`]),
//! text fields ([`input`]) and drawing pieces ([`widgets`]). Only `platform.rs` touches the real terminal.

pub mod app;
pub mod home;
pub mod input;
pub mod look;
pub mod runtime;
pub mod theme;
pub mod widgets;

use crossterm::event::Event as TerminalEvent;

use self::app::{Event, Kept, Setup};
use self::theme::ColorMode;
use crate::config::{Colors, Config};
use crate::i18n::Language;
use crate::login;
use crate::secrets::TokenStore;

/// The theme worn before the account says which, the default of the web.
pub const DEFAULT_THEME: &str = "transferly-dark";

/// The event of the interface for an event of the terminal: keys and pasted text; a resize only redraws.
#[must_use]
pub fn from_terminal(event: TerminalEvent) -> Option<Event> {
    match event {
        TerminalEvent::Key(key) => Some(Event::Key(key)),
        TerminalEvent::Paste(text) => Some(Event::Paste(text)),
        TerminalEvent::Resize(..) => Some(Event::Tick),
        _ => None,
    }
}

/// What the interface starts with on this computer: the language and colors of the system, the profiles, and the server already signed
/// in to when its token is kept.
#[must_use]
pub fn setup(
    config: &Config,
    tokens: &dyn TokenStore,
    env: impl Fn(&str) -> Option<String>,
    host: String,
) -> Setup {
    let signed_in = config.current.as_ref().and_then(|server| {
        let token = tokens.get(server).ok().flatten()?;
        let email = config
            .profile(server)
            .and_then(|profile| profile.account.clone())
            .unwrap_or_default();
        Some(Kept {
            server: server.clone(),
            email,
            token,
        })
    });
    Setup {
        language: Language::from_env(&env),
        mode: ColorMode::detect(&env, Colors::Theme),
        theme: DEFAULT_THEME.to_owned(),
        settings: config.settings.clone(),
        profiles: config.profiles.clone(),
        host,
        browser_possible: login::can_open_browser(&env),
        signed_in,
    }
}

#[cfg(test)]
mod tests;

#[cfg(test)]
mod home_tests;
