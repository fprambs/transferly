# ADR-0013: Private key files

- Status: accepted
- Date: 2026-09-28

## Context

A site that logs in with a key took the key as pasted text, and it was only checked when connecting: a public key, a PuTTY file or a missing passphrase failed later as an authentication error. FileZilla takes a key file and reads OpenSSH, PEM and PuTTY keys alike. The SSH library reads OpenSSH, PEM (plain and with the legacy OpenSSL encryption) and plain PKCS#8, but not PuTTY's PPK format nor encrypted PKCS#8. It also trusts the cost parameters a file names (bcrypt rounds of an OpenSSH key), and a PPK file names its own Argon2 memory and passes.

## Decision

**Keys are read by content and checked when saved.** The `sshkey` package tells OpenSSH, PEM, PKCS#8 (plain or PBES2 with PBKDF2 and AES-CBC), RFC 4716 and PPK versions 2 and 3 apart by their content, whatever the file is called. It reports whether the file holds a private key, the type, size and SHA-256 fingerprint, and whether the passphrase opens it. Saving a site or a quick connection refuses a key it cannot use with a stable code for the site's or the jump host's key: `private_key_is_public`, `passphrase_required`, `wrong_passphrase`, `private_key_unreadable` and `private_key_unsupported` with a reason. `POST /ssh-keys/inspect` answers the same questions without storing anything, so the form can show the key before saving.

**PuTTY and encrypted PKCS#8 keys are stored in OpenSSH's format**, encrypted with the same passphrase, so connecting keeps using the SSH library alone. Keys the library reads are stored as given. A key that is not encrypted drops any passphrase sent with it, since a passphrase makes the library refuse an unencrypted key.

**Only RSA, ECDSA on the SSH curves and Ed25519 are used.** DSA (removed from OpenSSH), Ed448 and X25519 are refused as `key_type`. PPK files derived with Argon2d are refused as `argon2d`: the Argon2 library offers only Argon2i and Argon2id, and PuTTYgen's default is Argon2id.

**Costly derivations are bounded and run two at a time.** The bounds are 1024 bcrypt rounds (ssh-keygen uses 16), Argon2 memory up to 256 MiB with memory × passes up to 4 GiB (PuTTYgen uses 8 MiB × 13), 16 lanes, and 1,000,000 PBKDF2 iterations (OpenSSL uses 2048). Files above them are refused as `cost`. Inspecting and saving are the only places an untrusted key is read. Connecting reads keys that were checked when saved.

## Consequences

- Positive: a wrong key is caught while the form is open, PuTTY users need not convert their keys, and a crafted file cannot hold the server's processor or memory.
- Negative: the stored form of a PuTTY key differs from the uploaded file (the same key, in another format); Argon2d files must be saved again in PuTTYgen; replacing an encrypted key needs its passphrase in the same request.
