//! Transferly from the terminal (ADR-0029): the command line, the interface and the client of the API of a self-hosted Transferly.

use std::process::ExitCode;

use clap::Parser;

/// The command line of `transferly-tui`.
#[derive(Debug, Parser)]
#[command(
    name = "transferly-tui",
    version,
    about = "Transferly from the terminal"
)]
pub struct Cli {}

/// Runs the command line and returns the exit status.
pub fn run(_cli: &Cli) -> ExitCode {
    ExitCode::SUCCESS
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The command line parses with no arguments and the client exits successfully.
    #[test]
    fn runs_without_arguments() {
        let cli = Cli::try_parse_from(["transferly-tui"]).expect("no arguments");
        assert_eq!(run(&cli), ExitCode::SUCCESS);
    }
}
