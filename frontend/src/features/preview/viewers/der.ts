/** A value of DER, the encoding of certificates and keys: its tag, where its contents are, and its children when it holds others. */
export interface DerNode {
  /** The tag byte: class, whether it is constructed, and number. */
  tag: number
  /** The contents, without the tag and the length. */
  value: Uint8Array
  /** The whole encoding, tag and length included. */
  whole: Uint8Array
}

/** A DER encoding that is damaged or goes past its bounds. */
export class DerError extends Error {
  /** Name of the error. */
  override name = 'DerError'
}

/** The tags of the universal types the reader looks for. */
export const Tag = {
  boolean: 0x01,
  integer: 0x02,
  bitString: 0x03,
  octetString: 0x04,
  oid: 0x06,
  utf8: 0x0c,
  printable: 0x13,
  t61: 0x14,
  ia5: 0x16,
  utcTime: 0x17,
  generalizedTime: 0x18,
  bmp: 0x1e,
  sequence: 0x30,
  set: 0x31,
} as const

/** Reads one value at `offset` of `bytes`, and tells where the next one starts. */
export function readNode(bytes: Uint8Array, offset = 0): { node: DerNode; next: number } {
  if (offset + 2 > bytes.length) {
    throw new DerError('truncated')
  }
  const tag = bytes[offset]
  if ((tag & 0x1f) === 0x1f) {
    throw new DerError('unsupported tag')
  }
  let length = bytes[offset + 1]
  let start = offset + 2
  if (length & 0x80) {
    const count = length & 0x7f
    if (count === 0 || count > 4 || start + count > bytes.length) {
      throw new DerError('bad length')
    }
    length = 0
    for (let index = 0; index < count; index++) {
      length = length * 256 + bytes[start + index]
    }
    start += count
  }
  const end = start + length
  if (end > bytes.length) {
    throw new DerError('truncated')
  }
  return { node: { tag, value: bytes.subarray(start, end), whole: bytes.subarray(offset, end) }, next: end }
}

/** Reads a whole encoding that must be exactly one value. */
export function parseDer(bytes: Uint8Array): DerNode {
  const { node, next } = readNode(bytes)
  if (next !== bytes.length) {
    throw new DerError('trailing bytes')
  }
  return node
}

/** Reads the values a constructed value holds. */
export function childrenOf(node: DerNode): DerNode[] {
  const children: DerNode[] = []
  for (let offset = 0; offset < node.value.length; ) {
    const read = readNode(node.value, offset)
    children.push(read.node)
    offset = read.next
  }
  return children
}

/** Reads an object identifier as its dotted numbers. */
export function oidOf(node: DerNode): string {
  if (node.tag !== Tag.oid || node.value.length === 0) {
    throw new DerError('not an object identifier')
  }
  const numbers: number[] = []
  let current = 0
  for (const byte of node.value) {
    current = current * 128 + (byte & 0x7f)
    if ((byte & 0x80) === 0) {
      numbers.push(current)
      current = 0
    }
  }
  const first = Math.min(Math.floor(numbers[0] / 40), 2)
  const parts = [first, numbers[0] - first * 40, ...numbers.slice(1)]
  return parts.join('.')
}

/** Reads a string of one of the types certificates use. */
export function textOf(node: DerNode): string {
  switch (node.tag) {
    case Tag.bmp:
      return new TextDecoder('utf-16be').decode(node.value)
    case Tag.t61:
      return new TextDecoder('latin1').decode(node.value)
    default:
      return new TextDecoder().decode(node.value)
  }
}

/** Reads a UTCTime or a GeneralizedTime as an instant. */
export function timeOf(node: DerNode): Date {
  const text = new TextDecoder().decode(node.value)
  const match = /^(\d{2}|\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})?Z$/.exec(text)
  if ((node.tag !== Tag.utcTime && node.tag !== Tag.generalizedTime) || match === null) {
    throw new DerError('not a time')
  }
  let year = Number(match[1])
  if (match[1].length === 2) {
    year += year >= 50 ? 1900 : 2000
  }
  return new Date(Date.UTC(year, Number(match[2]) - 1, Number(match[3]), Number(match[4]), Number(match[5]), Number(match[6] ?? '0')))
}

/** Writes bytes as hexadecimal pairs separated by colons, upper case. */
export function hexOf(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0').toUpperCase()).join(':')
}

/** Counts the bits of an unsigned big-endian integer, leading zero bytes aside. */
export function bitLength(bytes: Uint8Array): number {
  let start = 0
  while (start < bytes.length - 1 && bytes[start] === 0) {
    start++
  }
  const first = bytes[start]
  return first === undefined || first === 0 ? 0 : (bytes.length - start - 1) * 8 + (32 - Math.clz32(first))
}
