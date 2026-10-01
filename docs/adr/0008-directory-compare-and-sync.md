# ADR-0008: Directory comparison and synchronization

- Status: accepted
- Date: 2026-09-27

## Context

Phase 4 starts with the feature FileZilla users expect most: comparing two folders and mirroring one onto the other. Transferly already lists, downloads, uploads, copies between sites and archives folders (`core/files`), each remote site carries a listing time zone (`Site.ListTimezone`) because servers report times with no zone of their own, and transfers already go through `core/transfer` with its scheduler, bandwidth caps and audit trail. This ADR decides what "the same file" means across that uncertainty, what a plan looks like before anything moves, and what happens to files that exist on only one side.

## Decision

Scope of one comparison: two folders, read non-recursively one level at a time like the existing listing, walked together on demand. Either side can be a remote site or the user's local disk through the File System Access API the local panel already uses; the four combinations (remote-remote, remote-local, local-remote, local-local is refused, both sides need to differ) share one comparison model. Local entries are described with the same `Entry` shape the explorer already uses, so the comparison never special-cases which side is local.

Matching entries by name only, case-sensitively; a rename shows as a delete on one side and a create on the other, which is honest about what SFTP/FTP/local FS can tell us and matches how FileZilla itself compares.

Deciding whether two files with the same name differ:
1. **Size**: different sizes always mean different files, on every server; nothing else is read.
2. **Modification time**, when sizes agree, compared with a **60-second tolerance** in either direction, because SFTP and FTP report times to the second and clocks drift. The times compared are the ones the listing already returns: each site's FTP adapter applies its configured `ListTimezone` before answering (`adapters/remotefs/ftpfs`), and SFTP has no zone ambiguity, so every `RemoteEntry.modifiedAt` the API returns, remote or local, is already an absolute instant safe to compare directly — the comparison itself does no zone math.
3. **Checksum**, only offered as a manual override once sizes agree: a button on an ambiguous row asks both sides for their checksum (the existing per-file endpoint) and replaces the time-based verdict with a sure one. Opt-in and one row at a time, because hashing a whole tree is slow and rarely needed.

Same size and time within tolerance, unless the checksum override says otherwise → equal.

Sides without a size or a time it can trust (a directory, or an entry the server describes incompletely) always compare by descending one level and matching what is inside, never by guessing.

The plan, before anything moves:
- One row per differing or one-sided entry: `create` (exists only on the target's missing side), `update` (differs), `delete` (exists only on the side sync would empty into), each with its full path, sizes and times on both sides and which rule decided it (checksum, size, time or existence).
- Direction is chosen once per sync, before comparing: **mirror source → target** or **mirror target → source**; there is no automatic bidirectional merge; a two-way *conflict* (both sides changed since the two are compared) is not detected because there is no last-known-common state to compare against — every difference is shown, and the person picks the direction and, per row, whether to include it.
- **Nothing deletes by default.** A checkbox per delete row, unchecked by default, and a second explicit confirmation before the sync starts if any delete is checked ("this will permanently remove N files from &lt;side&gt;"). A sync that only ever creates and updates needs one confirmation like today's transfers; one with deletes needs the extra step.
- The plan is a snapshot: it is generated once, shown, edited (rows can be unchecked) and then run. A sync never re-lists mid-run to catch changes; a row that fails because the remote changed underneath (moved, removed, permission) is reported as a failed step, not retried silently, exactly like a single transfer failing today.

Comparing needs no new backend endpoint: both panes already fetch their listing (or, for a local folder, already list it through the File System Access API) as the same `RemoteEntry` shape with absolute, comparable times, so the diff itself — matching by name, applying the size and time rules above — runs in the frontend against data it already has. The only backend call the comparison uses is the existing per-file checksum endpoint, and only for the manual override.

Execution reuses what exists instead of inventing a second transfer path: every `create` and `update` becomes a normal upload, download or site-to-site copy (`core/files`, `core/transfer`) submitted the way the dual-pane explorer already submits one when a file is dragged from one pane to the other; every checked `delete` is a normal remove. The interface runs the plan's rows as a batch of ordinary transfers it already knows the identifiers of, follows them together through the same per-transfer progress events, and shows one aggregate bar and a per-row status; canceling the sync cancels those transfers and leaves every other one untouched. Local-side steps run against the File System Access API exactly as today's local panel writes files, so a local disk is never state the server has to track, and nothing about a "sync" is persisted once the browser tab closes — like the transfer queue itself, it is a session-scoped view over ordinary transfers.

Symlinks are never followed during a comparison, matching the existing rule for archiving and copying folder trees (fase-2 finding).

## Consequences

- Positive: no new transfer machinery and no new backend surface for comparing (the diff is pure, client-side, over data already fetched); no new permission model (an update still needs upload+download rights, a delete still needs delete rights, checked per side by the existing role matrix when each transfer is submitted); the plan step gives a person a chance to see every destructive action named before it happens.
- Negative: name-only matching cannot detect a rename or a move, which will show as a spurious delete+create pair; without a stored last-sync state, a true three-way conflict (both sides changed) cannot be flagged as one — it is just another difference the person resolves by choosing a direction; a large tree still means many small transfers queued at once, bounded only by the scheduler and bandwidth limits from 3.8, not by anything sync-specific yet (a batch size cap can be added later if that proves too coarse in practice); a sync followed from one browser tab is not visible from another the way a saved job would be.
