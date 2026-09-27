import { createHmac } from 'node:crypto'

/** Alphabet of base32, as authenticator apps write secrets. */
const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'

/** Decodes a base32 secret without padding. */
function decode(secret: string): Buffer {
  let bits = ''
  for (const char of secret.toUpperCase()) {
    bits += alphabet.indexOf(char).toString(2).padStart(5, '0')
  }
  const bytes: number[] = []
  for (let i = 0; i + 8 <= bits.length; i += 8) {
    bytes.push(Number.parseInt(bits.slice(i, i + 8), 2))
  }
  return Buffer.from(bytes)
}

/** Computes the six digit code an authenticator app shows for a base32 secret, `steps` periods after now (RFC 6238, HMAC-SHA-1). */
export function totpCode(secret: string, steps = 0): string {
  const counter = Math.floor(Date.now() / 1000 / 30) + steps
  const message = Buffer.alloc(8)
  message.writeBigUInt64BE(BigInt(counter))
  const digest = createHmac('sha1', decode(secret)).update(message).digest()
  const offset = digest[digest.length - 1] & 0x0f
  const value = digest.readUInt32BE(offset) & 0x7fffffff
  return String(value % 1_000_000).padStart(6, '0')
}
