# ADR-0020: Transferly name and brand

- Status: accepted
- Date: 2026-10-01

## Context

The product is published as Transferly, in the repository `fprambs/transferly`, and had been developed under a working name that lived in the Go module path, the binary, the environment variables, the Docker stacks, the database, cookies, file formats, theme identifiers, the domain separation labels of the cryptography and the documentation; the interface had no logo of its own. A brand kit was designed for the new name (`branding/`): a mark, logos, a palette (Core Blue, Action Orange, Signal Green), typography and icons. The product is still in development and no installation has data that must survive, so the working name is removed without a trace.

## Decision

**One name everywhere.** Transferly is the only name, with no compatibility layer for the working name:

- the Go module is `github.com/fprambs/transferly` and the binary `cmd/transferly`;
- environment variables are `TRANSFERLY_*`; the Docker projects, images, Postgres role and databases, and the users of the test servers are `transferly`;
- the session cookie is `transferly_session`, and the session and anti-CSRF labels start with `transferly/`, so existing sessions end once;
- the encrypted export is `transferly-export` (`transferly-sites.json`) and its import endpoint `/imports/transferly`;
- browser storage keys, panel layout identifiers and the messages of the isolated viewer page use `transferly`.

**Including the cryptography.** The domain separation labels of the vault and the second factor are `transferly/secret/v1`, `transferly/workspace-key/v1`, `transferly/user-key/v1`, `transferly/dek-wrap/v1`, `transferly/mfa/v1/…` and `transferly/mfa/recovery/v1`. They are part of the ciphertexts and derived keys, so secrets and second factors sealed under the working name cannot be read: development databases are recreated, vaults set up again and sites imported again. From now on these labels are frozen: renaming them again would make every stored secret unreadable. FileZilla is not an old name: its mentions are the importer of its site manager and the behaviors Transferly follows.

**Brand themes.** Transferly Dark and Transferly Light are the default themes, with the palette of the kit: Action Orange for actions, Signal Green for success and live connections, Core Blue for brand text on light surfaces. The theme migration has `transferly-dark` as its default from the start. The logo keeps its brand colors in every theme, from `--brand-arrow` and `--brand-ribbon` in `index.css`.

**The kit is the source.** `branding/scripts/build.py` generates the logos, icons, favicon and tokens; the interface copies the favicon and draws the mark from the same geometry.

## Consequences

- Positive: one name in code, operations and interface; the brand reaches the favicon, the top bar and the sign-in pages; the themes follow the brand and still pass the contrast rules.
- Negative: development databases and Docker volumes of the working name are not reused; vaults, second factors and sites are set up again; files exported before the rename are not accepted by the import.
