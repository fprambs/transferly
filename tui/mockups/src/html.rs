//! A buffer of ratatui as an HTML page of styled cells in a monospaced font, for the screenshots of make tui-mockups.

use std::fmt::Write as _;

use ratatui::buffer::Buffer;
use ratatui::style::{Color, Modifier};

/// The colors a cell without its own color takes: the terminal's own text and background, and the color of dimmed text.
#[derive(Clone, Copy)]
pub struct Defaults {
    /// Text.
    pub fg: Color,
    /// Background.
    pub bg: Color,
    /// Dimmed text.
    pub muted: Color,
}

/// The CSS color of a cell color, or the given default for the terminal's own color.
fn css(color: Color, default: Color) -> String {
    match color {
        Color::Rgb(r, g, b) => format!("#{r:02x}{g:02x}{b:02x}"),
        Color::Reset if default != Color::Reset => css(default, Color::Reset),
        _ => "inherit".to_owned(),
    }
}

/// The page of a buffer: every cell with its colors and style, in DejaVu Sans Mono so box drawing lines up.
pub fn page(buffer: &Buffer, defaults: Defaults) -> String {
    let mut body = String::new();
    for y in 0..buffer.area.height {
        let mut row = String::new();
        let mut open: Option<String> = None;
        for x in 0..buffer.area.width {
            let cell = &buffer[(x, y)];
            let (mut fg, mut bg) = (cell.fg, cell.bg);
            if cell.modifier.contains(Modifier::REVERSED) {
                std::mem::swap(&mut fg, &mut bg);
            }
            if cell.modifier.contains(Modifier::DIM) && fg == Color::Reset {
                fg = defaults.muted;
            }
            let mut style = format!("color:{};background:{}", css(fg, defaults.fg), css(bg, defaults.bg));
            if cell.modifier.contains(Modifier::BOLD) {
                style.push_str(";font-weight:700");
            }
            if cell.modifier.contains(Modifier::UNDERLINED) {
                style.push_str(";text-decoration:underline");
            }
            if open.as_deref() != Some(style.as_str()) {
                if open.is_some() {
                    row.push_str("</span>");
                }
                let _ = write!(row, "<span style=\"{style}\">");
                open = Some(style);
            }
            for ch in cell.symbol().chars() {
                match ch {
                    '<' => row.push_str("&lt;"),
                    '>' => row.push_str("&gt;"),
                    '&' => row.push_str("&amp;"),
                    _ => row.push(ch),
                }
            }
        }
        if open.is_some() {
            row.push_str("</span>");
        }
        body.push_str(&row);
        body.push('\n');
    }
    format!(
        "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><style>@font-face{{font-family:Mockup;src:url(DejaVuSansMono.ttf)}}@font-face{{font-family:Mockup;font-weight:700;src:url(DejaVuSansMono-Bold.ttf)}}html,body{{margin:0;background:{bg}}}pre{{margin:0;padding:12px;font:15px/1.17 Mockup,monospace;color:{fg};display:inline-block}}</style></head><body><pre id=\"screen\">{body}</pre></body></html>",
        bg = css(defaults.bg, Color::Reset),
        fg = css(defaults.fg, Color::Reset),
    )
}
