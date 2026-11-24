//! Spike: the smallest `transferly-tui`, to try cargo-dist builds and installers.

/// Prints the name and version, which is what the installer test checks.
fn main() {
    println!("transferly-tui {}", env!("CARGO_PKG_VERSION"));
}
