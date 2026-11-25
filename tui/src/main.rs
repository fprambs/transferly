//! Entry point of `transferly-tui`: it reads the command line and hands it to the library.

use clap::Parser;

/// Runs the terminal client and exits with its status.
fn main() -> std::process::ExitCode {
    transferly_tui::run(&transferly_tui::Cli::parse())
}
