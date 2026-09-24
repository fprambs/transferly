/** Color scheme the user can choose; `system` follows the operating system. */
export type ThemePreference = 'light' | 'dark' | 'system'

/** Color scheme actually applied to the page. */
export type ResolvedTheme = 'light' | 'dark'

/** Every selectable theme preference, in display order. */
export const themePreferences: readonly ThemePreference[] = ['light', 'dark', 'system']

/**
 * Resolves a preference to the scheme to apply.
 *
 * @param preference - The user's choice.
 * @param prefersDark - Whether the operating system prefers a dark scheme.
 */
export function resolveTheme(preference: ThemePreference, prefersDark: boolean): ResolvedTheme {
  if (preference === 'system') {
    return prefersDark ? 'dark' : 'light'
  }
  return preference
}

/**
 * Applies a resolved scheme to an element by toggling the `dark` class and the CSS `color-scheme`.
 *
 * @param root - Element receiving the scheme; normally `<html>`.
 * @param theme - Scheme to apply.
 */
export function applyTheme(root: HTMLElement, theme: ResolvedTheme): void {
  root.classList.toggle('dark', theme === 'dark')
  root.style.colorScheme = theme
}
