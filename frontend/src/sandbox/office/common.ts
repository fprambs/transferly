/** Creates an element with a class and, optionally, its text, which is always set as text, never as markup. */
export function element<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag)
  if (className !== undefined) {
    node.className = className
  }
  if (text !== undefined) {
    node.textContent = text
  }
  return node
}

/** Fills the placeholders of a translated label, such as `{{count}}`, with their values. */
export function fill(label: string, values: Record<string, string | number>): string {
  return Object.entries(values).reduce((text, [name, value]) => text.replaceAll(`{{${name}}}`, String(value)), label)
}

/** A document protected with a password, which the viewer does not open. */
export class ProtectedDocument extends Error {
  /** Name of the error, which the page reports as a protected file. */
  override name = 'ProtectedDocument'
}

/** Keeps a color written as `#rgb` or `#rrggbb`, or as six hexadecimal digits, and drops anything else a document may hold. */
export function safeColor(value: string | null | undefined): string | undefined {
  if (value === null || value === undefined) {
    return undefined
  }
  const hex = value.startsWith('#') ? value.slice(1) : value
  return /^([0-9a-f]{3}|[0-9a-f]{6})$/i.test(hex) ? `#${hex}` : undefined
}
