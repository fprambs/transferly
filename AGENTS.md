# Instructions for coding agents

Transferly is a self-hosted web file manager (Go backend, React frontend, OpenAPI contract in `api/openapi.yaml`) with a terminal client,
`transferly-tui`, in Rust under `tui/`. Read `CONTRIBUTING.md`, `docs/ROADMAP.md` (the plan, in Spanish) and, for the terminal client,
`docs/adr/0029-terminal-client-and-browser-login.md`, `docs/design/terminal-client.md` and `docs/design/terminal-client-mockups.md`.

## Rules that are not negotiable

- **Everything runs in Docker; install nothing on the host.** Rust: `docker compose -f compose.dev.yaml run --rm --no-deps -T rust <cmd>`
  (image with Rust 1.100, clippy, rustfmt, llvm-cov, nextest, deny). Go and Node: the `tools` service. Use `make` targets below.
- **Branch `feat/terminal-client`**; plain `git push` while working. Full suites (`make unit`, `make e2e`) only before a PR to `main`.
- **Every change keeps the gates green:**
  - `make tui-lint` (rustfmt + clippy pedantic with `-D warnings`), `make tui-test`, `make tui-cov` (**100 % of lines and functions**,
    regions reported), `make tui-deny` (licenses compatible with Apache-2.0, no OpenSSL or native-tls).
  - Backend: 100 % statement coverage (`scripts/coverage-go.sh`); frontend: 100 % with Vitest, TSDoc on every declaration.
- **Commits** in English, Conventional Commits, body explaining why. Commit as the git user already configured in this repository (`git config user.name`); do not change the author, and add no `Co-Authored-By`, `Signed-off-by` or any other trailer, nor mentions of an agent or tool, in commits or pull requests.
- **User-visible text** in English and Spanish: `tui/locales/en.json` and `es.json` must have the same keys (a test checks it).
- **Never share personal data** of the user with external services.

## Style of the terminal client

- Architecture (`tui/src/ui/`): `App` and `Home` are pure state machines: `Event` in, `Vec<Effect>` / `Vec<Ask>` out. The runtime
  (`runtime.rs`) runs effects as tokio tasks and feeds results back as `Event::Replied(Reply::…)`. Nothing in `app.rs`, `home.rs`,
  `home/transfers.rs`, `explorer.rs` or `transfers.rs` does I/O; the terminal is touched only in `platform.rs`.
- Every module and public item has a doc comment saying what it is for, in plain English. Names are words, not abbreviations.
- Tests live next to the code (`ui/tests.rs`, `ui/home_tests.rs`, `ui/explorer_tests.rs`, `#[cfg(test)] mod tests` in modules). They
  draw on `ratatui::backend::TestBackend` and assert on the text; network goes through `wiremock`; `tests/testenv.rs` runs against the
  isolated stack with `make tui-it`.
- Coverage tips that saved time: avoid closures in `map_err` (use fn paths such as `.map_err(failure)`); a `let … else { return … }`
  that never fails leaves a line uncovered (restructure, or keep it on one line with `#[rustfmt::skip]`); generic helpers count each
  instantiation, so keep them tiny.
- Colors come from the theme palette (`ui/theme.rs`, 12 themes exported from the web); never hard-code colors. Without colors
  (`NO_COLOR`) everything must stay readable.
- Screenshots: `make tui-mockups` draws `tui/mockups` (mockups in `src/main.rs` and `src/t7.rs`, real screens in `src/real.rs`) into
  `docs/design/screens/*.png`.

## Common commands

| What | Command |
|---|---|
| Lint, test, coverage, licenses of the TUI | `make tui-lint tui-test tui-cov tui-deny` |
| Against the isolated stack with real SFTP | `make tui-it` |
| Regenerate code from the contract | `make gen` |
| Screenshots of the design and the app | `make tui-mockups` |
| Build the static binary to try | `docker compose -f compose.dev.yaml run --rm --no-deps -T rust cargo build --release --target x86_64-unknown-linux-musl -p transferly-tui` |
| Development stack (web on :5173) | `TRANSFERLY_HTTP_PORT=8081 make testenv` |

## Current work

The next task is **T7** in `docs/ROADMAP.md`; the brief is `docs/handoff/t7.md`.
