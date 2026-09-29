/** Whether the browser runs on an Apple system, where shortcuts use ⌘ instead of Ctrl and read as symbols. */
export function isApple(): boolean {
  const platform = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.platform
  return /mac|iphone|ipad|ipod/i.test(platform)
}

/** Symbols of the modifier keys on Apple systems. */
const appleKeys: Readonly<Record<string, string>> = { Mod: '⌘', Shift: '⇧', Alt: '⌥', Ctrl: '⌃' }

/**
 * Writes a shortcut for the system of the user (4.14c): `Mod` is Ctrl on Windows and Linux and ⌘ on Apple systems, where modifiers read
 * as symbols run together ("⌘⇧K"); elsewhere the keys are joined with + ("Ctrl+Shift+K").
 *
 * @param spec - Keys joined with +, such as "Mod+Shift+K" or "Alt+W".
 * @param apple - Whether to write it for an Apple system; the browser's own by default.
 */
export function formatShortcut(spec: string, apple = isApple()): string {
  const keys = spec.split('+')
  return apple ? keys.map((key) => appleKeys[key] ?? key).join('') : keys.map((key) => (key === 'Mod' ? 'Ctrl' : key)).join('+')
}

/** Whether a key press holds the main modifier of the system: ⌘ on Apple systems, Ctrl elsewhere; either is accepted on both. */
export function holdsMod(event: Pick<KeyboardEvent, 'ctrlKey' | 'metaKey'>): boolean {
  return event.ctrlKey || event.metaKey
}
