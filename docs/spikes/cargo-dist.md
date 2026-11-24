# Spike: One-Command Install with cargo-dist

Program: `tui/spikes/distinstall`, the smallest `transferly-tui`. For [ADR-0029](../adr/0029-terminal-client-and-device-login.md).
Run on 2026-11-24 in Docker (`rust:1-bookworm`, cargo-dist 0.34.0 installed in a cache volume); nothing was installed on the host.

## Question

Can the terminal client be installed with one command on Linux, macOS and Windows, from GitHub Releases, without a second release
process next to the server's?

## Results

| Check | Result |
|---|---|
| `dist init` with shell and PowerShell installers, GitHub CI and hosting | writes `dist-workspace.toml`, `[profile.dist]` and `.github/workflows/release.yml` |
| Crate in `tui/` with `dist-workspace.toml` at the root (`members = ["cargo:tui"]`) and Go next to it | works; the workflow goes to the root `.github/workflows` |
| `dist plan --tag v0.0.1` | one archive and checksum per target, `transferly-tui-installer.sh`, `transferly-tui-installer.ps1`, `sha256.sum`, source archive |
| Local build of `x86_64-unknown-linux-musl` (`dist build --artifacts=all`) | static binary of 526 KB (the spike prints its version), archive of 194 KB |
| `curl -LsSf …/transferly-tui-installer.sh \| sh` on clean `debian:bookworm-slim` and `alpine:3.22` | installs and runs; the release was served from a local server with GitHub's path layout through `TRANSFERLY_TUI_INSTALLER_GITHUB_BASE_URL` |
| `install-path = ["$XDG_BIN_HOME/", "~/.local/bin"]` | installs to `~/.local/bin` instead of the default `~/.cargo/bin`, which means nothing to people who do not use Rust |

## Findings

- `dist init` proposes seven targets, with Linux `gnu` and `musl`. The static `musl` builds run on any distribution, glibc or not,
  so Linux ships only `x86_64-unknown-linux-musl` and `aarch64-unknown-linux-musl`: five targets in all, as the ADR says.
- The generated workflow builds on `macos-15` (arm64), `macos-15-intel`, `windows-2022`, `ubuntu-22.04` and `ubuntu-22.04-arm`.
  The repository is public, so these runners cost nothing; the billing block noted in `docs/guides/releases.md` still stops them.
- The workflow runs on tags matching `**[0-9]+.[0-9]+.[0-9]+*`, so the server's `vX.Y.Z` tags trigger it, and creates the GitHub
  Release with the artifacts. The current `ci.yml` publishes images to GHCR and creates no GitHub Release, so they do not collide.
  The tag must equal the version of `tui/Cargo.toml`; `scripts/release.sh` sets it as it sets `frontend/package.json`.
- On pull requests the workflow only runs `dist plan`, which checks the configuration without building.
- Not tried here, since it needs a Mac and Windows: files that `curl` downloads get no quarantine attribute, so Gatekeeper should
  not stop the unsigned binary; Windows still needs a check on a real machine. T5 verifies both before the first release; Homebrew,
  winget or signed packages can come later.
- The updater `install-updater` installs a second binary; `transferly-tui update` is better built in with the `axoupdater` library,
  which reads the same release metadata.

## Decision

1. Release the terminal client with cargo-dist from `dist-workspace.toml` at the root, five targets, shell and PowerShell installers,
   installed to `$XDG_BIN_HOME` or `~/.local/bin`, with the same tag as the server.
2. Install command: `curl -LsSf https://github.com/fprambs/transferly/releases/latest/download/transferly-tui-installer.sh | sh`;
   on Windows `irm https://github.com/fprambs/transferly/releases/latest/download/transferly-tui-installer.ps1 | iex`.
3. `transferly-tui update` uses `axoupdater` as a library.
4. The generated `release.yml` enters the repository in T5, once the client exists, so that tags before it do not publish an empty
   release.
