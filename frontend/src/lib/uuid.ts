/** Number of random bytes in a version 4 UUID. */
const uuidBytes = 16

/**
 * Creates a random version 4 UUID.
 *
 * `crypto.randomUUID` only exists in secure contexts, and Transferly may be served over plain HTTP inside a private network,
 * so this uses `crypto.getRandomValues`, which is always available.
 */
export function uuid(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(uuidBytes))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
