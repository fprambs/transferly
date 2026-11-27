//! Transferly from the terminal (ADR-0029): the command line, the interface and the client of the API of a self-hosted Transferly.

pub mod api;
pub mod commands;
pub mod config;
pub mod i18n;
pub mod login;
pub mod platform;
pub mod secrets;
pub mod ui;

use std::path::PathBuf;
use std::process::ExitCode;

use clap::{Parser, Subcommand};

/// The command line of `transferly-tui`.
#[derive(Debug, Parser)]
#[command(
    name = "transferly-tui",
    version,
    about = "Transferly from the terminal"
)]
pub struct Cli {
    /// What to do; without a command the interface opens.
    #[command(subcommand)]
    pub command: Option<Command>,
}

/// The commands outside the interface.
#[derive(Debug, Subcommand)]
pub enum Command {
    /// Sign this terminal in to a server, approving in the browser.
    Login {
        /// Address of the server, such as files.example.com.
        server: String,
        /// Paste the code from the approval page instead of receiving it on 127.0.0.1, for machines without a browser.
        #[arg(long)]
        paste: bool,
    },
    /// Say which server and account this terminal is signed in to.
    Status,
    /// End the session of this terminal and forget its token.
    Logout,
    /// Show the privacy, cookies and terms notice.
    Privacy,
}

/// Runs the command line and returns the exit status.
pub fn run(cli: &Cli) -> ExitCode {
    run_in(cli, config::default_dir(), platform::interface)
}

/// Runs the command line with the given configuration folder, or none when the system has no folder for it, opening the interface
/// with the given function when there is no command.
fn run_in(
    cli: &Cli,
    dir: Option<PathBuf>,
    interface: fn(&std::path::Path) -> std::io::Result<()>,
) -> ExitCode {
    let Some(dir) = dir else {
        eprintln!("transferly-tui: this system has no configuration folder for the user.");
        return ExitCode::FAILURE;
    };
    let Some(command) = &cli.command else {
        return match interface(&dir) {
            Ok(()) => ExitCode::SUCCESS,
            Err(error) => {
                eprintln!("transferly-tui: the terminal could not be used: {error}");
                ExitCode::FAILURE
            }
        };
    };
    let runtime = tokio::runtime::Runtime::new().expect("the async runtime");
    let mut stdout = std::io::stdout();
    let mut stdin = std::io::stdin().lock();
    let mut ctx = commands::Context {
        config_path: dir.join(config::FILE_NAME),
        tokens: secrets::open(&dir, platform::credential_store()),
        out: &mut stdout,
        input: &mut stdin,
        open_browser: &platform::open_browser,
        browser_possible: login::can_open_browser(|name| std::env::var(name).ok()),
        host: login::host_name(),
        wait: commands::BROWSER_WAIT,
        listen_on: login::LOOPBACK.to_owned(),
    };
    runtime.block_on(async {
        match command {
            Command::Login { server, paste } => commands::login(&mut ctx, server, *paste).await,
            Command::Status => commands::status(&mut ctx).await,
            Command::Logout => commands::logout(&mut ctx).await,
            Command::Privacy => {
                let language = i18n::Language::from_env(|name| std::env::var(name).ok());
                println!("{}", i18n::Messages::new(language).get("privacy.summary"));
                ExitCode::SUCCESS
            }
        }
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Stands in for a terminal that cannot be set up.
    fn no_terminal(_: &std::path::Path) -> std::io::Result<()> {
        Err(std::io::Error::other("not a terminal"))
    }

    /// Stands in for an interface that ran and was closed.
    #[allow(
        clippy::unnecessary_wraps,
        reason = "it stands in for platform::interface, which returns a Result"
    )]
    fn closed_interface(_: &std::path::Path) -> std::io::Result<()> {
        Ok(())
    }

    /// Without a command the interface opens; a terminal that cannot be used, or no configuration folder, fails.
    #[test]
    fn runs_without_a_command() {
        let cli = Cli::try_parse_from(["transferly-tui"]).expect("no arguments");
        let dir = tempfile::tempdir().expect("folder");
        assert_eq!(
            run_in(&cli, Some(dir.path().to_path_buf()), closed_interface),
            ExitCode::SUCCESS
        );
        assert_eq!(
            run_in(&cli, Some(dir.path().to_path_buf()), no_terminal),
            ExitCode::FAILURE
        );
        assert_eq!(run_in(&cli, None, no_terminal), ExitCode::FAILURE);
        let status = Cli::try_parse_from(["transferly-tui", "--version"]);
        assert!(status.is_err(), "--version prints and exits through clap");
        let _ = run(&Cli::try_parse_from(["transferly-tui", "status"]).expect("status"));
    }

    /// The privacy notice is available without a saved session.
    #[test]
    fn prints_privacy_without_a_session() {
        let cli = Cli::try_parse_from(["transferly-tui", "privacy"]).expect("privacy command");
        let dir = tempfile::tempdir().expect("folder");
        assert_eq!(
            run_in(&cli, Some(dir.path().to_path_buf()), no_terminal),
            ExitCode::SUCCESS
        );
    }

    /// The commands parse and run against the configuration folder; with nothing signed in, status and logout say so.
    #[test]
    fn runs_the_commands() {
        let dir = tempfile::tempdir().expect("folder");
        for args in [
            &["transferly-tui", "status"][..],
            &["transferly-tui", "logout"],
        ] {
            let cli = Cli::try_parse_from(args).expect("command");
            assert_eq!(
                run_in(&cli, Some(dir.path().to_path_buf()), no_terminal),
                ExitCode::FAILURE
            );
        }
        let cli = Cli::try_parse_from(["transferly-tui", "login", "not a url", "--paste"])
            .expect("login");
        assert!(matches!(
            &cli.command,
            Some(Command::Login { paste: true, .. })
        ));
        assert_eq!(
            run_in(&cli, Some(dir.path().to_path_buf()), no_terminal),
            ExitCode::from(2)
        );
    }
}
