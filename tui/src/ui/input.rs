//! A one-line text field: the address of a server, a pasted code, later a filter or a command.

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};

/// The text of a field and where the cursor is, in characters.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Input {
    value: String,
    cursor: usize,
}

impl Input {
    /// A field holding a value, with the cursor at its end.
    #[must_use]
    pub fn with(value: &str) -> Self {
        Self {
            value: value.to_owned(),
            cursor: value.chars().count(),
        }
    }

    /// The text.
    #[must_use]
    pub fn value(&self) -> &str {
        &self.value
    }

    /// The position of the cursor, in characters.
    #[must_use]
    pub fn cursor(&self) -> usize {
        self.cursor
    }

    /// The byte offset of a character position.
    fn offset(&self, chars: usize) -> usize {
        self.value
            .char_indices()
            .nth(chars)
            .map_or(self.value.len(), |(index, _)| index)
    }

    /// Inserts text at the cursor, without line breaks, as typing or pasting does.
    pub fn insert(&mut self, text: &str) {
        let clean: String = text.chars().filter(|c| !c.is_control()).collect();
        let at = self.offset(self.cursor);
        self.value.insert_str(at, &clean);
        self.cursor += clean.chars().count();
    }

    /// Applies an editing key and tells whether the field used it: characters, Backspace, Delete, ←, →, Home, End, Ctrl+U clears and
    /// Ctrl+W deletes the word before the cursor.
    pub fn handle(&mut self, key: KeyEvent) -> bool {
        let control = key.modifiers.contains(KeyModifiers::CONTROL);
        match key.code {
            KeyCode::Char('u') if control => *self = Self::default(),
            KeyCode::Char('w') if control => {
                let before: Vec<char> = self.value.chars().take(self.cursor).collect();
                let kept = before
                    .iter()
                    .rposition(|c| !c.is_whitespace())
                    .map_or(0, |end| {
                        before[..=end]
                            .iter()
                            .rposition(|c| c.is_whitespace())
                            .map_or(0, |space| space + 1)
                    });
                let (start, end) = (self.offset(kept), self.offset(self.cursor));
                self.value.replace_range(start..end, "");
                self.cursor = kept;
            }
            KeyCode::Char(c) if !control => self.insert(&c.to_string()),
            KeyCode::Backspace if self.cursor > 0 => {
                self.cursor -= 1;
                let at = self.offset(self.cursor);
                self.value.remove(at);
            }
            KeyCode::Delete if self.cursor < self.value.chars().count() => {
                let at = self.offset(self.cursor);
                self.value.remove(at);
            }
            KeyCode::Left => self.cursor = self.cursor.saturating_sub(1),
            KeyCode::Right => self.cursor = (self.cursor + 1).min(self.value.chars().count()),
            KeyCode::Home => self.cursor = 0,
            KeyCode::End => self.cursor = self.value.chars().count(),
            KeyCode::Backspace | KeyCode::Delete => {}
            _ => return false,
        }
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A key without modifiers.
    fn key(code: KeyCode) -> KeyEvent {
        KeyEvent::new(code, KeyModifiers::NONE)
    }

    /// A key with Control.
    fn ctrl(c: char) -> KeyEvent {
        KeyEvent::new(KeyCode::Char(c), KeyModifiers::CONTROL)
    }

    /// Typing, moving and deleting work on characters, not bytes, and stop at the ends.
    #[test]
    fn edits_characters() {
        let mut input = Input::default();
        for c in "ñandú".chars() {
            assert!(input.handle(key(KeyCode::Char(c))));
        }
        assert_eq!((input.value(), input.cursor()), ("ñandú", 5));
        input.handle(key(KeyCode::Left));
        input.handle(key(KeyCode::Backspace));
        assert_eq!(input.value(), "ñanú");
        input.handle(key(KeyCode::Home));
        input.handle(key(KeyCode::Delete));
        assert_eq!((input.value(), input.cursor()), ("anú", 0));
        input.handle(key(KeyCode::Backspace));
        input.handle(key(KeyCode::Left));
        input.handle(key(KeyCode::End));
        input.handle(key(KeyCode::Delete));
        input.handle(key(KeyCode::Right));
        assert_eq!((input.value(), input.cursor()), ("anú", 3));
        assert!(!input.handle(key(KeyCode::Enter)));
        assert!(!input.handle(ctrl('k')));
    }

    /// Pasting drops line breaks; Ctrl+W deletes the word before the cursor and Ctrl+U clears the field.
    #[test]
    fn pastes_and_deletes_words() {
        let mut input = Input::with("files.example.com");
        input.insert(" https://other\n");
        assert_eq!(input.value(), "files.example.com https://other");
        input.handle(ctrl('w'));
        assert_eq!(input.value(), "files.example.com ");
        input.handle(ctrl('w'));
        assert_eq!((input.value(), input.cursor()), ("", 0));
        input.handle(ctrl('w'));
        input.insert("   ");
        input.handle(ctrl('w'));
        assert_eq!(input.value(), "");
        let mut input = Input::with("abc");
        input.handle(ctrl('u'));
        assert_eq!(input, Input::default());
    }
}
