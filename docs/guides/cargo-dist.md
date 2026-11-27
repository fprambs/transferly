# cargo-dist

Sources: [dist book](https://axodotdev.github.io/cargo-dist/book/) (config reference, shell and PowerShell installers, updaters,
workspaces, customizing CI, GitHub attestations), [releases](https://github.com/axodotdev/cargo-dist/releases) and the spike
([report](../spikes/cargo-dist.md)). Versions: dist 0.34.0, axoupdater 0.10.3 (checked 2026-11-24). Releases `transferly-tui`.

## Rules

- Configuration lives in `dist-workspace.toml` at the root with `members = ["cargo:tui"]`; `cargo-dist-version` is pinned there and
  in the tools image, and both move together.
- **Targets:** `aarch64-apple-darwin`, `x86_64-apple-darwin`, `x86_64-pc-windows-msvc`, `x86_64-unknown-linux-musl`,
  `aarch64-unknown-linux-musl`. Linux ships only static musl builds.
- **Installers:** `shell` and `powershell`; `install-path = ["$XDG_BIN_HOME/", "~/.local/bin"]`. The shell installer adds the folder
  to `PATH` only through `~/.profile`, and no installer can change the shell that ran it, so the README tells people to open a new
  terminal or run the `source` line it prints; zsh users may need it in `~/.zshrc`.
- **Tags:** one unified tag for the server and the client, `vX.Y.Z`, so `tui/Cargo.toml` has the version of the release;
  `scripts/release.sh` sets it. A prerelease tag (`v1.2.0-rc.1`) becomes a GitHub prerelease, not "latest". A tag whose version no
  package has fails the release.
- **The workflow only runs for tags pushed after it is in the repository.** It enters in T5 with the first client; no release tag is
  pushed in between.
- **dist owns `release.yml`.** It is regenerated with `dist generate` and never edited by hand; CI fails when it is out of date.
  Changes go through the configuration: `github-build-setup` for build steps, `publish-jobs` and friends for extra jobs,
  `github-action-commits` to pin actions. `allow-dirty = ["ci"]` is not used.
- **Checksums:** one `.sha256` per archive and `sha256.sum`. Turn on `github-attestations = true` (public repository) so people can
  check a download with `gh attestation verify <file> --repo fprambs/transferly`. The pages read do not say whether the installers
  verify checksums before installing; do not claim it.
- **Updates:** `transferly-tui update` embeds `axoupdater` as a library; `install-updater` (a second binary) is not used. Its CI tests
  set `AXOUPDATER_GITHUB_TOKEN` to avoid the anonymous rate limit.
- `source-tarball` stays on: the repository is public.
- The installers keep only the binary; anything else the client needs (themes, translations) is built into it.

## Tests

- Pull requests run `dist plan`, which checks the configuration without building.
- The installer is tested as in the spike: serve the artifacts of a local `dist build` with GitHub's path layout, point
  `TRANSFERLY_TUI_INSTALLER_GITHUB_BASE_URL` at it, and install in clean Debian and Alpine containers.
