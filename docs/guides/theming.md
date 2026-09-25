# Theming

A theme is a set of fifteen colors (`ThemeTokens` in `frontend/src/lib/theme/tokens.ts`); everything else in the interface reads CSS custom properties derived from them. Adding a theme means adding one entry to `themes.ts`.

## Rules

- **No fixed colors in components.** Use the Tailwind names that map to the variables: `bg-background`, `bg-card`, `bg-surface`, `bg-raised`, `text-foreground`, `text-muted-foreground`, `text-faint`, `text-accent-text`, `bg-primary text-primary-foreground`, `border-border`, `border-line-soft`, and the state colors `text-ok`, `text-warn`, `text-danger` (`text-destructive`), `text-info`. Soft state backgrounds use `bg-accent-soft` or `color-mix(in srgb, var(--warn) 16%, transparent)`.
- shadcn/ui's `accent` is a hover background (the `raised` step), not the brand color. The brand color is `primary`.
- The default theme, Sentinel Dark, is also written in the `:root` block of `index.css`, so the page paints correctly before any script runs. A test keeps that block equal to the registry.
- Applying a theme sets the variables on `<html>`, the `dark` class shadcn variants read, `color-scheme`, and `data-theme`.
- Themes have stable identifiers (`sentinel-dark`, `monokai`, ...): lower case letters, digits and hyphens. They are stored in the browser (`sentinel-ui`) and in the account (`theme` preference), and an unknown identifier falls back to the default theme.
- Theme names are proper names and are not translated; only the group headings (Dark, Light) are.

## Readability

Every theme must pass the contrast rules in `themes.test.ts` (WCAG 2.1 AA): primary text 7:1; secondary, tertiary and brand text 4.5:1 on every surface; text on the brand color 4.5:1; the four state colors 4.5:1 on the page and on panels. The Playwright accessibility scan runs axe on the real page in all twelve themes.

## Adding a theme

1. Add the entry to `themes` with its fifteen colors.
2. Run the unit tests; the readability test names every pair that fails. Adjust the color, not the rule.
3. Add it to the list in `e2e/specs/accessibility.spec.ts`; add it to the visual specs only if its look needs a baseline of its own.
