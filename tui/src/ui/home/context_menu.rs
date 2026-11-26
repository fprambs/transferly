//! Draws the explorer's context menu and sends its actions through the explorer shortcuts.

use ratatui::Frame;
use ratatui::layout::{Constraint, Layout, Position, Rect};
use ratatui::style::{Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, BorderType, Clear};

use super::transfers::other;
use super::{Ask, Home, KeyCode, Look, Overlay, Side, explorer_areas};
use crate::ui::mouse::{key, row};
use crate::ui::widgets::{error_line, highlight};

/// The labels and shortcuts of the menu, in order.
const ACTIONS: [(&str, &str, KeyCode); 8] = [
    ("menu.open", "Enter", KeyCode::Enter),
    ("menu.view", "F3", KeyCode::F(3)),
    ("menu.edit", "F4", KeyCode::F(4)),
    ("menu.copy", "F5", KeyCode::F(5)),
    ("menu.rename", "F2", KeyCode::F(2)),
    ("menu.copyPath", "y", KeyCode::Char('y')),
    ("menu.details", "i", KeyCode::Char('i')),
    ("menu.delete", "Del", KeyCode::Delete),
];

/// The separator row immediately before Delete.
const SEPARATOR: usize = ACTIONS.len() - 1;

impl Home {
    /// Opens the menu when the panel cursor is on an entry.
    pub(super) fn open_context_menu(&mut self, side: Side) {
        if self.panel(side).current().is_some() {
            self.overlay = Some(Overlay::ContextMenu { selected: 0 });
        }
    }

    /// Copies the cursor entry's full path or shows its details.
    pub(super) fn entry_shortcut(&mut self, side: Side, code: KeyCode, look: &Look) -> Vec<Ask> {
        let panel = self.panel(side);
        let Some(entry) = panel.current() else {
            return Vec::new();
        };
        if code == KeyCode::Char('i') {
            self.overlay = Some(Overlay::Details { side, entry });
            return Vec::new();
        }
        let path = panel.join(&entry.name);
        self.say(look.messages.get("menu.pathCopied"), false);
        vec![Ask::CopyPath(path)]
    }

    /// Moves the highlight or invokes the selected action after the overlay has been taken.
    pub(super) fn context_menu_key(
        &mut self,
        code: KeyCode,
        selected: usize,
        look: &mut Look,
    ) -> Vec<Ask> {
        let selected = match code {
            KeyCode::Esc => return Vec::new(),
            KeyCode::Enter => return self.key(key(ACTIONS[selected].2), look),
            KeyCode::Up => selected.saturating_sub(1),
            KeyCode::Down => (selected + 1).min(ACTIONS.len() - 1),
            _ => selected,
        };
        self.overlay = Some(Overlay::ContextMenu { selected });
        Vec::new()
    }

    /// Places the menu below the cursor, keeping it within the screen.
    fn context_menu_area(&self, area: Rect, look: &Look) -> Rect {
        let [_, _, left, right, ..] = self.layout(area);
        let side = self.last;
        let [panel, _] = explorer_areas(if side == Side::Left { left } else { right });
        let cursor = self.panel(side).cursor_row(panel, look);
        let copy = Line::from(self.context_copy_label(look)).width();
        let width = u16::try_from((copy + 10).max(30))
            .unwrap_or(u16::MAX)
            .min(area.width);
        let height = 12;
        Rect::new(
            (panel.x + 1).min(area.right() - width),
            (cursor + 1).min(area.bottom() - height),
            width,
            height,
        )
    }

    /// Names the destination exactly as its explorer panel does.
    fn context_copy_label(&self, look: &Look) -> String {
        let title = self.panel(other(self.last)).title(look);
        look.messages.fill("menu.copy", &[("panel", &title)])
    }

    /// Closes on an outside click or invokes the action on the clicked row.
    pub(super) fn context_menu_click(
        &mut self,
        area: Rect,
        position: Position,
        look: &mut Look,
    ) -> Vec<Ask> {
        let bounds = self.context_menu_area(area, look);
        if !bounds.contains(position) {
            self.overlay = None;
        }
        let inside = menu_block(look).inner(bounds);
        match row(inside, position, 0, ACTIONS.len() + 1) {
            Some(index) if index != SEPARATOR => {
                self.overlay = None;
                self.key(key(ACTIONS[index - usize::from(index > SEPARATOR)].2), look)
            }
            _ => Vec::new(),
        }
    }

    /// Draws the labels and a right-aligned column of shortcuts.
    pub(super) fn render_context_menu(&self, frame: &mut Frame, selected: usize, look: &Look) {
        let bounds = self.context_menu_area(frame.area(), look);
        let block = menu_block(look);
        let inside = block.inner(bounds);
        frame.render_widget(Clear, bounds);
        frame.render_widget(block, bounds);
        for (index, (message, shortcut, _)) in ACTIONS.into_iter().enumerate() {
            let y = inside.y + u16::try_from(index).expect("a menu row");
            let label = if message == "menu.copy" {
                self.context_copy_label(look)
            } else {
                look.messages.get(message)
            };
            let (y, line) = if index == SEPARATOR {
                let rule = format!(" {}", "─".repeat(usize::from(inside.width - 1)));
                frame.render_widget(
                    Line::from(rule).fg(look.palette.line),
                    Rect::new(inside.x, y, inside.width, 1),
                );
                let danger = error_line(&label, &look.palette).style;
                (
                    y + 1,
                    Line::from(vec![Span::styled(format!(" {label}"), danger)]),
                )
            } else {
                (y, Line::from(format!(" {label}")))
            };
            let area = Rect::new(inside.x, y, inside.width, 1);
            let [label_area, key_area] =
                Layout::horizontal([Constraint::Fill(1), Constraint::Length(7)]).areas(area);
            let shortcut = Line::from(format!("{shortcut} ")).right_aligned();
            let (line, shortcut) = if index == selected {
                (
                    highlight(line, &look.palette),
                    highlight(shortcut, &look.palette),
                )
            } else {
                (line, shortcut.fg(look.palette.muted))
            };
            frame.render_widget(line, label_area);
            frame.render_widget(shortcut, key_area);
        }
    }
}

/// The rounded menu border and its background.
fn menu_block(look: &Look) -> Block<'static> {
    let palette = &look.palette;
    Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(Style::new().fg(palette.accent))
        .style(Style::new().bg(palette.surface).fg(palette.text))
}
