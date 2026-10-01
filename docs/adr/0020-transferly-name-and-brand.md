# ADR-0020: Transferly name and brand

- Status: accepted
- Date: 2026-10-01

## Context

The product was developed as Sentinel and is published as Transferly, in the repository `fprambs/transferly`. The old name lived in the Go module path, the binary, the environment variables, the Docker stacks, the database, cookies, file formats, theme identifiers and the documentation, and the interface had no logo of its own. A brand kit was designed for the new name (`branding/`): a mark, logos, a palette (Core Blue, Action Orange, Signal Green), typography and icons. No installation with real data exists yet.

## Decision

**One name everywhere.** Transferly replaces Sentinel, with no compatibility layer for the old name:

- the Go module is `github.com/fprambs/transferly` and the binary `cmd/transferly`;
- environment variables are `TRANSFERLY_*`; the Docker projects, images, Postgres role and databases, and the users of the test servers are `transferly`;
- the session cookie is `transferly_session`, and the session and anti-CSRF labels start with `transferly/`, so existing sessions end once;
- the encrypted export is `transferly-export` (`transferly-sites.json`) and its import endpoint `/imports/transferly`;
- browser storage keys, panel layout identifiers and the messages of the isolated viewer page use `transferly`.

**Two kinds of mention stay.** The domain separation labels of the vault and the second factor (`sentinel/secret/v1`, `sentinel/workspace-key/v1`, `sentinel/user-key/v1`, `sentinel/dek-wrap/v1`, `sentinel/mfa/v1/…`, `sentinel/mfa/recovery/v1`) are part of the ciphertexts and derived keys; changing them would make every stored secret and second factor unreadable. They are constants, not a name, and are not renamed. FileZilla is not an old name: its mentions are the importer of its site manager and the behaviors Transferly follows.

**Brand themes.** Transferly Dark and Transferly Light replace Sentinel Dark and Sentinel Light as the default themes, with the palette of the kit: Action Orange for actions, Signal Green for success and live connections, Core Blue for brand text on light surfaces. Migration 30 renames the stored identifiers and the default. The logo keeps its brand colors in every theme, from `--brand-arrow` and `--brand-ribbon` in `index.css`.

**The kit is the source.** `branding/scripts/build.py` generates the logos, icons, favicon and tokens; the interface copies the favicon and draws the mark from the same geometry.

## Consequences

- Positive: one name in code, operations and interface; the brand reaches the favicon, the top bar and the sign-in pages; the themes follow the brand and still pass the contrast rules.
- Negative: development databases and Docker volumes of the old name are not reused, sessions end once, and files exported before the rename are not accepted by the import. The vault and second factor labels keep the old name inside the code.
