//! The transfers of the main screen: `F5` and `c` copy what a panel selects to the other one after a confirmation, uploads and
//! downloads move their bytes through this client and copies between sites run on the server, and the queue shows in a window in front
//! while they run and in the bottom panel, where they are paused, resumed, cancelled and reordered.

use crossterm::event::{KeyCode, KeyEvent};
use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::Stylize;
use ratatui::text::Line;
use ratatui::widgets::{Paragraph, Wrap};
use uuid::Uuid;

use super::super::explorer::{Entry, Kind, Source};
use super::super::look::Look;
use super::super::transfers::{Direction, Job, Row, Status, line};
use super::super::widgets::{centered, error_line, highlight, panel, popup};
use super::{Ask, Call, CallError, Focus, Home, Overlay, POLICIES, Side, Tab, explain};

/// The other panel.
fn other(side: Side) -> Side {
    match side {
        Side::Left => Side::Right,
        Side::Right => Side::Left,
    }
}

/// A name that is free in a folder: the name, or `name (1).ext`, `name (2).ext`… as the server names copies it keeps both of.
fn free_name(name: &str, taken: &[Entry]) -> String {
    let (stem, ext) = match name.rfind('.').filter(|at| *at > 0) {
        Some(at) => (&name[..at], &name[at..]),
        None => (name, ""),
    };
    (1..u32::MAX)
        .map(|n| format!("{stem} ({n}){ext}"))
        .find(|candidate| taken.iter().all(|entry| &entry.name != candidate))
        .unwrap_or_default()
}

/// What to do with an entry given what exists at the destination: the name to write and whether to replace, or nothing to skip it.
fn plan(entry: &Entry, existing: &[Entry], policy: &str) -> Option<(String, bool)> {
    let found = existing.iter().find(|known| known.name == entry.name);
    match (found, policy) {
        (None, _) | (Some(_), "fail") => Some((entry.name.clone(), false)),
        (Some(_), "overwrite") => Some((entry.name.clone(), true)),
        (Some(known), "newer") if entry.modified > known.modified => {
            Some((entry.name.clone(), true))
        }
        (Some(_), "rename") => Some((free_name(&entry.name, existing), false)),
        _ => None,
    }
}

impl Home {
    /// `F5` or `c` on a panel: asks to copy its selection to the other panel, when both show something and it is not the local disk on
    /// both sides.
    pub(super) fn ask_copy(&mut self, side: Side, look: &Look) {
        let items = self.panel(side).selection();
        let (from, to) = (self.panel(side).source(), self.panel(other(side)).source());
        let reason = match (from, to) {
            _ if items.is_empty() => Some("copy.nothing"),
            (_, None) | (None, _) => Some("copy.noTarget"),
            (Some(Source::Local), Some(Source::Local)) => Some("copy.localToLocal"),
            _ => None,
        };
        if let Some(reason) = reason {
            self.say(look.messages.get(reason), true);
            return;
        }
        self.overlay = Some(Overlay::Copy {
            from: side,
            items,
            policy: 0,
        });
    }

    /// Handles a key on the confirmation of a copy: ← → change the policy, Enter copies, Esc gives up.
    pub(super) fn copy_key(
        &mut self,
        key: KeyEvent,
        from: Side,
        items: Vec<Entry>,
        mut policy: usize,
        look: &Look,
    ) -> Vec<Ask> {
        match key.code {
            KeyCode::Esc | KeyCode::Char('n') => return Vec::new(),
            KeyCode::Enter | KeyCode::Char('y') => {
                return self.copy(from, &items, POLICIES[policy], look);
            }
            KeyCode::Right | KeyCode::Char('l') => policy = (policy + 1) % POLICIES.len(),
            KeyCode::Left | KeyCode::Char('h') => {
                policy = (policy + POLICIES.len() - 1) % POLICIES.len();
            }
            _ => {}
        }
        self.overlay = Some(Overlay::Copy {
            from,
            items,
            policy,
        });
        Vec::new()
    }

    /// Starts copying entries from a panel to the other: uploads and downloads of files through this client, copies between sites on the
    /// server, folders included; folders between this computer and a site come with the next step and are said to be left out.
    fn copy(&mut self, from: Side, items: &[Entry], policy: &str, look: &Look) -> Vec<Ask> {
        let to = other(from);
        #[rustfmt::skip]
        let (Some(source), Some(target), Some(workspace)) = (self.panel(from).source().cloned(), self.panel(to).source().cloned(), self.workspace) else { return Vec::new() };
        #[rustfmt::skip]
        let site_name = |source: &Source| if let Source::Site { name, .. } = source { name.clone() } else { String::new() };
        let mut asks = Vec::new();
        let mut skipped = 0;
        for entry in items {
            let Some((name, replace)) = plan(entry, self.panel(to).entries(), policy) else {
                skipped += 1;
                continue;
            };
            let id = Uuid::new_v4();
            let (here, there) = (
                self.panel(from).join(&entry.name),
                self.panel(to).join(&name),
            );
            let mode = if replace { "overwrite" } else { "create" }.to_owned();
            let folder = entry.kind == Kind::Folder;
            let (direction, site, local, remote, shown) = match (source.site(), target.site()) {
                (Some(source_site), Some(target_site)) => {
                    self.queue.start(
                        Row::started(id, Direction::Copy, &entry.name, &site_name(&target), None),
                        None,
                    );
                    let (workspace, policy) = (workspace, policy.to_owned());
                    asks.push(Ask::Call(Call::Copy {
                        workspace,
                        source: source_site,
                        from: here,
                        target: target_site,
                        to: there,
                        policy,
                        id,
                        side: to,
                    }));
                    continue;
                }
                (None, Some(site)) => (Direction::Upload, site, here, there, &target),
                (site, _) => (
                    Direction::Download,
                    site.unwrap_or_default(),
                    there,
                    here,
                    &source,
                ),
            };
            let job = Job {
                id,
                direction,
                workspace,
                site,
                local: local.into(),
                remote,
                mode,
                from: 0,
                side: to,
                folder,
            };
            self.queue.start(
                Row::started(
                    id,
                    direction,
                    &if folder {
                        format!("{name}/")
                    } else {
                        name.clone()
                    },
                    &site_name(shown),
                    (!folder).then_some(entry.size),
                ),
                Some(job.clone()),
            );
            asks.push(Ask::Transfer(job, to));
        }
        if skipped > 0 {
            self.say(
                look.messages
                    .fill("copy.skipped", &[("count", &skipped.to_string())]),
                false,
            );
        }
        if !asks.is_empty() {
            self.panel_mut(from).clear_marks();
            self.overlay = Some(Overlay::Transfers);
        }
        asks
    }

    /// A transfer of this client ended: its panel is listed again, or the queue marks it cut or failed and the status bar says why.
    pub(super) fn transfer_ended(
        &mut self,
        id: Uuid,
        side: Side,
        outcome: Result<(), CallError>,
        look: &Look,
    ) -> Vec<Ask> {
        let Err(error) = outcome else {
            let asks = self.panel_mut(side).retry();
            return self.panel_asks(side, asks);
        };
        if let Some(mut row) = self
            .queue
            .rows()
            .iter()
            .find(|row| row.id == id && row.status.active())
            .cloned()
        {
            row.status = if error.code.is_none() {
                Status::Interrupted
            } else {
                Status::Failed
            };
            row.error_code.clone_from(&error.code);
            self.queue.put(row);
        }
        self.say(explain(&error, look), true);
        Vec::new()
    }

    /// Handles a key on the bottom panel: `[` and `]` switch tabs; on the queue the arrows move, `p` pauses or resumes, `x` and Delete
    /// cancel after asking, `R` resumes a transfer of this client that was cut, `+` and `-` change the priority of one that waits.
    pub(super) fn bottom_key(&mut self, key: KeyEvent) -> Vec<Ask> {
        match key.code {
            KeyCode::Char('[') => self.tab = Tab::Transfers,
            KeyCode::Char(']') => self.tab = Tab::Log,
            _ if self.tab == Tab::Log => {}
            KeyCode::Down | KeyCode::Char('j') => {
                self.queue.cursor =
                    (self.queue.cursor + 1).min(self.queue.rows().len().saturating_sub(1));
            }
            KeyCode::Up | KeyCode::Char('k') => {
                self.queue.cursor = self.queue.cursor.saturating_sub(1);
            }
            _ => return self.queue_key(key),
        }
        Vec::new()
    }

    /// Handles a key on the transfer under the cursor of the queue.
    fn queue_key(&mut self, key: KeyEvent) -> Vec<Ask> {
        let Some(row) = self.queue.current().cloned() else {
            return Vec::new();
        };
        let job = self.queue.job(row.id).cloned();
        let action = |action: &str| {
            vec![Ask::Call(Call::TransferAction {
                id: row.id,
                action: action.to_owned(),
            })]
        };
        match key.code {
            KeyCode::Char('x') | KeyCode::Delete if row.status.active() => {
                self.overlay = Some(Overlay::Cancel(row.id));
            }
            KeyCode::Char('p')
                if row.direction == Direction::Copy || row.status == Status::Queued =>
            {
                return action(if row.paused { "resume" } else { "pause" });
            }
            KeyCode::Char('p') if row.status == Status::Running => return vec![Ask::Abort(row.id)],
            KeyCode::Char('p' | 'R')
                if matches!(row.status, Status::Interrupted | Status::Failed) =>
            {
                return self.resume(row, job);
            }
            KeyCode::Char('+') if row.status == Status::Queued => {
                return vec![Ask::Call(Call::Priority {
                    id: row.id,
                    priority: (row.priority + 1).min(1),
                })];
            }
            KeyCode::Char('-') if row.status == Status::Queued => {
                return vec![Ask::Call(Call::Priority {
                    id: row.id,
                    priority: (row.priority - 1).max(-1),
                })];
            }
            _ => {}
        }
        Vec::new()
    }

    /// Resumes a transfer of this client that was cut: an upload continues where the server staged it, a download where its partial
    /// file ends; one this client did not start is resumed by the server.
    fn resume(&mut self, mut row: Row, job: Option<Job>) -> Vec<Ask> {
        let Some(mut job) = job else {
            return vec![Ask::Call(Call::TransferAction {
                id: row.id,
                action: "resume".to_owned(),
            })];
        };
        (if job.folder { "overwrite" } else { "resume" }).clone_into(&mut job.mode);
        job.from = if job.direction == Direction::Upload {
            row.bytes_done
        } else {
            0
        };
        row.status = Status::Queued;
        row.error_code = None;
        self.queue.start(row, Some(job.clone()));
        let side = job.side;
        vec![Ask::Transfer(job, side)]
    }

    /// Handles a key on the question whether to cancel a transfer.
    pub(super) fn cancel_key(&mut self, key: KeyEvent, id: Uuid) -> Vec<Ask> {
        match key.code {
            KeyCode::Char('y') | KeyCode::Enter => vec![
                Ask::Abort(id),
                Ask::Call(Call::TransferAction {
                    id,
                    action: "cancel".to_owned(),
                }),
            ],
            KeyCode::Char('n') | KeyCode::Esc => Vec::new(),
            _ => {
                self.overlay = Some(Overlay::Cancel(id));
                Vec::new()
            }
        }
    }

    /// What the status bar says of the transfers: how many run and their share done together.
    pub(super) fn transfer_summary(&self) -> Option<String> {
        let (count, percent) = self.queue.summary()?;
        Some(percent.map_or_else(
            || format!("⇅ {count}"),
            |percent| format!("⇅ {count} · {percent}%"),
        ))
    }

    /// The confirmation of a copy: what goes, from where to where, and the policy for what exists.
    pub(super) fn render_copy(
        &self,
        frame: &mut Frame,
        from: Side,
        items: &[Entry],
        policy: usize,
        look: &Look,
    ) {
        let (m, palette) = (&look.messages, &look.palette);
        let shown = items.len().min(6);
        let area = centered(frame.area(), 76, u16::try_from(shown).unwrap_or(6) + 9);
        let inside = popup(
            frame,
            area,
            &m.get("copy.title"),
            &m.get("copy.keys"),
            palette,
        );
        let target = self.panel(other(from));
        let mut lines = vec![Line::from(m.fill(
            "copy.body",
            &[
                ("count", &items.len().to_string()),
                ("target", target.path()),
            ],
        ))];
        for entry in items.iter().take(shown) {
            let suffix = if entry.kind == Kind::Folder { "/" } else { "" };
            lines.push(Line::from(format!("  {}{suffix}", entry.name)).bold());
        }
        if items.len() > shown {
            lines.push(
                Line::from(m.fill(
                    "explorer.more",
                    &[("count", &(items.len() - shown).to_string())],
                ))
                .fg(palette.muted),
            );
        }
        lines.push(Line::default());
        let value = m.get(&format!("copy.policy.{}", POLICIES[policy]));
        lines.push(
            Line::from(format!("{}  ‹ {value} ›", m.get("copy.exists"))).fg(palette.accent_text),
        );
        frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: false }), inside);
    }

    /// The window of transfers in front of the screen: the newest transfers with their gauges.
    pub(super) fn render_transfers(&self, frame: &mut Frame, look: &Look) {
        let (m, palette) = (&look.messages, &look.palette);
        let rows = self.queue.rows();
        let shown = rows.len().clamp(1, 8);
        let width = frame.area().width.saturating_sub(8).min(110);
        let area = centered(frame.area(), width, u16::try_from(shown).unwrap_or(8) + 2);
        let title = m.fill(
            "transfers.title",
            &[("count", &self.queue.active().len().to_string())],
        );
        let inside = popup(frame, area, &title, &m.get("transfers.keys"), palette);
        if rows.is_empty() {
            frame.render_widget(
                Paragraph::new(Line::from(m.get("transfers.empty")).fg(palette.muted)),
                inside,
            );
            return;
        }
        let lines: Vec<Line> = rows
            .iter()
            .take(shown)
            .map(|row| {
                line(
                    row,
                    usize::from(inside.width),
                    palette,
                    &status_text(row, look),
                )
            })
            .collect();
        frame.render_widget(Paragraph::new(lines), inside);
    }

    /// The bottom panel: the queue of transfers or the log of connections, by its tab.
    pub(super) fn render_bottom(
        &self,
        frame: &mut Frame,
        area: Rect,
        look: &Look,
        zone: &jiff::tz::TimeZone,
    ) {
        if self.tab == Tab::Log {
            super::render_log(
                frame,
                area,
                &self.log,
                &self.sites,
                zone,
                look,
                self.focus == Focus::Bottom,
            );
            return;
        }
        let (m, palette) = (&look.messages, &look.palette);
        let block = panel(
            &m.get("transfers.tabs"),
            self.focus == Focus::Bottom,
            palette,
        )
        .title_bottom(Line::from(format!(" {} ", m.get("transfers.queueKeys"))).right_aligned());
        let inside = block.inner(area);
        frame.render_widget(block, area);
        let rows = self.queue.rows();
        if rows.is_empty() {
            frame.render_widget(
                Paragraph::new(Line::from(m.get("transfers.empty")).fg(palette.muted)),
                inside,
            );
            return;
        }
        let height = usize::from(inside.height.max(1));
        let skip = self.queue.cursor.saturating_sub(height - 1);
        let lines: Vec<Line> = rows
            .iter()
            .enumerate()
            .skip(skip)
            .take(height)
            .map(|(at, row)| {
                let text = line(
                    row,
                    usize::from(inside.width),
                    palette,
                    &status_text(row, look),
                );
                if at == self.queue.cursor && self.focus == Focus::Bottom {
                    highlight(text, palette)
                } else {
                    text
                }
            })
            .collect();
        frame.render_widget(Paragraph::new(lines), inside);
    }

    /// The question whether to cancel a transfer.
    pub(super) fn render_cancel(&self, frame: &mut Frame, id: Uuid, look: &Look) {
        let (m, palette) = (&look.messages, &look.palette);
        let name = self
            .queue
            .rows()
            .iter()
            .find(|row| row.id == id)
            .map_or("", |row| row.name.as_str());
        let area = centered(frame.area(), 60, 6);
        let inside = popup(
            frame,
            area,
            &m.get("transfers.cancelTitle"),
            &m.get("explorer.deleteKeys"),
            palette,
        );
        frame.render_widget(
            Paragraph::new(vec![error_line(
                &m.fill("transfers.cancelBody", &[("name", name)]),
                palette,
            )])
            .wrap(Wrap { trim: true }),
            inside,
        );
    }
}

/// How a transfer stands, in words: waiting, held back, done, cancelled or cut.
fn status_text(row: &Row, look: &Look) -> String {
    let key = match row.status {
        Status::Queued if row.paused => "transfers.paused",
        Status::Queued => "transfers.queued",
        Status::Running => "transfers.running",
        Status::Done => "transfers.done",
        Status::Failed => "transfers.failed",
        Status::Canceled => "transfers.canceled",
        Status::Interrupted => "transfers.interrupted",
    };
    look.messages.get(key)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A file named like one that exists gets the first free name, before its extension when it has one.
    #[test]
    fn plans_names() {
        let entry = |name: &str| Entry {
            name: name.to_owned(),
            kind: Kind::File,
            size: 1,
            modified: None,
        };
        let existing = vec![entry("README"), entry("README (1)"), entry("a.txt")];
        assert_eq!(free_name("README", &existing), "README (2)");
        assert_eq!(free_name(".env", &existing), ".env (1)");
        assert_eq!(
            plan(&entry("a.txt"), &existing, "overwrite"),
            Some(("a.txt".to_owned(), true))
        );
        assert_eq!(
            plan(&entry("a.txt"), &existing, "fail"),
            Some(("a.txt".to_owned(), false))
        );
        assert_eq!(plan(&entry("a.txt"), &existing, "skip"), None);
        assert_eq!(plan(&entry("a.txt"), &existing, "newer"), None);
        assert_eq!(
            plan(&entry("new"), &existing, "skip"),
            Some(("new".to_owned(), false))
        );
    }
}
