/** How many names are tried before giving up. */
export const maxRenames = 1000

/**
 * Finds the first name of the form `name (n).ext` that `exists` reports as free, or `undefined` when the first `limit` are taken.
 * A dot at the start of a name does not start an extension.
 */
export async function freeName(exists: (candidate: string) => Promise<boolean>, target: string, limit = maxRenames): Promise<string | undefined> {
  const slash = target.lastIndexOf('/')
  const dot = target.lastIndexOf('.') > slash + 1 ? target.lastIndexOf('.') : target.length
  for (let n = 1; n <= limit; n++) {
    const candidate = `${target.slice(0, dot)} (${n})${target.slice(dot)}`
    if (!(await exists(candidate))) {
      return candidate
    }
  }
  return undefined
}
