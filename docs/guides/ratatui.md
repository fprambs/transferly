# Ratatui and crossterm

Sources: [ratatui.rs](https://ratatui.rs/) (application patterns, event handling, the async terminal and event handler recipe, panic
hooks, snapshot testing) and [BREAKING-CHANGES.md](https://github.com/ratatui/ratatui/blob/main/BREAKING-CHANGES.md). Versions:
ratatui 0.30.2, crossterm 0.29.0 (checked 2026-11-24). Used by `transferly-tui` ([ADR-0029](../adr/0029-terminal-client-and-browser-login.md)).

## Rules

- **The terminal is always restored.** Entering raw mode and the alternate screen goes with leaving them on every exit path: normal
  quit, error and panic. Install a panic hook that restores the terminal first, ignoring its errors (`let _ =`), and then calls the
  original hook (`take_hook`, `set_hook`), so the panic message is printed to a usable terminal. Never panic while restoring.
- **One event loop, message passing.** A spawned task reads `crossterm::event::EventStream` and two `tokio::time::interval`s (tick
  and render) in a `tokio::select!` with a `CancellationToken`, and sends `Event`s on a channel; the app loop draws, awaits the next
  event, maps it to an action and updates the state. HTTP responses and WebSocket events enter the same channel, so the loop never
  awaits the network and drawing never blocks (see [tokio](tokio.md)).
- **Architecture:** state in one `App` model updated by actions (The Elm Architecture), with components that own their state for the
  panels (sites, local and remote explorers, transfers, log), each with `handle_key`, `update` and `render(&mut self, frame, area)`.
  Centralized matching of raw events does not scale; components get only the events of the focused panel.
- `render` has no side effects other than the `&mut` state that `StatefulWidget`s need (lists and tables keep their selection and
  offset there).
- Handle only `KeyEventKind::Press`; Windows also reports releases.
- Handle `Event::Resize` by redrawing; the next frame uses the new size.
- Rates: render at most 30 frames per second and only when something changed; the tick (4 Hz) drives spinners and elapsed times.
- 0.30 renamed `layout::Alignment` to `HorizontalAlignment`, removed `block::Title`, gives `Backend` an associated `Error` type and
  makes `Marker` non exhaustive. 0.31, coming, removes `assert_buffer_eq!` and splits `HighlightSpacing`; follow BREAKING-CHANGES.md
  when upgrading.
- Colors come from the theme of the client, never literals in widgets, as in the web interface ([theming](theming.md)).

## Tests

- Widgets and screens render into `Terminal::new(TestBackend::new(80, 24))` with a fixed size, and are compared with insta snapshots
  (`assert_snapshot!(terminal.backend())`); review changes with `cargo insta review`. Snapshots do not compare colors, so styles
  that carry meaning (selection, errors) are also asserted on the `Buffer` cells.
- Compare buffers with `assert_eq!(&actual, &expected)`, not the removed `assert_buffer_eq!`.
- State updates are tested without a terminal: action in, state and effects out.
- A few whole-app flows run the binary in a pseudo-terminal (login, open a site, upload); they are not the main tests.
