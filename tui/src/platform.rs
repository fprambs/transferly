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
