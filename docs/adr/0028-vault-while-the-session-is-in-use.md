# ADR-0028: The vault stays unlocked while the session is in use

- Status: accepted
- Date: 2026-10-05
- Amends: ADR-0004 (where unlocked keys live)

## Context

ADR-0004 keeps the data-encryption key (DEK) of an unlocked vault in an in-memory key ring with an idle timeout. Two things made the
vault lock while people were working:

- the idle time of a key was extended only when a stored secret was opened, that is, when a new connection was made. Browsing over
  connections already open kept the session alive but let the key expire, so the next new connection, often a preview, failed with
  `vault_locked`;
- the key ring lives in the memory of the process. A restart loses every key while sessions, stored in the database, stay signed in.
  In production restarts are rare, but the development server restarts on every change of the code.

The viewer also showed `vault_locked` with a retry that could not succeed, since nothing offered to unlock the vault.

## Decision

1. **Activity keeps the vault unlocked.** Every request of a signed-in session extends the idle time of its key (`KeyRing.Touch`),
   so the vault locks only when the session stops being used, and the session itself ends after the same idle time.
2. **A locked vault is never a dead end.** Wherever a read of the viewer fails with `vault_locked` (the file, the entries of an
   archive, an entry), the master password dialog opens at once and the read runs again once the vault is unlocked.
3. **Development may keep keys across restarts.** With `TRANSFERLY_VAULT_REMEMBER_SESSIONS=true`, which only `compose.dev.yaml`
   sets, the DEK of each unlocked session is kept in the table `vault_sessions`, sealed with AES-256-GCM under a key derived with
   HKDF-SHA256 from the session token, with the session identifier as associated data. The token lives only in the HttpOnly cookie
   of the browser; the database stores its SHA-256. After a restart, the first request of the session opens the sealed key with
   its token and puts it back in the key ring. The row is deleted when the vault is locked, and with the session when it signs out
   or expires (`ON DELETE CASCADE`). Off by default; production does not set it.

## Consequences

- Positive:
  - the vault no longer locks while someone is working, and a locked vault is unlocked where it is needed;
  - development keeps its vaults unlocked across the restarts of air.
- Negative, only where keys are kept across restarts:
  - a stolen session cookie opens the vault for as long as the session lives (at most 8 hours by default), not only while the
    process holds the key;
  - someone with both the database and a session cookie can open that session's DEK offline;
  - a database alone, or a backup, still reveals nothing, and neither does a cookie of a session that ended.
- Production keeps the guarantee of ADR-0004 that DEKs are never persisted.
