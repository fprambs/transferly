# Design of the terminal client

The interface of `transferly-tui` ([ADR-0029](../adr/0029-terminal-client-and-browser-login.md)) before it is built. The screens are
drawn with ratatui itself by `tui/mockups`, so what is shown here is what the library renders: see
[terminal-client-mockups.md](terminal-client-mockups.md), with a screenshot of each in the colors of the Transferly Dark theme
(`screens/`), regenerated with `make tui-mockups`.

## Sources

- **termscp** ([manual](https://docs.termscp.rs/), keyboard shortcuts and explorer chapters, read 2026-11-24): two explorer panels
  with the local disk on the left, a transfer queue at the bottom, a status bar, function keys in the Midnight Commander tradition
  (`F5` copy, `F7` new folder, `F8` delete), marking several files, synchronized browsing, filter with `/`.
- **termirs** (source of `key_event/` and `ui/` on `master`, read 2026-11-24): vim keys next to the arrows everywhere, a filter that
  narrows the list while typing, a footer with the keys of the screen and the version, a source selector per panel, red confirmation
  popups that say the action cannot be undone, the popup that waits while connecting and can be cancelled.
- **Transferly web** (`features/shortcuts`): `?` help, `Ctrl+K` command palette, `Ctrl+B` sites panel, `Enter` open, `Backspace` up,
  `F2` rename, `Delete` delete, `/` and `Ctrl+F` filter, tabs of connections, the toolbar (refresh, cancel, disconnect), the details
  panel, the transfer dock, the connection log.

## Principles

1. **Every action has a key**, shown where it applies: the status bar shows the keys of the focused panel, `?` shows them all, and
   the palette (`Ctrl+K`) finds any action by name. The mouse is optional.
2. **The same keys as the web** wherever a terminal can send them, so moving between the two costs nothing.
3. **Arrows and vim keys both work** in every list: `↑↓` and `j k`, `←` and `h` go up, `→` and `l` go in, `g`/`G` top and bottom.
4. **Only keys a terminal can send.** `Ctrl+H` is Backspace, `Ctrl+I` is Tab, `Ctrl+J` and `Ctrl+M` are Enter, `Ctrl+S` and `Ctrl+Q`
   may freeze the terminal, `Ctrl+Z` suspends, and `Ctrl+Shift+letter` or `Ctrl+Tab` cannot be told apart. None of them is used.
5. **Nothing destructive without a confirmation** that names what it will do; `Esc` always backs out of a popup.
6. **The server does the work.** The client shows what the API says; long operations show progress and can be cancelled.
7. **Same language and look as the web**: English and Spanish, and the twelve themes of the web with their exact colors; the
   language, the theme and the time zone are those of the account, so changing them in one changes them in the other.
8. **Commands and settings as in Claude Code**: `/` opens a command line at the bottom with the commands that match, and `/config`
   lists the settings to change in place.

## Layout

```
┌ tabs ────────────────────────────────────────────────────────────────────────────┐
│ sites (Ctrl+B) │ left panel                  │ right panel                         │
│ tree of the    │ local disk or a site        │ a site                              │
│ workspace      │ table: name, size, modified │ table: name, size, modified         │
├────────────────┴─────────────────────────────┴─────────────────────────────────────┤
│ Transfers | Log (Ctrl+E)                                                           │
├────────────────────────────────────────────────────────────────────────────────────┤
│ status: server · account · workspace · vault · sync        keys of the focused panel │
└────────────────────────────────────────────────────────────────────────────────────┘
```

- **Tabs** on the top line: each tab is a pair of panels, as in the web. The active one is highlighted; the version sits on the right.
- **Sites panel** on the left, 28 columns, hidden with `Ctrl+B`: the collections and sites of the workspace as a tree, with a dot on
  the connected ones.
- **Two explorer panels**: the left one starts on the local disk, the right one on a site. `o` changes what a panel shows (the local
  disk, a site, a quick connection). The focused panel has a highlighted border; its title is the site and its bottom border the path.
- **Bottom panel**, 9 rows, hidden with `Ctrl+E`, with two tabs: the transfer queue and the connection log.
- **Status bar** on the last line: the server, the account, the workspace, the state of the vault, synchronized browsing, a notice of
  a new release, and on the right the keys of the focused panel.
- **Narrow terminals**: below 100 columns the sites panel hides and only the focused explorer panel is shown, `Tab` switching
  panels. Below 60 × 16 the client asks for a larger window.
- **Popups** are centered over the screen with a cleared background: questions of the server, confirmations, forms, help, palette.

## Keys

Keys are written as the status bar and the help show them. A letter key acts only when no text field has the focus.

### Everywhere

| Key | Action | Web |
|---|---|---|
| `?`, `F1` | Help with every key of the screen | `?` |
| `/` | Command line: `/config`, `/theme`, `/language`… | |
| `Ctrl+K`, `Ctrl+P` | Command palette: actions, sites, folders by name | `Mod+K` |
| `Ctrl+B` | Show or hide the sites panel | `Mod+B` |
| `Ctrl+E` | Show or hide the transfers and log panel | panels menu |
| `Ctrl+O` | Quick connection in the focused panel | `Mod+Shift+K` |
| `Tab`, `Shift+Tab` | Next or previous panel | |
| `Alt+1`…`Alt+9` | Go to tab 1 to 9 | |
| `Alt+PgDn`, `Alt+PgUp` | Next or previous tab | `Alt+PgDn`, `Alt+PgUp` |
| `Ctrl+T` | New tab | |
| `Ctrl+W` | Close the tab | `Ctrl+W` |
| `Ctrl+C` | Cancel what the focused panel is doing; with nothing to cancel, ask to quit | toolbar cancel |
| `q`, `F10` | Quit; asks when transfers are running in this client | |
| `Ctrl+L` | Redraw the screen | |

### Explorer panels

| Key | Action | Web |
|---|---|---|
| `↑ ↓`, `j k` | Move | `↑ ↓` |
| `PgUp PgDn`, `Home End`, `g G` | Move by page, to the top, to the bottom | |
| `Enter`, `→`, `l` | Open the folder; on a file, copy it to the other panel (asks first) | `Enter` |
| `Backspace`, `←`, `h` | Up one level | `Backspace` |
| `Space`, `Insert` | Mark or unmark and move down | |
| `Ctrl+A` | Mark everything in the folder | |
| `Esc` | Clear the marks, then the filter | |
| `F5`, `c` | Copy the marked files, or the one under the cursor, to the other panel | |
| `F7`, `n` | New folder | toolbar |
| `F2` | Rename | `F2` |
| `Delete`, `F8` | Delete, after a confirmation | `Delete` |
| `f`, `Ctrl+F` | Filter while typing; `Enter` keeps it, `Esc` clears it | `Mod+F` (`/` in the web) |
| `i` | Details of the entry: size, dates, permissions, checksum on demand | details panel |
| `.` | Show or hide hidden files | |
| `s` | Sort by name, size or date, ascending or descending | column headers |
| `r` | Refresh | toolbar refresh |
| `Ctrl+G` | Go to a path | breadcrumbs |
| `Ctrl+Y` | Synchronized browsing on or off | |
| `o` | Show the local disk, a site or a quick connection in this panel | open a site |
| `Ctrl+D` | Disconnect the site of this panel | toolbar disconnect |

Copying between the local disk and a site is an upload or a download; between two sites the server copies (`POST /copies`). When a
file exists at the destination the confirmation asks what to do: fail, overwrite, skip, keep the newer, or keep both.

### Sites panel

| Key | Action |
|---|---|
| `↑ ↓`, `j k` | Move |
| `→`, `l`, `←`, `h` | Open or close a collection |
| `Enter` | Open the site in the focused explorer panel |
| `t` | Open the site in a new tab |
| `f`, `Ctrl+F` | Filter the sites by name or host |
| `w` | Change the workspace |

### Transfers and log panel

| Key | Action |
|---|---|
| `[`, `]` | Transfers tab or log tab |
| `↑ ↓`, `j k` | Move |
| `p` | Pause or resume the transfer |
| `x`, `Delete` | Cancel the transfer, after a confirmation |
| `R` | Resume a stopped upload of this client |
| `+`, `-` | Raise or lower its priority |
| `Enter` | Details: paths, sizes, speed, error and its guidance |
| `f` | In the log, follow new lines or stop following |

### Popups and forms

`Enter` confirms, `Esc` cancels, `Tab` and `Shift+Tab` move between fields and buttons, `←` and `→` move between buttons, and the
underlined letter of a button presses it (`y` yes, `n` no). Text fields edit with `←`, `→`, `Home`, `End`, `Backspace`, `Delete`,
`Ctrl+U` (clear) and `Ctrl+W` (delete a word), and paste with the terminal's own paste.

## Commands

`/` replaces the status bar with a command line, as Claude Code does: the commands that match what is typed are listed above it with
what they do, `↑ ↓` choose, `Tab` completes, `Enter` runs and `Esc` closes. A command can take its value right away, such as
`/theme nord` or `/language es`; without it, it opens its list.

| Command | Does |
|---|---|
| `/config` | The settings, below |
| `/theme [name]` | Wear another theme, with a live preview while choosing; it is the theme of the account |
| `/language [en\|es]` | Change the language; it is the language of the account |
| `/workspace [name]` | Change the workspace |
| `/server` | Switch to another server profile, or add one (signing in through the browser) |
| `/sessions` | The terminal sessions of the account, to end one |
| `/web` | Open the folder of the focused panel in the browser |
| `/update` | Install the new release, as `U` does |
| `/keys` | Every key of the screen, as `?` does |
| `/logout` | Sign this terminal out and forget its token |
| `/quit` | Close the client, as `q` does |

This is why `/` is not the filter key here as it is in the web: filtering is `f` or `Ctrl+F`, and `Ctrl+F` works in both.

## Settings

`/config` lists every setting with its value and where it lives; `↑ ↓` choose, `← →` change the value in place, `Enter` opens a list
when there are many values, `Esc` closes. Changes apply at once.

| Setting | Values | Lives in |
|---|---|---|
| Language | English, Español | the account (`PATCH /me`), shared with the web |
| Theme | the twelve themes of the web | the account, shared with the web |
| Time zone | IANA time zones | the account, shared with the web |
| Colors | the theme's, or the terminal's own palette | this computer |
| Enter on a file | copy to the other panel (with a confirmation), or show its details | this computer |
| Hidden files | shown, hidden | this computer |
| Ask before quitting with transfers | yes, no | this computer |
| Check for updates | once a day, never | this computer |

Settings of this computer live in `~/.config/transferly/config.toml` with the profiles of the servers; the account settings follow the
account to every computer and to the web.

## Screens

1. **Server**: started with no profile, the client asks for the address of the server, with the profiles used before below it.
2. **Approval in the browser**: the address to open, a QR code of it, a spinner while it waits for the callback; `o` opens the
   browser again, `c` copies the address, `p` switches to pasting the code, `Esc` goes back. Over SSH it starts in the paste mode.
3. **Explorer**: the main screen above. The first start of an account opens the personal workspace with the local disk on the left
   and the last site used, or nothing, on the right.
4. **Vault**: when a site needs it, a popup asks for the master password and the action runs again once it is unlocked.
5. **Questions of the server**: trust a host key (with the fingerprint, and both fingerprints when it changed), a password the site
   asks for, keyboard-interactive prompts. They name the site and can be refused.
6. **Confirmations**: delete (red, lists what goes, "this cannot be undone"), copy with existing files, quit with transfers running.
7. **Forms**: new folder, rename, go to path, quick connection.
8. **Help**: every key of the screen, grouped as above, with a filter.
9. **Palette**: one field, fuzzy matches of actions, sites and recent folders, each with its key.
10. **Errors**: the message, the probable cause and what to do, and the stable code, as the guided errors of the web.
11. **Commands**: the command line of `/` with the matching commands.
12. **Settings**: `/config`.

## Visual language

- Rounded borders; the focused panel has the accent border and a bold title, the others the dim border.
- The selected row is reversed; marked rows have the accent color and a `✓` in the first column; folders are bold and end in `/`.
- Sizes right aligned in binary units (`1.4 MiB`), dates as `2026-11-24 18:40` in the time zone of the account.
- Transfers: an arrow for the direction (`↑` upload, `↓` download, `⇄` copy between sites), a line gauge, speed and time left; failed
  ones in the danger color with their code.
- The twelve themes of the web, read from `tui/themes.json`, which `make gen` exports from `frontend/src/lib/theme/themes.ts`, so a
  color changed in the web reaches the terminal and CI catches a stale copy. The theme is the one of the account.
- Colors in truecolor when the terminal says so (`COLORTERM`), the nearest of 256 otherwise; "the terminal's own palette" uses the
  16 colors of the terminal instead; `NO_COLOR` turns colors off, and the marks, the focus and progress stay readable without them
  (`✓`, bold titles, `━` against `─`).

## Out of the first version

The embedded SSH terminal, file preview, archives, editing files, moving between panels (copy then delete), site editing, teams and
administration. They stay in the web; the palette offers to open the web page that does them.
