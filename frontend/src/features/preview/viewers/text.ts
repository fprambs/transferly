/** The largest text file shown as text, in bytes; past it, the hexadecimal view reads it by pages. */
export const textLimit = 5 * 1024 * 1024

/** Decodes the bytes of a text file as UTF-8, replacing what is not valid. */
export function decodeText(bytes: Uint8Array): string {
  return new TextDecoder('utf-8').decode(bytes)
}
