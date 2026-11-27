//! Mouse input and the row coordinates shared by drawing and hit testing.

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers, MouseButton, MouseEvent, MouseEventKind};
use ratatui::layout::{Position, Rect};
use ratatui::text::Line;

/// Mouse actions available to the interface.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MouseKind {
    /// The left button was pressed.
    Left,
    /// The right button was pressed.
    Right,
    /// The wheel moved up.
    Up,
    /// The wheel moved down.
    Down,
}

impl MouseKind {
    /// Recognizes presses and vertical scrolling, leaving Shift available for text selection.
    pub(super) fn from_terminal(event: MouseEvent) -> Option<Self> {
        if event.modifiers.contains(KeyModifiers::SHIFT) {
            return None;
        }
        match event.kind {
            MouseEventKind::Down(MouseButton::Left) => Some(Self::Left),
            MouseEventKind::Down(MouseButton::Right) => Some(Self::Right),
            MouseEventKind::ScrollUp => Some(Self::Up),
            MouseEventKind::ScrollDown => Some(Self::Down),
            _ => None,
        }
    }

    /// The navigation key corresponding to a wheel step.
    pub(super) fn wheel(self) -> Option<KeyEvent> {
        let code = match self {
            Self::Up => KeyCode::Up,
            Self::Down => KeyCode::Down,
            _ => return None,
        };
        Some(key(code))
    }
}

/// A synthetic key for reusing the action of a clicked control.
pub(super) fn key(code: KeyCode) -> KeyEvent {
    KeyEvent::new(code, KeyModifiers::NONE)
}

/// The index of the first row rendered in a list.
pub(super) fn first_row(area: Rect, cursor: usize) -> usize {
    cursor.saturating_sub(usize::from(area.height.saturating_sub(1)))
}

/// The index of a rendered row under the pointer.
pub(super) fn row(area: Rect, position: Position, first: usize, count: usize) -> Option<usize> {
    if !area.contains(position) {
        return None;
    }
    let index = first + usize::from(position.y - area.y);
    (index < count).then_some(index)
}

/// The action of a clicked key label on a popup's bottom border.
pub(super) fn footer_key(
    area: Rect,
    position: Position,
    labels: &str,
    codes: &[KeyCode],
) -> Option<KeyEvent> {
    if !area.contains(position) || position.y + 1 != area.bottom() {
        return None;
    }
    let width = Line::from(labels).width();
    let interior = usize::from(area.width.saturating_sub(2));
    if width + 2 > interior {
        return None;
    }
    let start = usize::from(area.right()) - width - 2;
    label_key(position.x, start, labels, codes)
}

/// The action under a column in a row of key labels separated by middle dots.
pub(super) fn label_key(
    column: u16,
    mut start: usize,
    labels: &str,
    codes: &[KeyCode],
) -> Option<KeyEvent> {
    for (label, code) in labels.split(" · ").zip(codes) {
        let end = start + Line::from(label).width();
        if (start..end).contains(&usize::from(column)) {
            return Some(key(*code));
        }
        start = end + 3;
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Only a fully visible label on the footer is actionable.
    #[test]
    fn hits_only_visible_footer_labels() {
        let area = Rect::new(10, 10, 15, 6);
        let codes = [KeyCode::Enter, KeyCode::Esc];
        assert_eq!(
            footer_key(area, Position::new(18, 15), "yes · no", &codes),
            None
        );
        assert_eq!(
            footer_key(area, Position::new(18, 14), "yes · no", &codes),
            None
        );
        assert_eq!(
            footer_key(area, Position::new(18, 15), "a very long label", &codes),
            None
        );
    }
}
