/** Colors of a theme. Every value is a six-digit hexadecimal color such as `#0b0d11`. */
export interface ThemeTokens {
  /** Page background. */
  bg: string
  /** Panels: top bar, sidebar, dock. */
  surface: string
  /** Controls and highlighted rows, one step above the surface. */
  raised: string
  /** Borders. */
  line: string
  /** Dividers between rows, fainter than a border. */
  lineSoft: string
  /** Primary text. */
  text: string
  /** Secondary text. */
  muted: string
  /** Tertiary text: captions, hints and units. */
  faint: string
  /** Brand color for fills: primary buttons, progress, focus. */
  accent: string
  /** Brand color for text and icons on the page and surfaces; equals the accent unless it is too light or dark to read. */
  accentText: string
  /** Text and icons on top of the accent. */
  ink: string
  /** Success and connected state. */
  ok: string
  /** Errors and destructive actions. */
  danger: string
  /** Warnings and states that need attention. */
  warn: string
  /** Informational state and folders. */
  info: string
}

/** Whether a theme is dark or light, which sets the browser's `color-scheme`. */
export type ThemeScheme = 'dark' | 'light'

/** A named set of colors the interface can wear. */
export interface ThemeDefinition {
  /** Stable identifier: lower case letters, digits and hyphens; stored with the account. */
  id: string
  /** Name shown in the theme list. Proper names are not translated. */
  name: string
  /** Whether it is a dark or a light theme. */
  scheme: ThemeScheme
  /** The colors. */
  tokens: ThemeTokens
}

/** Fraction of the accent used for soft accent backgrounds, as a percentage of the mix. */
const accentSoftPercent = 14

/**
 * Turns a theme into the CSS custom properties the interface reads.
 *
 * The names follow shadcn/ui (`--background`, `--primary`, ...) so its components work unchanged, plus the extra
 * steps the design needs (`--surface`, `--raised`, `--faint`, `--accent-text`, `--ok`, `--warn`, `--info`).
 * Note that shadcn's `--accent` is a hover background, while the brand color is `--primary`.
 *
 * @param tokens - The colors of the theme.
 * @returns Custom property names mapped to their values.
 */
export function toCssVariables(tokens: ThemeTokens): Record<string, string> {
  return {
    '--background': tokens.bg,
    '--foreground': tokens.text,
    '--card': tokens.surface,
    '--card-foreground': tokens.text,
    '--popover': tokens.raised,
    '--popover-foreground': tokens.text,
    '--primary': tokens.accent,
    '--primary-foreground': tokens.ink,
    '--secondary': tokens.raised,
    '--secondary-foreground': tokens.text,
    '--muted': tokens.raised,
    '--muted-foreground': tokens.muted,
    '--accent': tokens.raised,
    '--accent-foreground': tokens.text,
    '--destructive': tokens.danger,
    '--border': tokens.line,
    '--input': tokens.line,
    '--ring': tokens.accent,
    '--chart-1': tokens.accent,
    '--chart-2': tokens.info,
    '--chart-3': tokens.ok,
    '--chart-4': tokens.warn,
    '--chart-5': tokens.danger,
    '--sidebar': tokens.surface,
    '--sidebar-foreground': tokens.text,
    '--sidebar-primary': tokens.accent,
    '--sidebar-primary-foreground': tokens.ink,
    '--sidebar-accent': tokens.raised,
    '--sidebar-accent-foreground': tokens.text,
    '--sidebar-border': tokens.line,
    '--sidebar-ring': tokens.accent,
    '--surface': tokens.surface,
    '--raised': tokens.raised,
    '--line-soft': tokens.lineSoft,
    '--faint': tokens.faint,
    '--accent-text': tokens.accentText,
    '--accent-soft': `color-mix(in srgb, ${tokens.accent} ${accentSoftPercent}%, transparent)`,
    '--ok': tokens.ok,
    '--warn': tokens.warn,
    '--info': tokens.info,
  }
}
