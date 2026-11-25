# Rust tooling

Sources: [Clippy](https://doc.rust-lang.org/clippy/) and its [configuration](https://doc.rust-lang.org/clippy/configuration.html),
[rustfmt](https://github.com/rust-lang/rustfmt), [cargo-llvm-cov](https://github.com/taiki-e/cargo-llvm-cov),
[cargo-nextest](https://nexte.st/), [cargo-deny](https://embarkstudios.github.io/cargo-deny/), [insta](https://insta.rs/docs/settings/),
[Rust 2024 edition](https://doc.rust-lang.org/edition-guide/rust-2024/index.html), [releases.rs](https://releases.rs/). Versions
(checked 2026-11-24): Rust 1.100.0, cargo-llvm-cov 0.9.2, cargo-nextest 0.9.152, cargo-deny 0.22.1, insta 1.49.0.

## Rules

- **Everything runs in Docker.** The tools image gets a pinned Rust toolchain with `clippy`, `rustfmt` and `llvm-tools-preview`, the
  musl target, and the tools above installed with `cargo install --locked` at pinned versions. Nothing is installed on the host.
- **Edition 2024**, stable since Rust 1.85. `rust-version` in `tui/Cargo.toml` is the toolchain of the image.
- **Lints in `Cargo.toml`** (`[lints.rust]`, `[lints.clippy]`), not `clippy.toml`, which is unstable. `clippy::all` and
  `clippy::pedantic` as warnings, `unsafe_code = "forbid"`, `missing_docs = "warn"` (every declaration is documented, as the
  [documentation guide](documentation.md) asks). The `restriction` group is never enabled as a whole.
- `cargo clippy --all-targets --all-features -- -D warnings` and `cargo fmt --all -- --check` gate every change. `rustfmt.toml` sets
  `edition` and `style_edition = "2024"`, since a bare `rustfmt` defaults to edition 2015; the other options keep their defaults.
- **Tests run with `cargo nextest`** (0.9.150 or later; earlier ones have GHSA-vc25-qg58-w57c). nextest does not run doctests:
  `cargo test --doc` runs after it.
- **100 % coverage** of lines and functions with `cargo llvm-cov nextest --fail-under-lines 100 --fail-under-functions 100`
  (`make tui-cov`), the same bar as the 100 % of statements of Go. Branch coverage needs nightly and `--branch` and has no gate flag,
  so it is not gated on stable. Generated code (`tui/client`), the entry point (`src/main.rs`) and the thin calls into the operating
  system that a container cannot make (`src/platform.rs`: the credential store of the system and the browser) are left out with
  `--ignore-filename-regex`, like `*.gen.go` and `cmd/`; `coverage(off)` is nightly only and not used.
- **Regions are reported, not gated.** Every `?` is a region: each one whose error a test can produce has its test (cut answers,
  unwritable folders, refused codes), and infallible calls use `expect` with the reason instead of `?`. What remains are errors only
  the operating system produces, such as `accept` failing for lack of file descriptors; hiding them to reach 100 % of regions would
  make the code worse (2026-11-25: 99.9 % of regions).
- **Dependencies:** `cargo deny check` (advisories, licenses, bans, sources) with `deny.toml`. Licenses must be compatible with
  Apache-2.0, sources only crates.io, and duplicated TLS stacks are banned. cargo-deny covers what `cargo audit` checks, so audit is
  not used. `Cargo.lock` is committed.
- **Snapshots:** CI sets `INSTA_UPDATE=no`, so a changed snapshot fails and nothing is written; unused snapshots fail with
  `cargo insta test --unreferenced=reject`. Locally, `cargo insta test --review`.
