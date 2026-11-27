//! Where the bearer token of each server is kept (ADR-0029): the credential store of the system when there is one (Secret Service on
//! Linux and BSD, the Keychain on macOS, the Credential Manager on Windows), otherwise a file only the user can read.

use std::collections::BTreeMap;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use keyring_core::{CredentialStore, Entry};

/// Service name of the tokens in the credential store of the system.
pub const SERVICE: &str = "transferly-tui";

/// Name of the file of tokens, used when the system has no credential store.
pub const FILE_NAME: &str = "tokens.toml";

/// Why a token could not be read, written or deleted.
#[derive(Debug, thiserror::Error)]
pub enum SecretError {
    /// The credential store of the system failed.
    #[error("the credential store of the system: {0}")]
    Keyring(#[from] keyring_core::Error),
    /// The file of tokens could not be read or written.
    #[error("the file of tokens: {0}")]
    Io(#[from] io::Error),
    /// The file of tokens is not valid.
    #[error("the file of tokens is not valid: {0}")]
    Invalid(#[from] toml::de::Error),
}

/// Keeps one token per server.
pub trait TokenStore: Send + Sync {
    /// The token of a server, if any.
    ///
    /// # Errors
    ///
    /// The store cannot be read.
    fn get(&self, server: &str) -> Result<Option<String>, SecretError>;
    /// Keeps the token of a server, replacing the one it had.
    ///
    /// # Errors
    ///
    /// The store cannot be written.
    fn set(&self, server: &str, token: &str) -> Result<(), SecretError>;
    /// Forgets the token of a server; forgetting none is not an error.
    ///
    /// # Errors
    ///
    /// The store cannot be written.
    fn delete(&self, server: &str) -> Result<(), SecretError>;
    /// Where the tokens are kept, for the status of the client.
    fn describe(&self) -> String;
}

/// Tokens in the credential store of the system, one entry per server.
pub struct Keyring {
    store: Arc<CredentialStore>,
}

impl Keyring {
    /// The entry of a server in the store.
    fn entry(&self, server: &str) -> keyring_core::Result<Entry> {
        Entry::new_in_store(&self.store, SERVICE, server)
    }
}

impl TokenStore for Keyring {
    fn get(&self, server: &str) -> Result<Option<String>, SecretError> {
        match self.entry(server).and_then(|entry| entry.get_password()) {
            Ok(token) => Ok(Some(token)),
            Err(keyring_core::Error::NoEntry) => Ok(None),
            Err(error) => Err(error.into()),
        }
    }

    fn set(&self, server: &str, token: &str) -> Result<(), SecretError> {
        Ok(self
            .entry(server)
            .and_then(|entry| entry.set_password(token))?)
    }

    fn delete(&self, server: &str) -> Result<(), SecretError> {
        match self
            .entry(server)
            .and_then(|entry| entry.delete_credential())
        {
            Ok(()) | Err(keyring_core::Error::NoEntry) => Ok(()),
            Err(error) => Err(error.into()),
        }
    }

    fn describe(&self) -> String {
        "the credential store of the system".to_owned()
    }
}

/// Tokens in a TOML file that only the user can read, for systems without a credential store, such as a server reached over SSH.
pub struct TokenFile {
    path: PathBuf,
}

impl TokenFile {
    /// The tokens in the file, by server.
    fn read(&self) -> Result<BTreeMap<String, String>, SecretError> {
        match std::fs::read_to_string(&self.path) {
            Ok(text) => Ok(toml::from_str(&text)?),
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(BTreeMap::new()),
            Err(error) => Err(error.into()),
        }
    }

    /// Writes the tokens to a temporary file readable by the user only, then renames it over the file.
    fn write(&self, tokens: &BTreeMap<String, String>) -> Result<(), SecretError> {
        if let Some(dir) = self.path.parent() {
            std::fs::create_dir_all(dir)?;
        }
        let temporary = self.path.with_extension("toml.tmp");
        let text = toml::to_string(tokens).expect("tokens are plain text");
        write_private(&temporary, &text).and_then(|()| std::fs::rename(&temporary, &self.path))?;
        Ok(())
    }
}

impl TokenStore for TokenFile {
    fn get(&self, server: &str) -> Result<Option<String>, SecretError> {
        Ok(self.read()?.remove(server))
    }

    fn set(&self, server: &str, token: &str) -> Result<(), SecretError> {
        let mut tokens = self.read()?;
        tokens.insert(server.to_owned(), token.to_owned());
        self.write(&tokens)
    }

    fn delete(&self, server: &str) -> Result<(), SecretError> {
        let mut tokens = self.read()?;
        if tokens.remove(server).is_some() {
            self.write(&tokens)?;
        }
        Ok(())
    }

    fn describe(&self) -> String {
        format!("{} (readable by you only)", self.path.display())
    }
}

/// Creates a file only the user can read and write, with the given text.
#[cfg(unix)]
fn write_private(path: &Path, text: &str) -> io::Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(path)?;
    file.write_all(text.as_bytes())
}

/// Creates a file with the given text; the profile folder of the user on Windows is private to the user already.
#[cfg(not(unix))]
fn write_private(path: &Path, text: &str) -> io::Result<()> {
    std::fs::write(path, text)
}

/// The store of tokens: the credential store of the platform when it opened, otherwise the file in the configuration folder.
#[must_use]
pub fn open(
    config_dir: &Path,
    platform: keyring_core::Result<Arc<CredentialStore>>,
) -> Box<dyn TokenStore> {
    match platform {
        Ok(store) => Box::new(Keyring { store }),
        Err(_) => Box::new(TokenFile {
            path: config_dir.join(FILE_NAME),
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Runs the same checks on any store: nothing at first, a token kept and replaced, deleted twice without error.
    fn round_trip(store: &dyn TokenStore) {
        assert_eq!(store.get("https://a.example.com").expect("get"), None);
        store.set("https://a.example.com", "first").expect("set");
        store
            .set("https://a.example.com", "second")
            .expect("replace");
        store
            .set("https://b.example.com", "other")
            .expect("set another");
        assert_eq!(
            store.get("https://a.example.com").expect("get").as_deref(),
            Some("second")
        );
        store.delete("https://a.example.com").expect("delete");
        store.delete("https://a.example.com").expect("delete again");
        assert_eq!(store.get("https://a.example.com").expect("get"), None);
        assert_eq!(
            store.get("https://b.example.com").expect("get").as_deref(),
            Some("other")
        );
    }

    /// The credential store keeps tokens by server and reports its failures.
    #[test]
    fn keeps_tokens_in_the_credential_store() {
        let dir = tempfile::tempdir().expect("temporary folder");
        let store = open(
            dir.path(),
            Ok(keyring_core::mock::Store::new().expect("mock store")),
        );
        round_trip(store.as_ref());
        assert_eq!(store.describe(), "the credential store of the system");
        assert!(!dir.path().join(FILE_NAME).exists());

        let mock = keyring_core::mock::Store::new().expect("mock store");
        let failing = Keyring {
            store: mock.clone(),
        };
        let entry = failing.entry("https://broken.example.com").expect("entry");
        entry.set_password("x").expect("set");
        let credential: &keyring_core::mock::Cred =
            entry.as_any().downcast_ref().expect("a mock credential");
        credential.set_error(keyring_core::Error::Invalid(
            "store".into(),
            "locked".into(),
        ));
        assert!(matches!(
            failing.get("https://broken.example.com"),
            Err(SecretError::Keyring(_))
        ));
        credential.set_error(keyring_core::Error::Invalid(
            "store".into(),
            "locked".into(),
        ));
        assert!(matches!(
            failing.delete("https://broken.example.com"),
            Err(SecretError::Keyring(_))
        ));
        credential.set_error(keyring_core::Error::Invalid(
            "store".into(),
            "locked".into(),
        ));
        assert!(matches!(
            failing.set("https://broken.example.com", "y"),
            Err(SecretError::Keyring(_))
        ));
    }

    /// Without a credential store the tokens go to a file only the user can read, written whole every time.
    #[test]
    fn keeps_tokens_in_a_private_file() {
        let dir = tempfile::tempdir().expect("temporary folder");
        let store = open(
            &dir.path().join("config"),
            Err(keyring_core::Error::NoDefaultStore),
        );
        round_trip(store.as_ref());
        let path = dir.path().join("config").join(FILE_NAME);
        assert!(store.describe().contains(FILE_NAME));
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(
                std::fs::metadata(&path).expect("file").permissions().mode() & 0o777,
                0o600
            );
        }
        store
            .delete("https://never.example.com")
            .expect("deleting a token that is not there");

        std::fs::write(&path, "not = [valid").expect("write");
        assert!(matches!(
            store.get("https://a.example.com"),
            Err(SecretError::Invalid(_))
        ));
        assert!(matches!(
            store.delete("https://a.example.com"),
            Err(SecretError::Invalid(_))
        ));
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let locked = dir.path().join("locked");
            std::fs::create_dir(&locked).expect("folder");
            std::fs::set_permissions(&locked, std::fs::Permissions::from_mode(0o500))
                .expect("read-only");
            let unwritable = TokenFile {
                path: locked.join("config").join(FILE_NAME),
            };
            assert!(matches!(
                unwritable.set("https://a.example.com", "t"),
                Err(SecretError::Io(_))
            ));
        }
        let folder = TokenFile {
            path: dir.path().to_path_buf(),
        };
        assert!(matches!(
            folder.get("https://a.example.com"),
            Err(SecretError::Io(_))
        ));
        let nowhere = TokenFile {
            path: PathBuf::new(),
        };
        assert!(matches!(
            nowhere.set("https://a.example.com", "t"),
            Err(SecretError::Io(_))
        ));
        let blocked = TokenFile {
            path: path.join("x"),
        };
        assert!(matches!(
            blocked.set("https://a.example.com", "t"),
            Err(SecretError::Io(_))
        ));
    }
}
