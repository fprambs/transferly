//! The pieces every screen is drawn with, as the mockups of docs/design/terminal-client-mockups.md show them: panels with rounded
//! borders, the card of the sign-in screens and text fields, in the colors of the palette.

use ratatui::Frame;
use ratatui::layout::{Constraint, Layout, Margin, Position, Rect};
use ratatui::style::{Modifier, Style, Stylize};
use ratatui::text::Line;
use ratatui::widgets::{Block, BorderType, Clear, Paragraph};

use super::input::Input;
use super::theme::Palette;

/// A panel with rounded borders; the focused one has the accent border and a bold title.
#[must_use]
pub fn panel(title: &str, focused: bool, palette: &Palette) -> Block<'static> {
    let title = Line::from(format!(" {title} "));
    let block = Block::bordered().border_type(BorderType::Rounded);
    if focused {
        block
            .border_style(Style::new().fg(palette.accent))
            .title(title.fg(palette.accent_text).add_modifier(Modifier::BOLD))
    } else {
        block
            .border_style(Style::new().fg(palette.line))
            .title(title.fg(palette.muted))
    }
}

/// A rectangle of the given size centered in an area, shrunk to fit it.
#[must_use]
pub fn centered(area: Rect, width: u16, height: u16) -> Rect {
    let width = width.min(area.width);
    let height = height.min(area.height);
    Rect::new(
        area.x + (area.width - width) / 2,
        area.y + (area.height - height) / 2,
        width,
        height,
    )
}

/// Paints the background of the screen.
pub fn background(frame: &mut Frame, palette: &Palette) {
    frame.render_widget(
        Block::new().style(Style::new().bg(palette.bg).fg(palette.text)),
        frame.area(),
    );
}

/// The card of the sign-in screens: the name and tagline above a focused panel with its keys at the bottom; returns its inside.
pub fn card(
    frame: &mut Frame,
    palette: &Palette,
    heading: (&str, &str),
    title: &str,
    keys: &str,
) -> Rect {
    let area = centered(frame.area(), 76, 22);
    let [head, body] = Layout::vertical([Constraint::Length(3), Constraint::Fill(1)]).areas(area);
    frame.render_widget(
        Paragraph::new(vec![
            Line::from(heading.0.to_owned()).bold().centered(),
            Line::from(heading.1.to_owned())
                .fg(palette.muted)
                .centered(),
        ]),
        head,
    );
    let block =
        panel(title, true, palette).title_bottom(Line::from(format!(" {keys} ")).right_aligned());
    let inside = block.inner(body).inner(Margin::new(2, 1));
    frame.render_widget(block, body);
    inside
}

/// A one-line text field with its label as the title of its border; when focused it shows the cursor of the terminal.
pub fn field(
    frame: &mut Frame,
    area: Rect,
    palette: &Palette,
    label: &str,
    input: &Input,
    focused: bool,
) {
    let block = panel(label, focused, palette);
    let inside = block.inner(area);
    let width = usize::from(inside.width.max(1)) - 1;
    let skip = input.cursor().saturating_sub(width);
    let visible: String = input.value().chars().skip(skip).take(width + 1).collect();
    frame.render_widget(Paragraph::new(visible).block(block), area);
    if focused {
        let column = u16::try_from(input.cursor() - skip).unwrap_or(u16::MAX);
        frame.set_cursor_position(Position::new(inside.x.saturating_add(column), inside.y));
    }
}

/// A popup over the screen: the area cleared, a focused panel with its title and its keys at the bottom; returns its inside.
pub fn popup(frame: &mut Frame, area: Rect, title: &str, keys: &str, palette: &Palette) -> Rect {
    let block = panel(title, true, palette)
        .title_bottom(Line::from(format!(" {keys} ")).right_aligned())
        .style(Style::new().bg(palette.surface).fg(palette.text));
    let inside = block.inner(area).inner(Margin::new(1, 0));
    frame.render_widget(Clear, area);
    frame.render_widget(block, area);
    inside
}

/// A line marked as the selected one: on the raised background and bold, or reversed when there is no color.
#[must_use]
pub fn highlight(line: Line<'static>, palette: &Palette) -> Line<'static> {
    let style = if palette.plain {
        Style::new().add_modifier(Modifier::REVERSED)
    } else {
        Style::new()
            .bg(palette.raised)
            .fg(palette.accent_text)
            .add_modifier(Modifier::BOLD)
    };
    line.style(style)
}

/// A line in the danger color, for what went wrong.
#[must_use]
pub fn error_line(text: &str, palette: &Palette) -> Line<'static> {
    let line = Line::from(text.to_owned()).fg(palette.danger);
    if palette.plain {
        line.add_modifier(Modifier::BOLD)
    } else {
        line
    }
}

#[cfg(test)]
mod tests {
    use ratatui::Terminal;
    use ratatui::backend::TestBackend;

    use super::*;
    use crate::ui::theme::{ColorMode, Palette, find};

    /// Draws with a test backend and returns the text of every row.
    fn draw(
        width: u16,
        height: u16,
        paint: impl FnOnce(&mut Frame),
    ) -> (Vec<String>, Terminal<TestBackend>) {
        let mut terminal = Terminal::new(TestBackend::new(width, height)).expect("backend");
        terminal.draw(paint).expect("draw");
        let buffer = terminal.backend().buffer().clone();
        let rows = (0..height)
            .map(|y| {
                (0..width)
                    .map(|x| buffer[(x, y)].symbol().to_owned())
                    .collect::<String>()
            })
            .collect();
        (rows, terminal)
    }

    /// A field longer than its box scrolls to keep the cursor in view and puts the cursor of the terminal there.
    #[test]
    fn scrolls_long_fields() {
        let palette = Palette::new(&find("transferly-dark"), ColorMode::TrueColor);
        let input = Input::with("https://a-very-long-server-name.example.com");
        let (rows, mut terminal) = draw(20, 3, |frame| {
            field(frame, frame.area(), &palette, "Server", &input, true);
        });
        assert!(rows[1].contains("example.com") && !rows[1].contains("https"));
        terminal
            .backend_mut()
            .assert_cursor_position(Position::new(19 - 1, 1));
        let (rows, _) = draw(20, 3, |frame| {
            field(frame, frame.area(), &palette, "Server", &input, false);
        });
        assert!(rows[0].contains("Server"));
    }

    /// The card centers the name, the tagline and the panel with its keys; errors are bold when there is no color.
    #[test]
    fn draws_the_card() {
        let palette = Palette::new(&find("nord"), ColorMode::Plain);
        let (rows, _) = draw(80, 24, |frame| {
            background(frame, &palette);
            let inside = card(
                frame,
                &palette,
                ("Transferly", "tagline"),
                "Sign in",
                "Esc quit",
            );
            frame.render_widget(error_line("broken", &palette), inside);
        });
        assert!(rows.iter().any(|row| row.trim() == "Transferly"));
        assert!(rows.iter().any(|row| row.contains("Sign in")));
        assert!(rows.iter().any(|row| row.contains("Esc quit")));
        assert!(rows.iter().any(|row| row.contains("broken")));
        assert_eq!(
            centered(Rect::new(0, 0, 10, 4), 20, 20),
            Rect::new(0, 0, 10, 4)
        );
        assert!(
            error_line("x", &Palette::new(&find("nord"), ColorMode::TrueColor))
                .style
                .add_modifier
                .is_empty()
        );
    }
}
