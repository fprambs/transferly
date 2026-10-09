# Security policy

Report security vulnerabilities privately through the repository's GitHub Security Advisories page:
https://github.com/fprambs/transferly/security/advisories/new

Do not put credentials, private keys, session tokens or exploit details in public issues. Include the affected version,
deployment configuration, a minimal reproduction with synthetic data and the expected impact. Private reporting must be enabled
in repository settings before the repository becomes public.

The stable release is not yet published. Security fixes will target the latest stable version once it exists; development builds
are not a supported production release. Support dates and affected versions will be stated in each advisory.

Transferly currently supports a single application instance. Protocol capabilities differ: safe overwrite requires an adapter/server
that implements atomic replacement. Unsupported replacement fails without deleting the destination; save under another name instead.
