/** An origin no address of the application can have, to resolve candidate return paths against. */
const probeOrigin = 'https://transferly.invalid'

/**
 * Returns where to go back after signing in when `value` is a path of this application, such as `/cli/authorize?…`, and `undefined`
 * otherwise. Anything that would leave the origin is refused, so the login screen is never an open redirect: absolute addresses,
 * protocol-relative ones (`//host`, `/\host`) and paths that only become one once the browser drops tabs and newlines.
 */
export function returnPath(value: string | undefined): string | undefined {
  if (value === undefined || !value.startsWith('/')) {
    return undefined
  }
  const url = new URL(value, probeOrigin)
  return url.origin === probeOrigin ? url.pathname + url.search + url.hash : undefined
}
