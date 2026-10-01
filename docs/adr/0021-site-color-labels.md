# ADR-0021: Site color labels

- Status: accepted
- Date: 2026-10-01

## Context

People who keep production, staging and test servers side by side open the wrong one by mistake: the names are alike and every tab looks the same. FileZilla answers this with a background color per site, and its site manager files carry it (`Colour`, a number from 0 to 7). The interface also lacked color that means something, beyond the brand (ADR-0020).

## Decision

**A site may wear one label color**, from a fixed list: red, orange, yellow, green, teal, blue, violet and pink, or none.

- The color is stored by name in `sites.color` (empty for none, checked by the database) and validated by the site service (`invalid_color`); the API returns it as `SiteColor` and leaves it out when there is none.
- Names, not hexadecimal values, so every theme draws them in its own tones: `index.css` defines `--label-*` for light and dark schemes, stronger on light surfaces and lighter on dark ones.
- The interface shows the label where a mistake happens: a bar beside the site in the sidebar, the top edge of its tabs and a line above the path of its explorer. The color only reinforces the name, which is always written.
- The FileZilla importer reads `Colour`: red, green, blue, yellow and orange keep their name, cyan becomes teal and magenta pink. The encrypted export carries the color.

## Consequences

- Positive: servers tell apart at a glance in the places where opening the wrong one costs the most, and FileZilla users keep their colors.
- Negative: eight fixed colors, no custom ones; a theme that wants other tones has to change `index.css`.
