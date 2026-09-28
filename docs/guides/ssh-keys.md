# SSH key files

Sources (read 2026-09-28): [PuTTY manual, appendix C: PPK file format](https://the.earth.li/~sgtatham/putty/latest/htmldoc/AppendixC.html), [RFC 4716](https://www.rfc-editor.org/rfc/rfc4716) (SSH2 public key files), [RFC 8018](https://www.rfc-editor.org/rfc/rfc8018) (PBES2, PBKDF2), [RFC 5958](https://www.rfc-editor.org/rfc/rfc5958) (PKCS#8 EncryptedPrivateKeyInfo), and `go doc golang.org/x/crypto/ssh` and `golang.org/x/crypto/argon2` v0.57.0. Package: `backend/internal/core/sshkey`. Decision: [ADR-0013](../adr/0013-private-key-files.md).

## What the SSH library reads

- `ssh.ParseRawPrivateKey` reads PEM blocks `OPENSSH PRIVATE KEY`, `RSA PRIVATE KEY`, `EC PRIVATE KEY`, `DSA PRIVATE KEY` and `PRIVATE KEY` (PKCS#8). An encrypted one answers `*ssh.PassphraseMissingError`, whose `PublicKey` is set only for OpenSSH keys (their public half is in clear text).
- `ssh.ParseRawPrivateKeyWithPassphrase` answers `x509.IncorrectPasswordError` for a wrong passphrase, and "not an encrypted key" for a key that is not encrypted: never pass a passphrase for an unencrypted key.
- It does not read `ENCRYPTED PRIVATE KEY` (encrypted PKCS#8) nor PPK. `ssh.MarshalPrivateKey` and `MarshalPrivateKeyWithPassphrase` write RSA, ECDSA and Ed25519 keys in OpenSSH's format; that is how converted keys are stored.
- OpenSSH keys return `*ed25519.PrivateKey` (a pointer), PKCS#8 returns `ed25519.PrivateKey`; `ssh.NewSignerFromKey` takes both. It refuses ECDSA on curves other than P-256, P-384 and P-521.
- The library uses the bcrypt rounds an OpenSSH key names without a bound. `sshkey` reads the header first and refuses more than 1024.

## PPK (appendix C)

- Text: `PuTTY-User-Key-File-<version>: <algorithm>`, `Encryption` (`none` or `aes256-cbc`), `Comment`, `Public-Lines` and base64, for version 3 `Key-Derivation` (`Argon2id`, `Argon2i`, `Argon2d`) with `Argon2-Memory` (KiB), `-Passes`, `-Parallelism`, `-Salt` (hex), then `Private-Lines` and base64, and `Private-MAC` (hex). Lines may end in LF, CRLF or CR.
- Version 3: Argon2 with empty secret and associated data gives 80 bytes: AES-256 key, CBC IV, HMAC-SHA-256 key. Unencrypted files have empty keys but still a MAC.
- Version 2: AES key = first 32 bytes of SHA-1(0 ‖ passphrase) ‖ SHA-1(1 ‖ passphrase) (32-bit sequence numbers), IV zero, MAC HMAC-SHA-1 keyed with SHA-1("putty-private-key-file-mac-key" ‖ passphrase), the passphrase empty when unencrypted.
- The MAC covers the algorithm, encryption, comment, public blob and decrypted private blob (padding included), each as an SSH string. A MAC mismatch on an encrypted file is a wrong passphrase; on an unencrypted one, a damaged file.
- Private blobs: RSA `mpint d, p, q, iqmp` (n and e come from the public blob); ECDSA `mpint d`; Ed25519 a 32-byte string, the seed (the manual says mpint, PuTTY writes a fixed-length string). The halves are checked to belong together (`rsa.Validate`, recomputing the public point, the Ed25519 public key from the seed).
- Argon2d is not in `x/crypto/argon2` and is refused. Version 1 was never released and is refused.

## Encrypted PKCS#8

`EncryptedPrivateKeyInfo` with PBES2 (1.2.840.113549.1.5.13): PBKDF2 (…1.5.12) with HMAC-SHA-1 (default), -256, -384 or -512, and AES-128/192/256-CBC with the IV as parameter. The decrypted DER is read with `asn1.Unmarshal`, which leaves the padding aside, and `x509.ParsePKCS8PrivateKey`; a failure there is a wrong passphrase.

## Tests

`testdata` holds keys made for the tests with `ssh-keygen`, `puttygen` and `openssl` (Alpine), with the passphrase `correct horse`, and the public keys the tools exported, whose fingerprints the tests compare. Damaged and crafted files (a valid MAC over a private half that does not match, bcrypt rounds or Argon2 costs beyond the bounds, PBES2 variants) are built in the tests.
