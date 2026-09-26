# ADR-0006: Team workspaces, invitations and shared keys

- Status: accepted
- Date: 2026-09-26

## Context

Phase 3 lets several people work in one workspace. Three things need a decision: how people join (the instance has no open sign-up today), how roles apply to a shared workspace, and how the members share the secrets of a site without the server keeping a key that opens every vault. The vault design of [ADR-0004](0004-credential-vault.md) already promises a workspace key sealed to each member's X25519 key; this ADR fixes the details.

## Decision

Joining:

- Registration stays closed. A member with `manage_members` creates an **invitation** for an email address and a role (`admin`, `editor` or `viewer`; never `owner`). The reply shows a one-time link; the server sends no email.
- The token is 256 random bits, stored only as a SHA-256 hash, valid for 7 days and single use. It is bound to the normalized email: accepting requires an account with that email, which the invitee signs in to or creates from the link (sign-up is allowed only with a valid invitation, and only for its email).
- Every failure of the accept call (unknown, expired, used, revoked or foreign token) answers the same 404, so tokens and accounts cannot be probed. Creating an invitation never tells whether the email already has an account.
- Pending invitations can be listed and revoked, and each creation, acceptance and revocation goes to the audit log.

Roles and ownership:

- Team workspaces use the matrix of the architecture document. A workspace always has exactly one owner; ownership moves only through an explicit transfer by the owner. Personal workspaces have one member and cannot be shared.
- A member can change or remove only members ranked below them, and grant only roles below their own: an admin manages editors and viewers, the owner also manages admins, and nobody changes the owner or their own role. A member may leave a team unless they are its owner.

Keys:

- Each user has an X25519 key pair created with the vault; the private key is sealed with the user's DEK. The public key is readable by workspace admins.
- A team workspace has a random DEK. For every member it is sealed with ECDH between an ephemeral key and the member's public key, HKDF-SHA-256 and AES-256-GCM, bound to workspace, user and DEK version, and stored in `memberships.wrapped_workspace_dek`. The personal workspace keeps using the user DEK.
- A member who has not set up a vault cannot receive the DEK; the invitation can be accepted, and the DEK is sealed for them the next time an admin session with an unlocked vault sees them as pending.
- Removing a member (or a member leaving) rotates the workspace DEK: a new version is created, sealed for the remaining members, and secrets are re-sealed in the background under the new version. Until a secret is re-sealed its `dek_version` tells which key opens it, so nothing is lost if the process stops.
- A viewer holds the DEK too, because the server needs it to connect on their behalf; the API still never returns secrets, and the role matrix decides what each member can do with a connection.

## Consequences

- Positive: no shared password, no sign-up abuse, a leaked token is useless to anyone but its invitee, and a removed member's copy of the old DEK protects nothing created after the rotation.
- Negative: an admin must be online with an unlocked vault to hand the DEK to a member who set up their vault late; without email, the link travels by whatever channel the admin picks; a removed member could have copied the old DEK while they were a member, which is why the rotation covers future secrets and old ones are re-sealed, but credentials they already saw must still be changed at the remote server.
