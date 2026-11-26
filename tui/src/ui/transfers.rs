//! The transfers of the account (docs/design/terminal-client.md, Transfers and log panel): the queue as the event stream tells it
//! (`snapshot`, `transfer.state`, `transfer.progress`), with the speed and the time left of each, drawn as rows with a gauge. Uploads and
//! downloads move their bytes through this client, so it also keeps what it needs to resume them; copies between sites run on the
//! server.

use std::path::PathBuf;
use std::time::Instant;

use ratatui::style::{Modifier, Stylize};
use ratatui::text::{Line, Span};
use serde::Deserialize;
use uuid::Uuid;

use super::explorer::human;
use super::theme::Palette;
use super::widgets::visible_character;

/// Which way a transfer goes.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Direction {
    /// From this computer to a site.
    Upload,
    /// From a site to this computer.
    Download,
    /// From one site to another, on the server.
    Copy,
}

/// Where a transfer is.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Status {
    /// Waiting for its turn.
    Queued,
    /// Moving bytes.
    Running,
    /// Finished.
    Done,
    /// Stopped by an error.
    Failed,
    /// Stopped by its user.
    Canceled,
    /// Cut before the end; an upload of this client can resume.
    Interrupted,
}

impl Status {
    /// Whether the transfer still waits or moves bytes.
    #[must_use]
    pub fn active(self) -> bool {
        matches!(self, Self::Queued | Self::Running)
    }
}

/// A transfer as the stream describes it.
#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Row {
    /// Identifies the transfer.
    pub id: Uuid,
    /// Which way it goes.
    pub direction: Direction,
    /// The name of the file or folder.
    pub name: String,
    /// The site it reads from or writes to.
    #[serde(default)]
    pub site_name: String,
    /// The path on the site.
    #[serde(default)]
    pub path: String,
    /// Where it is.
    pub status: Status,
    /// How many bytes it moves, once known.
    #[serde(default)]
    pub total_bytes: Option<u64>,
    /// How many it moved.
    #[serde(default)]
    pub bytes_done: u64,
    /// The stable code of the error that stopped it.
    #[serde(default)]
    pub error_code: Option<String>,
    /// Whether its user holds it back while it waits.
    #[serde(default)]
    pub paused: bool,
    /// Its place in the queue while it waits, from -1 to 1.
    #[serde(default)]
    pub priority: i8,
    /// Bytes per second, smoothed.
    #[serde(skip)]
    pub speed: f64,
    /// The last progress seen: when and how many bytes, to measure the speed.
    #[serde(skip)]
    last: Option<(Instant, u64)>,
}

impl Row {
    /// A transfer this client starts, shown at once before the stream tells about it.
    #[must_use]
    pub fn started(
        id: Uuid,
        direction: Direction,
        name: &str,
        site_name: &str,
        total: Option<u64>,
    ) -> Self {
        Self {
            id,
            direction,
            name: name.to_owned(),
            site_name: site_name.to_owned(),
            path: String::new(),
            status: Status::Queued,
            total_bytes: total,
            bytes_done: 0,
            error_code: None,
            paused: false,
            priority: 0,
            speed: 0.0,
            last: None,
        }
    }

    /// The share done, from 0 to 1, when the size is known.
    #[must_use]
    pub fn ratio(&self) -> Option<f64> {
        if self.status == Status::Done {
            return Some(1.0);
        }
        #[allow(clippy::cast_precision_loss, reason = "a share for a gauge")]
        self.total_bytes
            .filter(|total| *total > 0)
            .map(|total| (self.bytes_done as f64 / total as f64).min(1.0))
    }

    /// Seconds left at the current speed, when both are known.
    #[must_use]
    pub fn left(&self) -> Option<u64> {
        let total = self.total_bytes?;
        if self.speed <= 0.0 {
            return None;
        }
        #[allow(
            clippy::cast_precision_loss,
            clippy::cast_possible_truncation,
            clippy::cast_sign_loss,
            reason = "seconds for a person to read"
        )]
        Some((total.saturating_sub(self.bytes_done) as f64 / self.speed).ceil() as u64)
    }
}

/// What this client needs to start or resume a transfer that moves bytes through it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Job {
    /// The transfer.
    pub id: Uuid,
    /// Upload or download.
    pub direction: Direction,
    /// The workspace of the site.
    pub workspace: Uuid,
    /// The site.
    pub site: Uuid,
    /// The file on this computer.
    pub local: PathBuf,
    /// The file on the site.
    pub remote: String,
    /// For an upload, `create` or `overwrite`; a resume sends `resume`.
    pub mode: String,
    /// Where to start: 0, or the bytes already moved when resuming.
    pub from: u64,
    /// The panel to list again once it ends: the one it writes to.
    pub side: super::home::Side,
    /// Whether it is a folder, moved with everything in it, a file at a time.
    pub folder: bool,
}

/// The transfers of the account, newest first, with the one under the cursor of the queue.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Queue {
    rows: Vec<Row>,
    jobs: Vec<Job>,
    /// The row under the cursor of the queue.
    pub cursor: usize,
}

impl Queue {
    /// The transfers, newest first.
    #[must_use]
    pub fn rows(&self) -> &[Row] {
        &self.rows
    }

    /// The transfer under the cursor.
    #[must_use]
    pub fn current(&self) -> Option<&Row> {
        self.rows.get(self.cursor)
    }

    /// The job of a transfer this client moves, to resume it.
    #[must_use]
    pub fn job(&self, id: Uuid) -> Option<&Job> {
        self.jobs.iter().find(|job| job.id == id)
    }

    /// Records a transfer this client starts, and its job.
    pub fn start(&mut self, row: Row, job: Option<Job>) {
        self.jobs
            .retain(|known| Some(known.id) != job.as_ref().map(|job| job.id));
        self.jobs.extend(job);
        self.put(row);
    }

    /// The running transfers, as the stream tells them when it connects, newest first; transfers it does not mention keep what they had.
    pub fn snapshot(&mut self, rows: Vec<Row>) {
        for row in rows.into_iter().rev() {
            self.put(row);
        }
    }

    /// A transfer changed: it is added or replaces the one with its id, keeping the speed measured so far.
    pub fn put(&mut self, mut row: Row) {
        if let Some(at) = self.rows.iter().position(|known| known.id == row.id) {
            let known = self.rows.remove(at);
            row.speed = known.speed;
            row.last = known.last;
            row.bytes_done = row.bytes_done.max(if row.status.active() {
                known.bytes_done
            } else {
                0
            });
            self.rows.insert(at, row);
        } else {
            self.rows.insert(0, row);
        }
    }

    /// Bytes moved by a transfer at an instant: the speed is smoothed over the progress seen.
    pub fn progress(&mut self, id: Uuid, done: u64, total: Option<u64>, at: Instant) {
        let Some(row) = self.rows.iter_mut().find(|row| row.id == id) else {
            return;
        };
        if let Some((then, before)) = row.last {
            let seconds = at.saturating_duration_since(then).as_secs_f64();
            if seconds > 0.0 {
                #[allow(clippy::cast_precision_loss, reason = "a speed for a person to read")]
                let now = done.saturating_sub(before) as f64 / seconds;
                row.speed = if row.speed > 0.0 {
                    row.speed * 0.7 + now * 0.3
                } else {
                    now
                };
            }
        }
        row.last = Some((at, done));
        row.bytes_done = done;
        row.status = Status::Running;
        if total.is_some() {
            row.total_bytes = total;
        }
    }

    /// The transfers still waiting or moving bytes.
    #[must_use]
    pub fn active(&self) -> Vec<&Row> {
        self.rows.iter().filter(|row| row.status.active()).collect()
    }

    /// How many transfers are active and their share done together, when their sizes are known.
    #[must_use]
    pub fn summary(&self) -> Option<(usize, Option<u8>)> {
        let active = self.active();
        if active.is_empty() {
            return None;
        }
        let total: u64 = active.iter().filter_map(|row| row.total_bytes).sum();
        let done: u64 = active
            .iter()
            .filter(|row| row.total_bytes.is_some())
            .map(|row| row.bytes_done)
            .sum();
        let percent =
            (total > 0).then(|| u8::try_from((done.min(total) * 100) / total).unwrap_or(100));
        Some((active.len(), percent))
    }
}

/// The arrow of a direction: up for uploads, down for downloads, both ways for copies between sites.
pub(super) fn arrow(direction: Direction) -> &'static str {
    match direction {
        Direction::Upload => "↑",
        Direction::Download => "↓",
        Direction::Copy => "⇄",
    }
}

/// A gauge of a width: `━` for what is done against `─`, so it reads without colors too.
#[must_use]
pub fn gauge(ratio: Option<f64>, width: usize) -> String {
    let full = gauge_fill(ratio, width);
    format!("{}{}", "━".repeat(full), "─".repeat(width - full))
}

/// The number of cells filled in a gauge.
pub(super) fn gauge_fill(ratio: Option<f64>, width: usize) -> usize {
    #[allow(
        clippy::cast_possible_truncation,
        clippy::cast_sign_loss,
        clippy::cast_precision_loss,
        reason = "cells of a gauge"
    )]
    ratio
        .map_or(0, |ratio| (ratio * width as f64).round() as usize)
        .min(width)
}

/// A duration in minutes and seconds, or hours when long.
#[must_use]
pub fn clock(seconds: u64) -> String {
    if seconds >= 3600 {
        format!(
            "{}:{:02}:{:02}",
            seconds / 3600,
            (seconds / 60) % 60,
            seconds % 60
        )
    } else {
        format!("{}:{:02}", seconds / 60, seconds % 60)
    }
}

/// A speed in binary units per second.
pub(super) fn speed_text(speed: f64) -> String {
    #[allow(
        clippy::cast_possible_truncation,
        clippy::cast_sign_loss,
        reason = "bytes per second for a person to read"
    )]
    let bytes = speed as u64;
    format!("{}/s", human(bytes))
}

/// A transfer as a line: the arrow, the name, the gauge, the share, the speed and the time left, or how it ended; failures carry their
/// explanation in the danger color.
#[must_use]
pub fn line(row: &Row, width: usize, palette: &Palette, status: &str) -> Line<'static> {
    let gauge_width = (width / 4).clamp(8, 30);
    let reserved = if row.status == Status::Failed { 58 } else { 40 };
    let name_width = width.saturating_sub(gauge_width + reserved).max(8);
    let name: String = row
        .name
        .chars()
        .take(name_width)
        .map(visible_character)
        .collect();
    let share = row.ratio().map_or_else(
        || "  ? ".to_owned(),
        |ratio| format!("{:>3.0}%", ratio * 100.0),
    );
    let speed = if row.status == Status::Running && row.speed > 0.0 {
        speed_text(row.speed)
    } else {
        String::new()
    };
    let left = row
        .left()
        .filter(|_| row.status == Status::Running)
        .map_or_default(clock);
    let tail = match row.status {
        Status::Running if !speed.is_empty() => format!("{speed:>11} {left:>7}"),
        _ => status.to_owned(),
    };
    let mut spans = vec![
        Span::raw(format!("{} ", arrow(row.direction))).bold(),
        Span::raw(format!("{name:<name_width$} ")),
        Span::raw(gauge(row.ratio(), gauge_width)).fg(palette.accent),
        Span::raw(format!(" {share} ")),
        Span::raw(format!("{tail:<20}")),
        Span::raw(format!(" {}", row.site_name)).fg(palette.muted),
    ];
    if row.status == Status::Failed {
        spans[4] = Span::raw(format!("{tail:<20}"))
            .fg(palette.danger)
            .add_modifier(Modifier::BOLD);
    }
    if row.status == Status::Done {
        spans[2] = Span::raw(gauge(Some(1.0), gauge_width)).fg(palette.ok);
    }
    Line::from(spans)
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use super::*;
    use crate::ui::theme::{ColorMode, Palette, find};

    /// A transfer of the stream, as JSON.
    fn row(status: &str, done: u64) -> Row {
        serde_json::from_value(serde_json::json!({"id": Uuid::nil(), "direction": "upload", "name": "a.bin", "siteName": "nas", "status": status, "totalBytes": 1000, "bytesDone": done}))
            .expect("a transfer")
    }

    /// The queue follows the stream: new transfers go first, changes keep the speed measured, progress measures it smoothed, and the
    /// summary adds up the active ones.
    #[test]
    fn follows_the_stream() {
        let mut queue = Queue::default();
        assert_eq!(queue.summary(), None);
        let other = Row::started(Uuid::from_u128(2), Direction::Download, "b", "nas", None);
        queue.start(other, None);
        queue.snapshot(vec![row("running", 100)]);
        assert_eq!(queue.rows()[0].id, Uuid::nil());
        let start = Instant::now();
        queue.progress(Uuid::nil(), 200, None, start);
        queue.progress(Uuid::nil(), 400, Some(1000), start + Duration::from_secs(1));
        queue.progress(Uuid::nil(), 600, None, start + Duration::from_secs(2));
        queue.progress(Uuid::nil(), 600, None, start + Duration::from_secs(2));
        queue.progress(Uuid::from_u128(9), 1, None, start);
        let first = &queue.rows()[0];
        assert!((first.speed - 200.0).abs() < 1.0, "{}", first.speed);
        assert_eq!((first.ratio(), first.left()), (Some(0.6), Some(2)));
        queue.put(row("running", 500));
        assert_eq!(
            queue.rows()[0].bytes_done,
            600,
            "an older count does not go back"
        );
        assert_eq!(queue.summary(), Some((2, Some(60))));
        queue.put(row("done", 1000));
        assert_eq!(queue.rows()[0].bytes_done, 1000);
        assert_eq!(queue.active().len(), 1);
        assert_eq!(queue.summary(), Some((1, None)));
        assert_eq!(queue.rows()[1].left(), None);
        assert_eq!(
            Row::started(Uuid::nil(), Direction::Copy, "c", "s", Some(0)).ratio(),
            None
        );
        let job = Job {
            id: Uuid::nil(),
            direction: Direction::Upload,
            workspace: Uuid::nil(),
            site: Uuid::nil(),
            local: "a".into(),
            remote: "/a".into(),
            mode: "create".into(),
            from: 0,
            side: crate::ui::home::Side::Left,
            folder: false,
        };
        queue.start(row("queued", 0), Some(job.clone()));
        queue.start(row("queued", 0), Some(job.clone()));
        assert_eq!(queue.job(Uuid::nil()), Some(&job));
        assert_eq!(queue.current().map(|row| row.id), Some(Uuid::nil()));
    }

    /// Gauges fill with `━` against `─`, durations read as minutes or hours, and each status draws its own tail.
    #[test]
    fn draws_transfers() {
        assert_eq!(gauge(Some(0.5), 4), "━━──");
        assert_eq!(gauge(None, 3), "───");
        assert_eq!(
            (clock(75), clock(3725)),
            ("1:15".to_owned(), "1:02:05".to_owned())
        );
        let palette = Palette::new(&find("nord"), ColorMode::TrueColor);
        let text = |row: &Row| line(row, 100, &palette, "waiting").to_string();
        let mut running = row("running", 500);
        running.speed = 100.0;
        assert!(
            text(&running).contains("50%")
                && text(&running).contains("100 B/s")
                && text(&running).contains("0:05"),
            "{}",
            text(&running)
        );
        assert!(text(&row("failed", 10)).contains("waiting"));
        assert!(text(&row("done", 1000)).contains("100%"));
        assert!(text(&row("queued", 0)).contains("waiting"));
        let copy = Row::started(Uuid::nil(), Direction::Copy, "c", "s", None);
        assert!(text(&copy).contains("⇄") && text(&copy).contains('?'));
        assert!(
            text(&Row::started(
                Uuid::nil(),
                Direction::Download,
                "d",
                "s",
                None
            ))
            .contains("↓")
        );
        assert!(!Status::Done.active() && Status::Running.active());
    }
}
