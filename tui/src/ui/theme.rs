//! The themes of the web (`tui/themes.json`, exported by `make gen` from `frontend/src/lib/theme/themes.ts`) and how this terminal shows
//! them: in truecolor when it says it can, as the nearest of its 256 colors otherwise, in its own 16 colors when the person prefers them,
//! or without color when `NO_COLOR` is set (docs/design/terminal-client.md, Visual language).

use std::collections::HashMap;

use ratatui::style::Color;
use serde::Deserialize;

use crate::config::Colors;

/// A theme as the web defines it.
#[derive(Debug, Clone, Deserialize)]
pub struct Theme {
    /// Identifier, as the account stores it, such as `transferly-dark`.
    pub id: String,
    /// Name shown when choosing.
    pub name: String,
    /// Colors by token, as `#rrggbb`.
    tokens: HashMap<String, String>,
}

/// The file of themes.
#[derive(Deserialize)]
struct File {
    #[serde(rename = "defaultThemeId")]
    default_theme_id: String,
    themes: Vec<Theme>,
}

/// The themes file embedded in the binary.
fn file() -> File {
    serde_json::from_str(include_str!("../../themes.json")).expect("the embedded themes")
}

/// Every theme of the web, in its order.
#[must_use]
pub fn all() -> Vec<Theme> {
    file().themes
}

/// The theme with an identifier, or the default theme for an unknown one, as the web falls back.
///
/// # Panics
///
/// The embedded themes lack their own default, which their tests rule out.
#[must_use]
pub fn find(id: &str) -> Theme {
    let file = file();
    let default = file.default_theme_id.clone();
    let mut themes = file.themes;
    let index = themes
        .iter()
        .position(|theme| theme.id == id)
        .or_else(|| themes.iter().position(|theme| theme.id == default))
        .expect("the default theme exists");
    themes.swap_remove(index)
}

/// How this terminal shows colors.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ColorMode {
    /// 24-bit colors, exactly those of the theme.
    TrueColor,
    /// The nearest of the 256 colors of xterm.
    Indexed,
    /// The 16 colors of the terminal's own palette.
    Terminal,
    /// No color at all; bold, reverse and symbols carry the meaning.
    Plain,
}

impl ColorMode {
    /// The mode for this terminal: none with `NO_COLOR`, its own palette when the person chose it, truecolor when `COLORTERM` says so,
    /// and 256 colors otherwise.
    #[must_use]
    pub fn detect(env: impl Fn(&str) -> Option<String>, colors: Colors) -> Self {
        if env("NO_COLOR").is_some_and(|value| !value.is_empty()) {
            return Self::Plain;
        }
        if colors == Colors::Terminal {
            return Self::Terminal;
        }
        match env("COLORTERM").as_deref() {
            Some("truecolor" | "24bit") => Self::TrueColor,
            _ => Self::Indexed,
        }
    }
}

/// The colors the interface paints with.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Palette {
    /// Background of the screen.
    pub bg: Color,
    /// Background of bars and popups.
    pub surface: Color,
    /// Background of the selected row.
    pub raised: Color,
    /// Borders without the focus.
    pub line: Color,
    /// Text.
    pub text: Color,
    /// Secondary text.
    pub muted: Color,
    /// Faint text, such as headers.
    pub faint: Color,
    /// The focus, progress and the selected tab.
    pub accent: Color,
    /// Text in the accent color.
    pub accent_text: Color,
    /// Text on the accent color.
    pub ink: Color,
    /// Success.
    pub ok: Color,
    /// Danger.
    pub danger: Color,
    /// Warnings.
    pub warn: Color,
    /// Folders and links.
    pub info: Color,
    /// Whether there is no color, so selection and focus must show with reverse and bold.
    pub plain: bool,
}

impl Palette {
    /// The palette of a theme in a color mode.
    #[must_use]
    pub fn new(theme: &Theme, mode: ColorMode) -> Self {
        match mode {
            ColorMode::Plain => Self::uniform(Color::Reset, true),
            ColorMode::Terminal => Self {
                surface: Color::Reset,
                raised: Color::DarkGray,
                line: Color::DarkGray,
                muted: Color::Gray,
                faint: Color::DarkGray,
                accent: Color::Yellow,
                accent_text: Color::Yellow,
                ink: Color::Black,
                ok: Color::Green,
                danger: Color::Red,
                warn: Color::Yellow,
                info: Color::Blue,
                ..Self::uniform(Color::Reset, false)
            },
            ColorMode::TrueColor | ColorMode::Indexed => {
                let token = |name: &str| {
                    let (r, g, b) = rgb(theme.tokens.get(name).map_or("#000000", String::as_str));
                    if mode == ColorMode::Indexed {
                        Color::Indexed(nearest_xterm(r, g, b))
                    } else {
                        Color::Rgb(r, g, b)
                    }
                };
                Self {
                    bg: token("bg"),
                    surface: token("surface"),
                    raised: token("raised"),
                    line: token("line"),
                    text: token("text"),
                    muted: token("muted"),
                    faint: token("faint"),
                    accent: token("accent"),
                    accent_text: token("accentText"),
                    ink: token("ink"),
                    ok: token("ok"),
                    danger: token("danger"),
                    warn: token("warn"),
                    info: token("info"),
                    plain: false,
                }
            }
        }
    }

    /// A palette of one color everywhere.
    fn uniform(color: Color, plain: bool) -> Self {
        Self {
            bg: color,
            surface: color,
            raised: color,
            line: color,
            text: color,
            muted: color,
            faint: color,
            accent: color,
            accent_text: color,
            ink: color,
            ok: color,
            danger: color,
            warn: color,
            info: color,
            plain,
        }
    }
}

/// The red, green and blue of `#rrggbb`; anything else is black.
fn rgb(hex: &str) -> (u8, u8, u8) {
    let [_, r, g, b] = u32::from_str_radix(hex.trim_start_matches('#'), 16)
        .unwrap_or(0)
        .to_be_bytes();
    (r, g, b)
}

/// The levels of each channel in the 6×6×6 color cube of xterm.
const CUBE: [u8; 6] = [0, 95, 135, 175, 215, 255];

/// The nearest of the 256 colors of xterm, from its color cube and its gray ramp.
fn nearest_xterm(r: u8, g: u8, b: u8) -> u8 {
    let level = |channel: u8| {
        (0u8..6)
            .min_by_key(|index| CUBE[usize::from(*index)].abs_diff(channel))
            .unwrap_or(0)
    };
    let (ri, gi, bi) = (level(r), level(g), level(b));
    let cube = (
        CUBE[usize::from(ri)],
        CUBE[usize::from(gi)],
        CUBE[usize::from(bi)],
    );
    let average = (u16::from(r) + u16::from(g) + u16::from(b)) / 3;
    let gray_index = u8::try_from(average.saturating_sub(3) / 10)
        .unwrap_or(u8::MAX)
        .min(23);
    let gray = 8 + 10 * gray_index;
    let distance = |(cr, cg, cb): (u8, u8, u8)| {
        let d = |a: u8, b: u8| u32::from(a.abs_diff(b)).pow(2);
        d(cr, r) + d(cg, g) + d(cb, b)
    };
    if distance((gray, gray, gray)) < distance(cube) {
        232 + gray_index
    } else {
        16 + 36 * ri + 6 * gi + bi
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The twelve themes of the web load with every token, and unknown identifiers fall back to the default.
    #[test]
    fn loads_the_themes_of_the_web() {
        let themes = all();
        assert_eq!(themes.len(), 12);
        for theme in &themes {
            for token in [
                "bg",
                "surface",
                "raised",
                "line",
                "text",
                "muted",
                "faint",
                "accent",
                "accentText",
                "ink",
                "ok",
                "danger",
                "warn",
                "info",
            ] {
                assert!(
                    theme.tokens.contains_key(token),
                    "{} has no {token}",
                    theme.id
                );
            }
        }
        assert_eq!(find("nord").name, "Nord");
        assert_eq!(find("no-such-theme").id, "transferly-dark");
    }

    /// The mode follows `NO_COLOR`, then the choice of the person, then what the terminal says it can show.
    #[test]
    fn detects_the_color_mode() {
        let env = |pairs: &'static [(&'static str, &'static str)]| {
            move |name: &str| {
                pairs
                    .iter()
                    .find(|(k, _)| *k == name)
                    .map(|(_, v)| (*v).to_owned())
            }
        };
        assert_eq!(
            ColorMode::detect(
                env(&[("NO_COLOR", "1"), ("COLORTERM", "truecolor")]),
                Colors::Theme
            ),
            ColorMode::Plain
        );
        assert_eq!(
            ColorMode::detect(env(&[("NO_COLOR", "")]), Colors::Terminal),
            ColorMode::Terminal
        );
        assert_eq!(
            ColorMode::detect(env(&[("COLORTERM", "24bit")]), Colors::Theme),
            ColorMode::TrueColor
        );
        assert_eq!(
            ColorMode::detect(env(&[("COLORTERM", "truecolor")]), Colors::Theme),
            ColorMode::TrueColor
        );
        assert_eq!(
            ColorMode::detect(env(&[]), Colors::Theme),
            ColorMode::Indexed
        );
    }

    /// Each mode paints with what the terminal can show: the exact colors, the nearest indexed ones, the terminal's own, or none.
    #[test]
    fn paints_in_each_mode() {
        let theme = find("transferly-dark");
        let exact = Palette::new(&theme, ColorMode::TrueColor);
        assert_eq!(
            (exact.accent, exact.bg, exact.plain),
            (
                Color::Rgb(0xff, 0x98, 0x00),
                Color::Rgb(0x12, 0x12, 0x12),
                false
            )
        );
        let indexed = Palette::new(&theme, ColorMode::Indexed);
        assert_eq!(
            (indexed.accent, indexed.bg),
            (Color::Indexed(208), Color::Indexed(233))
        );
        let terminal = Palette::new(&theme, ColorMode::Terminal);
        assert_eq!(
            (terminal.accent, terminal.bg, terminal.text),
            (Color::Yellow, Color::Reset, Color::Reset)
        );
        let plain = Palette::new(&theme, ColorMode::Plain);
        assert!(plain.plain && plain.accent == Color::Reset && plain.danger == Color::Reset);
    }

    /// The nearest xterm color is the cube or the gray ramp, whichever is closer; broken hex is black.
    #[test]
    fn finds_the_nearest_indexed_color() {
        assert_eq!(nearest_xterm(255, 0, 0), 196);
        assert_eq!(nearest_xterm(0, 0, 0), 16);
        assert_eq!(nearest_xterm(128, 128, 128), 244);
        assert_eq!(nearest_xterm(255, 255, 255), 231);
        assert_eq!(rgb("not a color"), (0, 0, 0));
        let broken = Theme {
            id: "x".into(),
            name: "x".into(),
            tokens: HashMap::new(),
        };
        assert_eq!(
            Palette::new(&broken, ColorMode::TrueColor).text,
            Color::Rgb(0, 0, 0)
        );
    }
}
