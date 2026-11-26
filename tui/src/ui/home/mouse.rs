//! Routes mouse input to visible panels and overlays using their drawing areas.

use ratatui::layout::Position;

use super::{
    Ask, Focus, Home, KeyCode, Look, Overlay, Rect, SETTINGS, Side, Tab, choices_area,
    commands_area, commands_list, host_key_area, panel, settings_area,
};
use crate::ui::mouse::{MouseKind, first_row, footer_key, key, row};
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
        if self.preview.is_some() {
            let asks = kind
                .wheel()
                .map_or_else(Vec::new, |key| self.preview_key(key));
            return (asks, false);
        }
        if area.width < 60 || area.height < 16 {
            return (Vec::new(), false);
        }
        if matches!(self.overlay, Some(Overlay::ContextMenu { .. })) {
            let asks = if matches!(kind, MouseKind::Left | MouseKind::Right) {
                self.context_menu_click(area, position, look)
            } else {
                Vec::new()
            };
            return (asks, false);
        }
        let [_, sites, left, right, bottom, _, keys] = self.layout(area);
        if let Some(overlay) = self.overlay.as_ref() {
            if kind == MouseKind::Right {
                return (Vec::new(), false);
            }
            return (
                self.overlay_mouse(area, position, kind, overlay.clone(), look),
                false,
            );
        }
        if self.site_view.is_some() {
            return self.site_view_mouse(area, position, kind, double, look);
        }
        if kind == MouseKind::Left && keys.contains(position) {
            let action = self.shortcut_bar(keys, look).key_at(position.x - keys.x);
            return (
                action.map_or_else(Vec::new, |code| self.key(key(code), look)),
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
        for (panel, side) in [(left, Side::Left), (right, Side::Right)] {
            let (bar, target) = self.panel(side).settings_bar(panel, look);
            if kind == MouseKind::Left
                && target.contains(position)
                && let Some(code) = bar.key_at(position.x - target.x)
            {
                self.focus = Self::focus_of(side);
                self.last = side;
                return (self.key(key(code), look), false);
            }
        }
        for (target, focus) in [
            (sites, Focus::Sites),
            (left, Focus::Left),
            (right, Focus::Right),
            (bottom, Focus::Bottom),
        ] {
            if target.contains(position)
                && (kind != MouseKind::Right || matches!(focus, Focus::Left | Focus::Right))
            {
                return self.panel_mouse(target, position, kind, double, focus, look);
            }
        }
        (Vec::new(), false)
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
        if kind == MouseKind::Right {
            if selected {
                self.open_context_menu(self.last);
            }
            return (Vec::new(), false);
        }
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
                let inside = popup_inside(commands_area(area, matches.len()), &look.palette);
                if let Some(index) = row(commands_list(inside), position, 0, matches.len()) {
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
            Overlay::Cancel { .. } => footer_key(
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
