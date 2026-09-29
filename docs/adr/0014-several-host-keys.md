# ADR-0014: Several trusted keys per server

- Status: accepted
- Date: 2026-09-29

## Context

Sentinel trusted one key per server, key type and port (trust on first use). A user reported a server that presents a different key on each connection, most likely several nodes behind a load balancer, each with its own key. Sentinel reported it as a changed key and offered only "Try again". FileZilla failed with "socket unexpectedly closed". WinSCP warns and offers to update the key, to add it next to the trusted one, or to connect once. OpenSSH's `known_hosts` also accepts several lines for one host.

## Decision

**A server may have several trusted keys of the same type.** The primary key of `known_hosts` now includes the fingerprint, so a server's keys are still grouped by host, port and key type. A key is accepted when it matches any of them. A key of that type that matches none is `host_key_changed`, and the error lists the fingerprints already trusted (`params.trusted`), so the user can compare them.

**Trusting a changed key needs an explicit decision.** `POST /known-hosts` takes `mode`:
- `add` keeps the trusted keys and adds the new one, for a service with several servers behind one name;
- `replace` drops the trusted keys of that type and keeps only the new one, for a server whose key really changed;
- left out, a changed key is still refused with `host_key_changed`, so an older client cannot overwrite a trusted key by accident.

Every decision is audited as `host_key.trusted` with the fingerprint and the mode. `DELETE /known-hosts` takes an optional `fingerprint` to forget one key; without it, every key of the type is forgotten.

**There is no "connect once".** Connecting to a server that presents an unexpected key, without recording a decision, is the moment a man-in-the-middle attack succeeds. The warning offers add, replace or cancel (which disconnects the site), and a small help text says when each fits.

## Consequences

- Positive: servers behind a load balancer work after one "add" per node, the warning shows what changed, and every trusted key can be reviewed and forgotten on its own on the trusted servers page.
- Negative: a user who picks "add" without checking the fingerprint trusts an attacker's key for good, where "replace" or a one-time connection would at least be visible the next time. The warning stays a danger card with the help text for that reason.
- The migration's down step keeps only the oldest key of each server and type.
