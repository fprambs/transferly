# ADR-0016: Interactive logon, answered live from the browser

- Status: accepted
- Date: 2026-09-29

## Context

FileZilla's "Interactive" logon type asks the person, while connecting, whatever the server asks: the password, a one-time code, a custom challenge. ADR-0009 left the prompts that are not the stored password out of scope, because the connection runs on the server with no channel to the browser mid-handshake. Since 4.9 every client has a live event stream (the WebSocket of transfers and the connection log), so a question can reach the person who is connecting and the answer can come back while the handshake waits.

## Decision

**A prompt broker (`core/prompts`) relays the questions.** `Ask` publishes a `connection.prompt` event to the user who is connecting (name, instruction and each question with whether its answer may be shown), and waits for `POST /prompts/{id}` with the answers, `DELETE /prompts/{id}` when the person cancels, the end of the request, or three minutes. Every way out publishes `connection.prompt_closed`, so every tab of the user closes the dialog. A prompt belongs to its user: another account gets `prompt_not_found`. Answers are never stored, logged or audited, and the connection log only says that the server asked, with the name of the challenge. At most 16 prompts wait for one user at once (security review of phase 4): each holds a connection open, so many interactive connections from many tabs cannot pile up; one more fails as refused credentials.

**`interactive` is a logon type of SFTP and FTP sites with no stored secret.**
- SFTP offers `keyboard-interactive`, relaying each challenge, then `password`, asking for it with one hidden question, for servers that only offer that.
- FTP asks for the password after USER and shows the server's reply as the instruction, since one-time password servers put their challenge there. The patched FTP client (ADR-0015) gains `LoginInteractive` for it.
- An FTP connection that may wait for a person gets the answer window on top of its usual timeout; SFTP handshakes already wait on the request, not on a timer.

A cancel or an unanswered prompt fails the connection as refused credentials. The FileZilla import reads logon type 3 as interactive.

## Consequences

- Positive: servers with one-time codes or custom challenges can be reached, and no answer is kept anywhere.
- Negative: the pool closes idle connections, and connecting again asks again, as FileZilla does; a transfer that needs a new connection while nobody watches fails after the answer window. Prompts need the event stream: a client without it cannot answer.
