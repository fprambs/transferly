//! The configuration of this computer: the servers signed in to and the settings that do not belong to the account
//! (docs/design/terminal-client.md, Settings). It lives in `config.toml` in the configuration folder of the user.

use std::io;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use url::Url;

/// Name of the configuration file in the configuration folder.
pub const FILE_NAME: &str = "config.toml";

/// Why a server address or the configuration file was refused.
#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    /// The address is not an http or https address of a server.
    #[error("{0} is not the address of a server, such as https://files.example.com")]
    InvalidServer(String),
    /// The file could not be read or written.
    #[error("the configuration file: {0}")]
    Io(#[from] io::Error),
    /// The file is not valid TOML of the expected shape.
    #[error("the configuration file is not valid: {0}")]
    Invalid(#[from] toml::de::Error),
}

/// What the configuration file holds.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Config {
    /// The server the client opens, by its origin.
    pub current: Option<String>,
    /// The servers signed in to before, most recently used first.
    pub profiles: Vec<Profile>,
    /// The settings of this computer.
    pub settings: Settings,
}

/// A server signed in to from this computer.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Profile {
    /// Origin of the server, such as `https://files.example.com`.
    pub server: String,
    /// Email of the account signed in with, once known.
    pub account: Option<String>,
}

/// The settings of this computer; those of the account (language, theme, time zone) live in the account.
#[allow(
    clippy::struct_excessive_bools,
    reason = "independent settings of this computer, each a yes or no"
)]
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Settings {
    /// Whether the client wears the colors of the theme or the terminal's own palette.
    pub colors: Colors,
    /// What Enter does on a file.
    pub enter_on_file: EnterOnFile,
    /// Whether hidden files are shown.
    pub hidden_files: bool,
    /// Whether quitting with transfers running asks first.
    pub confirm_quit: bool,
    /// Whether the client looks for a new release once a day.
    pub check_updates: bool,
    /// Whether the terminal captures mouse input.
    pub mouse: bool,
}

impl Default for Settings {
    fn default() -> Self {
        Self {
            colors: Colors::Theme,
            enter_on_file: EnterOnFile::Copy,
            hidden_files: false,
            confirm_quit: true,
            check_updates: true,
            mouse: true,
        }
    }
}

/// Where the colors come from.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Colors {
    /// The theme of the account.
    Theme,
    /// The 16 colors of the terminal.
    Terminal,
}

/// What Enter does on a file.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum EnterOnFile {
    /// Copy it to the other panel, after a confirmation.
    Copy,
    /// Show its details.
    Details,
}

impl Config {
    /// Reads the configuration from a file; a file that does not exist is the default configuration.
    ///
    /// # Errors
    ///
    /// The file cannot be read, or it is not valid.
    pub fn load(path: &Path) -> Result<Self, ConfigError> {
        match std::fs::read_to_string(path) {
            Ok(text) => Ok(toml::from_str(&text)?),
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(Self::default()),
            Err(error) => Err(error.into()),
        }
    }

    /// Writes the configuration, creating its folder; it writes a temporary file and renames it, so a failure never leaves half a file.
    ///
    /// # Errors
    ///
    /// The folder or the file cannot be written.
    ///
    /// # Panics
    ///
    /// Never in practice: the configuration is plain data, which always serializes.
    pub fn save(&self, path: &Path) -> Result<(), ConfigError> {
        if let Some(dir) = path.parent() {
            std::fs::create_dir_all(dir)?;
        }
        let text = toml::to_string_pretty(self).expect("the configuration is plain data");
        let temporary = path.with_extension("toml.tmp");
        std::fs::write(&temporary, text).and_then(|()| std::fs::rename(&temporary, path))?;
        Ok(())
    }

    /// The profile of a server, if this computer signed in to it.
    #[must_use]
    pub fn profile(&self, server: &str) -> Option<&Profile> {
        self.profiles
            .iter()
            .find(|profile| profile.server == server)
    }

    /// Records a sign-in: the profile of the server moves first, with its account, and becomes the current one.
    pub fn signed_in(&mut self, server: &str, account: &str) {
        self.profiles.retain(|profile| profile.server != server);
        self.profiles.insert(
            0,
            Profile {
                server: server.to_owned(),
                account: Some(account.to_owned()),
            },
        );
        self.current = Some(server.to_owned());
    }
}

/// The configuration folder of the user: `~/.config/transferly` on Linux, the application data folders on macOS and Windows.
#[must_use]
pub fn default_dir() -> Option<PathBuf> {
    directories::ProjectDirs::from("", "", "transferly").map(|dirs| dirs.config_dir().to_path_buf())
}

/// The origin of a server from what a person types: `files.example.com` becomes `https://files.example.com`; a path, a query or
/// credentials are refused, since Transferly answers at the root of its origin.
///
/// # Errors
///
/// The text is not the address of a server.
pub fn server_origin(input: &str) -> Result<Url, ConfigError> {
    let trimmed = input.trim();
    let candidate = if trimmed.contains("://") {
        trimmed.to_owned()
    } else {
        format!("https://{trimmed}")
    };
    let invalid = || ConfigError::InvalidServer(trimmed.to_owned());
    let url = Url::parse(&candidate).map_err(|_| invalid())?;
    let only_origin = url.path() == "/"
        && url.query().is_none()
        && url.fragment().is_none()
        && url.username().is_empty()
        && url.password().is_none();
    if !matches!(url.scheme(), "http" | "https") || url.host_str().is_none() || !only_origin {
        return Err(invalid());
    }
    Ok(url)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A missing file is the default configuration; a saved one reads back the same, and a broken one is refused.
    #[test]
    fn loads_and_saves() {
        let dir = tempfile::tempdir().expect("temporary folder");
        let path = dir.path().join("nested").join(FILE_NAME);
        assert_eq!(
            Config::load(&path).expect("missing file"),
            Config::default()
        );

        let mut config = Config::default();
        config.signed_in("https://files.example.com", "ana@example.com");
        config.settings.enter_on_file = EnterOnFile::Details;
        config.settings.colors = Colors::Terminal;
        config.settings.mouse = false;
        config.save(&path).expect("save");
        assert_eq!(Config::load(&path).expect("load"), config);
        assert!(!path.with_extension("toml.tmp").exists());

        std::fs::write(&path, "[settings]\nhidden_files = true").expect("old config");
        assert!(Config::load(&path).expect("old settings").settings.mouse);
        std::fs::write(&path, "profiles = 3").expect("write");
        assert!(matches!(Config::load(&path), Err(ConfigError::Invalid(_))));
        assert!(matches!(Config::load(dir.path()), Err(ConfigError::Io(_))));
        assert!(matches!(
            Config::default().save(Path::new("")),
            Err(ConfigError::Io(_))
        ));
        assert!(matches!(
            config.save(&dir.path().join("nested").join(FILE_NAME).join("x")),
            Err(ConfigError::Io(_))
        ));
    }

    /// Signing in moves the server first, keeps one profile per server and makes it current.
    #[test]
    fn records_sign_ins() {
        let mut config = Config::default();
        config.signed_in("https://a.example.com", "ana@example.com");
        config.signed_in("https://b.example.com", "bob@example.com");
        config.signed_in("https://a.example.com", "ana@example.com");
        assert_eq!(config.current.as_deref(), Some("https://a.example.com"));
        assert_eq!(
            config
                .profiles
                .iter()
                .map(|p| p.server.as_str())
                .collect::<Vec<_>>(),
            ["https://a.example.com", "https://b.example.com"]
        );
        assert_eq!(
            config
                .profile("https://b.example.com")
                .and_then(|p| p.account.as_deref()),
            Some("bob@example.com")
        );
        assert!(config.profile("https://c.example.com").is_none());
        assert_eq!(Settings::default(), config.settings);
        assert!(default_dir().is_some());
    }

    /// Server addresses: a bare host becomes https, a port and a trailing slash are kept as the origin, anything more is refused.
    #[test]
    fn reads_server_addresses() {
        assert_eq!(
            server_origin(" files.example.com ").expect("host").as_str(),
            "https://files.example.com/"
        );
        assert_eq!(
            server_origin("http://localhost:8080/")
                .expect("port")
                .as_str(),
            "http://localhost:8080/"
        );
        for bad in [
            "",
            "ftp://files.example.com",
            "https://files.example.com/app",
            "https://u:p@files.example.com",
            "https://x.example.com/?a=1",
            "https://x.example.com/#a",
            "https://",
        ] {
            assert!(
                matches!(server_origin(bad), Err(ConfigError::InvalidServer(_))),
                "{bad}"
            );
        }
    }
}
