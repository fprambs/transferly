/** Escapes the characters a regular expression treats specially, so plain text matches itself literally. */
function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Turns a glob with `*` (any run of characters) and `?` (one character) into a regular expression that matches a whole name. */
function globToRegExp(pattern: string): RegExp {
  const body = pattern
    .split(/([*?])/)
    .map((part) => (part === '*' ? '.*' : part === '?' ? '.' : escapeRegExp(part)))
    .join('')
  return new RegExp(`^${body}$`, 'i')
}

/**
 * Builds a matcher for a search query: a pattern with `*` or `?` matches the whole name as a glob, case-insensitively; plain text
 * matches anywhere in the name, case-insensitively. A blank query matches nothing, so a search never starts by accident.
 */
export function matcherFor(query: string): (name: string) => boolean {
  const trimmed = query.trim()
  if (trimmed === '') {
    return () => false
  }
  if (/[*?]/.test(trimmed)) {
    const regex = globToRegExp(trimmed)
    return (name) => regex.test(name)
  }
  const needle = trimmed.toLowerCase()
  return (name) => name.toLowerCase().includes(needle)
}
