//! The thin calls into the operating system that tests cannot make in a container: the credential store of the system and the browser.
//! Everything that decides what to do with them lives elsewhere and is tested; this file is left out of coverage, as `main.rs` is.

use std::sync::Arc;

use keyring_core::CredentialStore;

/// The credential store of this platform: the Secret Service on Linux and BSD, the Keychain on macOS, the Credential Manager on
/// Windows.
///
/// # Errors
///
/// The platform has none, or it is not running, such as the Secret Service without a desktop session.
pub fn credential_store() -> keyring_core::Result<Arc<CredentialStore>> {
    #[cfg(all(unix, not(target_os = "macos")))]
    let store: Arc<CredentialStore> = zbus_secret_service_keyring_store::Store::new()?;
    #[cfg(target_os = "macos")]
    let store: Arc<CredentialStore> = apple_native_keyring_store::keychain::Store::new()?;
    #[cfg(windows)]
    let store: Arc<CredentialStore> = windows_native_keyring_store::Store::new()?;
    Ok(store)
}

/// Opens an address in the default browser and tells whether it did.
#[must_use]
pub fn open_browser(url: &str) -> bool {
    webbrowser::open(url).is_ok()
}

/// Copies text to the clipboard through the terminal with the OSC 52 sequence, which works over SSH too; terminals without it
/// ignore it.
pub fn copy_to_clipboard(text: &str) {
    use base64::Engine;
    use std::io::Write;
    let sequence = format!(
        "\x1b]52;c;{}\x07",
        base64::engine::general_purpose::STANDARD.encode(text)
    );
    let mut stdout = std::io::stdout();
    let _ = stdout.write_all(sequence.as_bytes());
    let _ = stdout.flush();
}

/// Opens the interface on the real terminal: sets it up (raw mode, alternate screen, bracketed paste, restored on panic), runs the
/// interface over the events of the terminal and restores it.
///
/// # Errors
///
/// The terminal cannot be set up or drawn on.
pub fn interface(dir: &std::path::Path) -> std::io::Result<()> {
    use futures::StreamExt;

    use crate::config::Config;
    use crate::ui::{self, app::App, runtime};

    let config_path = dir.join(crate::config::FILE_NAME);
    let config = Config::load(&config_path).unwrap_or_default();
    let tokens: Arc<dyn crate::secrets::TokenStore> =
        Arc::from(crate::secrets::open(dir, credential_store()));
    let mut app = App::new(ui::setup(
        &config,
        tokens.as_ref(),
        |name| std::env::var(name).ok(),
        crate::login::host_name(),
    ));
    let ports = runtime::Ports {
        config_path,
        tokens,
        open_browser: Arc::new(open_browser),
        copy: Arc::new(copy_to_clipboard),
        listen_on: crate::login::LOOPBACK.to_owned(),
        wait: crate::commands::BROWSER_WAIT,
        tick: std::time::Duration::from_millis(120),
    };
    let runtime = tokio::runtime::Runtime::new()?;
    let mut terminal = ratatui::try_init()?;
    crossterm::execute!(std::io::stdout(), crossterm::event::EnableBracketedPaste)?;
    let events = crossterm::event::EventStream::new()
        .filter_map(|event| async move { event.ok().and_then(ui::from_terminal) });
    let mut draw = |app: &App| terminal.draw(|frame| app.render(frame)).map(|_| ());
    let result = runtime.block_on(runtime::run(&mut app, &ports, Box::pin(events), &mut draw));
    let _ = crossterm::execute!(std::io::stdout(), crossterm::event::DisableBracketedPaste);
    ratatui::restore();
    result
}
