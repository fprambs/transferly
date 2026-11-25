//! How the interface looks and speaks: the messages of its language, the palette of its theme in the colors this terminal shows, and the
//! settings of this computer, which choose between the colors of the theme and the terminal's own.

use super::theme::{self, ColorMode, Palette};
use crate::config::{Colors, Settings};
use crate::i18n::{Language, Messages};

/// The language, the theme and the settings the interface wears.
#[derive(Debug, Clone)]
pub struct Look {
    /// The messages of the language shown.
    pub messages: Messages,
    /// The colors the interface paints with.
    pub palette: Palette,
    /// The theme worn, by its identifier.
    pub theme: String,
    /// The settings of this computer.
    pub settings: Settings,
    /// How this terminal shows colors, before the settings choose its own palette.
    base: ColorMode,
}

impl Look {
    /// The look of a language and a theme on a terminal, with the settings of this computer.
    #[must_use]
    pub fn new(language: Language, theme: &str, base: ColorMode, settings: Settings) -> Self {
        let mut look = Self {
            messages: Messages::new(language),
            palette: Palette::new(&theme::find(theme), base),
            theme: theme.to_owned(),
            settings,
            base,
        };
        look.wear(theme);
        look
    }

    /// How colors are shown: the terminal's own palette when the settings ask for it, unless there are no colors at all.
    #[must_use]
    pub fn mode(&self) -> ColorMode {
        if self.settings.colors == Colors::Terminal && self.base != ColorMode::Plain {
            ColorMode::Terminal
        } else {
            self.base
        }
    }

    /// Wears a theme, by its identifier; an unknown one wears the default, as the web does.
    pub fn wear(&mut self, theme: &str) {
        let found = theme::find(theme);
        self.palette = Palette::new(&found, self.mode());
        self.theme = found.id;
    }

    /// Speaks the language of a tag, such as `es`.
    pub fn speak(&mut self, tag: &str) {
        self.messages = Messages::new(Language::from_tag(tag));
    }
}
