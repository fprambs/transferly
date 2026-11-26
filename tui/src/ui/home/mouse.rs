//! Routes mouse input to visible panels and overlays using their drawing areas.

use ratatui::layout::Position;

use super::{
    Ask, Focus, Home, KeyCode, Look, Overlay, Rect, SETTINGS, Side, Span, Tab, choices_area,
    commands_area, host_key_area, panel, settings_area,
};
use crate::ui::mouse::{MouseKind, first_row, footer_key, key, label_key, row};
use crate::ui::widgets::popup_inside;

impl Home {
    /// Handles a pointer action and reports whether it selected a row that can open.
    pub(in crate::ui) fn mouse(
        &mut self,
        area: Rect,
        position: Position,
        kind: MouseKind,
        double: bool,
        look: &mut Look,
    ) -> (Vec<Ask>, bool) {
        if area.width < 60 || area.height < 16 || kind == MouseKind::Right {
            return (Vec::new(), false);
        }
        let [_, sites, left, right, bottom, status] = self.layout(area);
        if let Some(overlay) = self.overlay.as_ref() {
            return (
                self.overlay_mouse(area, status, position, kind, overlay.clone(), look),
                false,
            );
        }
        if let Some(side) = self
            .focused_side()
            .filter(|side| self.panel(*side).captures())
        {
            let panel_area = if side == Side::Left { left } else { right };
            let action = (kind == MouseKind::Left)
                .then(|| self.panel(side).confirmation_at(panel_area, position, look))
                .flatten();
            let asks = action.map_or_else(Vec::new, |key| self.explorer_key(side, key, look));
            return (asks, false);
        }
        if kind == MouseKind::Left && status.contains(position) {
            return (self.status_mouse(status, position, look), false);
        }
        for (target, focus) in [
            (sites, Focus::Sites),
            (left, Focus::Left),
            (right, Focus::Right),
            (bottom, Focus::Bottom),
        ] {
            if target.contains(position) {
                return self.panel_mouse(target, position, kind, double, focus, look);
            }
        }
        (Vec::new(), false)
    }

    /// Runs a visible shortcut on the status bar.
    fn status_mouse(&mut self, area: Rect, position: Position, look: &mut Look) -> Vec<Ask> {
        let used = self.status_spans(look).iter().map(Span::width).sum();
        let ([_, keys_area], labels) = self.status_layout(area, look, used);
        if !keys_area.contains(position) {
            return Vec::new();
        }
        let codes: &[_] = match self.focus {
            Focus::Sites => &[
                KeyCode::Enter,
                KeyCode::Char('w'),
                KeyCode::Char('/'),
                KeyCode::F(1),
                KeyCode::Char('q'),
            ],
            Focus::Left | Focus::Right => &[
                KeyCode::Enter,
                KeyCode::Char(' '),
                KeyCode::Char('n'),
                KeyCode::F(2),
                KeyCode::Delete,
                KeyCode::Char('f'),
                KeyCode::Char('o'),
                KeyCode::F(1),
            ],
            Focus::Bottom => &[
                KeyCode::Char('p'),
                KeyCode::Char('x'),
                KeyCode::Char('R'),
                if self.tab == Tab::Log {
                    KeyCode::Char('[')
                } else {
                    KeyCode::Char(']')
                },
                KeyCode::F(1),
            ],
        };
        label_key(position.x, usize::from(keys_area.x), &labels, codes)
            .map_or_else(Vec::new, |action| self.key(action, look))
    }

    /// Focuses the visible panel and applies a wheel step or selects the pointed row.
    fn panel_mouse(
        &mut self,
        area: Rect,
        position: Position,
        kind: MouseKind,
        double: bool,
        focus: Focus,
        look: &mut Look,
    ) -> (Vec<Ask>, bool) {
        self.focus = focus;
        let side = self.focused_side();
        if let Some(side) = side {
            self.last = side;
        }
        if let Some(action) = kind.wheel() {
            return (self.key(action, look), false);
        }
        let inside = panel("", false, &look.palette).inner(area);
        let selected = match focus {
            Focus::Sites => {
                let hit = row(
                    inside,
                    position,
                    first_row(inside, self.cursor),
                    self.rows().len(),
                );
                if let Some(index) = hit.filter(|_| !self.loading) {
                    self.cursor = index;
                    true
                } else {
                    false
                }
            }
            Focus::Left => self.left.select_at(area, position, look),
            Focus::Right => self.right.select_at(area, position, look),
            Focus::Bottom => {
                if self.tab == Tab::Transfers
                    && let Some(index) = row(
                        inside,
                        position,
                        first_row(inside, self.queue.cursor),
                        self.queue.rows().len(),
                    )
                {
                    self.queue.cursor = index;
                }
                false
            }
        };
        let asks = if selected && double {
            self.key(key(KeyCode::Enter), look)
        } else {
            Vec::new()
        };
        (asks, selected)
    }

    /// Routes clicks to the list or confirmation currently covering the panels.
    fn overlay_mouse(
        &mut self,
        area: Rect,
        status: Rect,
        position: Position,
        kind: MouseKind,
        overlay: Overlay,
        look: &mut Look,
    ) -> Vec<Ask> {
        if let Some(action) = kind.wheel() {
            return match overlay {
                Overlay::Transfers => {
                    self.scroll_transfers(action.code);
                    Vec::new()
                }
                Overlay::Commands { .. }
                | Overlay::Choose { .. }
                | Overlay::Settings { .. }
                | Overlay::Help { .. } => self.key(action, look),
                _ => Vec::new(),
            };
        }
        let hit =
            |bounds, first, count| row(popup_inside(bounds, &look.palette), position, first, count);
        let action = match overlay {
            Overlay::Commands { input, .. } => {
                let matches = Self::matches(&input);
                if let Some(index) = hit(commands_area(status, matches.len()), 0, matches.len()) {
                    self.overlay = None;
                    return self.commands_key(key(KeyCode::Enter), input, index, look);
                }
                return Vec::new();
            }
            Overlay::Choose {
                what,
                selected,
                worn,
            } => {
                let count = self.choices(what, look).0.len();
                let bounds = choices_area(area, count);
                let first = first_row(popup_inside(bounds, &look.palette), selected);
                return hit(bounds, first, count).map_or_else(Vec::new, |index| {
                    self.overlay = None;
                    self.choose_key(key(KeyCode::Enter), what, index, worn, look)
                });
            }
            Overlay::Settings { .. } => {
                return hit(settings_area(area), 0, SETTINGS.len())
                    .map_or_else(Vec::new, |index| self.key_setting(index, look));
            }
            Overlay::Transfers => {
                let bounds = self.transfers_area(area);
                let first = first_row(popup_inside(bounds, &look.palette), self.queue.cursor);
                if let Some(index) = hit(bounds, first, self.queue.rows().len()) {
                    self.queue.cursor = index;
                }
                None
            }
            Overlay::Copy { items, .. } => footer_key(
                Self::copy_area(area, items.len()),
                position,
                &look.messages.get("copy.keys"),
                &[KeyCode::Enter, KeyCode::Right, KeyCode::Esc],
            ),
            Overlay::Cancel(_) => footer_key(
                Self::cancel_area(area),
                position,
                &look.messages.get("explorer.deleteKeys"),
                &[KeyCode::Enter, KeyCode::Esc],
            ),
            Overlay::HostKey { trusted, .. } => {
                let (labels, codes): (_, &[_]) = if trusted.is_some() {
                    (
                        "hostKey.changedKeys",
                        &[KeyCode::Char('a'), KeyCode::Char('r'), KeyCode::Esc],
                    )
                } else {
                    ("hostKey.keys", &[KeyCode::Enter, KeyCode::Esc])
                };
                footer_key(
                    host_key_area(area, trusted.as_deref()),
                    position,
                    &look.messages.get(labels),
                    codes,
                )
            }
            _ => None,
        };
        action.map_or_else(Vec::new, |key| self.key(key, look))
    }

    /// Activates the clicked setting using the same action as Enter.
    fn key_setting(&mut self, selected: usize, look: &mut Look) -> Vec<Ask> {
        self.overlay = Some(Overlay::Settings { selected });
        self.key(key(KeyCode::Enter), look)
    }
}
