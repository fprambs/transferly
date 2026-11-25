//! The screens of `transferly-tui` itself, drawn by its interface (`transferly_tui::ui::app::App`) after the events that lead to them,
//! not mockups: the steps of signing in, for the screenshots of make tui-mockups.

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::Terminal;
use ratatui::backend::TestBackend;
use ratatui::buffer::Buffer;
use ratatui::style::Color;
use transferly_tui::config::Profile;
use transferly_tui::i18n::Language;
use transferly_tui::login::Answer;
use transferly_tui::ui::app::{App, Event, Setup};
use transferly_tui::ui::theme::ColorMode;

use crate::html;

/// A screen of the application: its file name, what it shows, and the buffer it draws.
pub struct Shot {
    /// Name of the page and the image.
    pub slug: &'static str,
    /// What it shows.
    pub about: &'static str,
    /// What the interface drew.
    pub buffer: Buffer,
    /// The colors of cells without their own.
    pub defaults: html::Defaults,
}

/// A pressed key.
fn key(code: KeyCode) -> Event {
    Event::Key(KeyEvent::new(code, KeyModifiers::NONE))
}

/// The interface of a computer with two servers used before, in a language, with or without a browser.
fn app(language: Language, browser: bool) -> App {
    let profile = |server: &str, account: &str| Profile { server: server.to_owned(), account: Some(account.to_owned()) };
    App::new(Setup {
        language,
        mode: ColorMode::TrueColor,
        theme: "transferly-dark".to_owned(),
        profiles: vec![profile("https://files.acme.com", "ana@acme.com"), profile("https://transferly.lan", "admin@home.lan")],
        host: "laptop".to_owned(),
        browser_possible: browser,
        signed_in: None,
    })
}

/// Draws the interface as it stands.
fn shot(slug: &'static str, about: &'static str, app: &App) -> Shot {
    let mut terminal = Terminal::new(TestBackend::new(100, 28)).expect("backend");
    terminal.draw(|frame| app.render(frame)).expect("draw");
    let palette = app.palette();
    let defaults = html::Defaults { fg: palette.text, bg: palette.bg, muted: if palette.muted == Color::Reset { Color::Reset } else { palette.muted } };
    Shot { slug, about, buffer: terminal.backend().buffer().clone(), defaults }
}

/// The steps of signing in: the server, the approval in the browser, the code pasted without a browser, and signed in.
pub fn shots() -> Vec<Shot> {
    let mut shots = Vec::new();
    let mut english = app(Language::English, true);
    english.handle(key(KeyCode::Down));
    shots.push(shot("app-server", "The first screen: the address of the server, with the profiles used before; ↓ chose one.", &english));

    english.handle(key(KeyCode::Enter));
    english.handle(Event::Probed(Ok(())));
    english.handle(Event::Listening(Ok("http://127.0.0.1:49152/callback".to_owned())));
    english.handle(Event::BrowserOpened(true));
    for _ in 0..3 {
        english.handle(Event::Tick);
    }
    shots.push(shot("app-approval", "The browser opened the approval page; the terminal waits on 127.0.0.1.", &english));

    let mut spanish = app(Language::Spanish, false);
    spanish.handle(key(KeyCode::Enter));
    spanish.handle(Event::Probed(Ok(())));
    spanish.handle(Event::Paste("rsA7h63aXQetjzVD7pKsfA1yzQggA868KGK49ScbGr8".to_owned()));
    shots.push(shot("app-paste", "Over SSH, without a browser: the code shown by the approval page is pasted, in Spanish.", &spanish));

    english.handle(Event::Answered(Ok(Answer::Approved("code".to_owned()))));
    english.handle(Event::SignedIn { server: "https://files.acme.com".to_owned(), email: "ana@acme.com".to_owned(), locale: "es".to_owned(), theme: "nord".to_owned() });
    shots.push(shot("app-signed-in", "Signed in: the interface takes the language (Spanish) and the theme (Nord) of the account.", &english));
    shots
}
