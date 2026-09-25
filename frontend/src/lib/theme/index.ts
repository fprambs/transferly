import { defaultThemeId, findTheme, systemLightThemeId } from './themes'
import { type ThemeDefinition, toCssVariables } from './tokens'

export { contrastRatio, luminance } from './contrast'
export { defaultThemeId, findTheme, systemLightThemeId, themes } from './themes'
export { type ThemeDefinition, type ThemeScheme, type ThemeTokens, toCssVariables } from './tokens'

/** The choice of theme a user makes. */
export interface ThemeChoice {
  /** Identifier of the theme to wear when the interface does not follow the system. */
  themeId: string
  /** Whether to wear a light theme by day and the default dark theme by night, following the operating system. */
  followSystem: boolean
}

/**
 * Picks the theme to wear.
 *
 * @param choice - What the user chose.
 * @param prefersDark - Whether the operating system prefers a dark scheme.
 * @returns The chosen theme; an unknown identifier falls back to the default theme.
 */
export function resolveTheme(choice: ThemeChoice, prefersDark: boolean): ThemeDefinition {
  const id = choice.followSystem ? (prefersDark ? defaultThemeId : systemLightThemeId) : choice.themeId
  return findTheme(id) ?? (findTheme(defaultThemeId) as ThemeDefinition)
}

/**
 * Applies a theme to an element: sets its custom properties, the `dark` class that shadcn/ui variants read, the
 * CSS `color-scheme` and a `data-theme` attribute for tests and styles.
 *
 * @param root - Element receiving the theme; normally `<html>`.
 * @param theme - Theme to apply.
 */
export function applyTheme(root: HTMLElement, theme: ThemeDefinition): void {
  for (const [name, value] of Object.entries(toCssVariables(theme.tokens))) {
    root.style.setProperty(name, value)
  }
  root.classList.toggle('dark', theme.scheme === 'dark')
  root.style.colorScheme = theme.scheme
  root.dataset.theme = theme.id
}
