//! Styled controls with shared rendering and mouse hit testing.

use crossterm::event::KeyCode;
use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::Stylize;
use ratatui::text::{Line, Span};

use super::look::Look;

/// A row of controls whose text and hit targets use the same terminal cell widths.
#[derive(Default)]
pub(super) struct Bar {
    items: Vec<(Option<KeyCode>, Line<'static>)>,
}

impl Bar {
    /// Appends a control and its key.
    pub(super) fn push(&mut self, code: KeyCode, spans: Vec<Span<'static>>) {
        self.items.push((Some(code), Line::from(spans)));
    }

    /// Appends a separator that has no mouse action.
    pub(super) fn separate(&mut self, text: &'static str, look: &Look) {
        self.items
            .push((None, Line::from(Span::raw(text).fg(look.palette.faint))));
    }

    /// The number of terminal cells occupied by the controls and separators.
    pub(super) fn width(&self) -> usize {
        self.items.iter().map(|(_, item)| item.width()).sum()
    }

    /// Paints a value with the theme's active or raised background.
    pub(super) fn chip(text: String, active: bool, look: &Look) -> Span<'static> {
        let palette = &look.palette;
        let span = Span::raw(text);
        if active {
            let span = span.fg(palette.ink).bg(palette.accent);
            if palette.plain { span.bold() } else { span }
        } else {
            span.fg(palette.text).bg(palette.raised)
        }
    }

    /// Appends a setting name and its value chip.
    pub(super) fn setting(
        &mut self,
        code: char,
        label: &str,
        value: &str,
        active: bool,
        look: &Look,
    ) {
        self.push(
            KeyCode::Char(code),
            vec![
                Span::raw(format!("{label} ")).fg(look.palette.muted),
                Self::chip(value.to_owned(), active, look),
            ],
        );
    }

    /// Builds a shortcut row from localized items, stopping at the first item that does not fit.
    pub(super) fn shortcuts(
        labels: &str,
        codes: &[KeyCode],
        mut width: usize,
        look: &Look,
    ) -> Self {
        let mut bar = Self::default();
        let separator = " │ ";
        for (item, &code) in labels.split(" · ").zip(codes) {
            let split = if item.starts_with("[ ] ") {
                "[ ]".len()
            } else {
                item.find(' ').unwrap_or(item.len())
            };
            let (label, action) = item.split_at(split);
            let action = action.trim_start();
            let spans = vec![
                Span::raw(format!("{label} "))
                    .fg(look.palette.ink)
                    .bg(look.palette.accent)
                    .bold(),
                Span::raw(format!(" {action}")).fg(look.palette.text),
            ];
            let separated = !bar.items.is_empty();
            let separator_width = usize::from(separated) * Span::raw(separator).width();
            let used = spans.iter().map(Span::width).sum::<usize>() + separator_width;
            if used > width {
                break;
            }
            width -= used;
            if separated {
                bar.separate(separator, look);
            }
            bar.push(code, spans);
        }
        bar
    }

    /// Draws the controls in order on the bar background.
    pub(super) fn render(self, frame: &mut Frame, area: Rect, look: &Look) {
        let spans: Vec<_> = self
            .items
            .into_iter()
            .flat_map(|(_, item)| item.spans)
            .collect();
        frame.render_widget(Line::from(spans).bg(look.palette.surface), area);
    }

    /// Looks up the key at a column relative to the start of the visible row.
    pub(super) fn key_at(&self, column: u16) -> Option<KeyCode> {
        let mut offset = usize::from(column);
        for (code, item) in &self.items {
            let width = item.width();
            if offset < width {
                return *code;
            }
            offset -= width;
        }
        None
    }
}
