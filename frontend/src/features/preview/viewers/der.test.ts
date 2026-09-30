import { describe, expect, it } from 'vitest'

import { bitLength, childrenOf, DerError, hexOf, oidOf, parseDer, readNode, Tag, textOf, timeOf } from './der'

/** A value of the given tag and contents, with a short or a long length as needed. */
function tlv(tag: number, value: number[]): number[] {
  const length = value.length < 128 ? [value.length] : [0x82, value.length >> 8, value.length & 0xff]
  return [tag, ...length, ...value]
}

/** A node of the given tag and contents. */
function node(tag: number, value: number[]) {
  return parseDer(new Uint8Array(tlv(tag, value)))
}

/**
 * Covers the DER reader (D2.7).
 *
 * Scenarios:
 * - values with short and long lengths are read, with their children, and a whole encoding must be one value
 * - truncated values, high tag numbers, bad lengths and trailing bytes fail
 * - object identifiers, the string types of certificates and both kinds of time are read; other values fail as such
 * - bytes become colon hexadecimal, and integers tell their bits, leading zeros aside
 */
describe('der', () => {
  it('reads values and their children', () => {
    const long = new Array(200).fill(1)
    const sequence = parseDer(new Uint8Array(tlv(Tag.sequence, [...tlv(Tag.integer, [5]), ...tlv(Tag.octetString, long)])))
    const [integer, octets] = childrenOf(sequence)
    expect([integer.tag, integer.value[0], octets.value.length]).toEqual([Tag.integer, 5, 200])
    expect(readNode(new Uint8Array([...tlv(Tag.integer, [1]), 0xff]), 0).next).toBe(3)
  })

  it('refuses damaged encodings', () => {
    for (const bytes of [[0x30], [0x30, 0x05, 1], [0x1f, 0x01, 0], [0x30, 0x80], [0x30, 0x85, 1, 1, 1, 1, 1], [0x30, 0x82, 1], [0x02, 0x01, 0, 0]]) {
      expect(() => parseDer(new Uint8Array(bytes))).toThrow(DerError)
    }
  })

  it('reads identifiers, strings and times', () => {
    expect(oidOf(node(Tag.oid, [0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x01, 0x0b]))).toBe('1.2.840.113549.1.1.11')
    expect(oidOf(node(Tag.oid, [0x88, 0x37]))).toBe('2.999')
    expect(() => oidOf(node(Tag.integer, [1]))).toThrow(DerError)
    expect(() => oidOf(node(Tag.oid, []))).toThrow(DerError)
    expect(textOf(node(Tag.utf8, [0xc3, 0xb1]))).toBe('ñ')
    expect(textOf(node(Tag.bmp, [0, 0x41, 0, 0xf1]))).toBe('Añ')
    expect(textOf(node(Tag.t61, [0xf1]))).toBe('ñ')
    const ascii = (text: string) => [...text].map((char) => char.charCodeAt(0))
    expect(timeOf(node(Tag.utcTime, ascii('250102030405Z'))).toISOString()).toBe('2025-01-02T03:04:05.000Z')
    expect(timeOf(node(Tag.utcTime, ascii('9901020304Z'))).toISOString()).toBe('1999-01-02T03:04:00.000Z')
    expect(timeOf(node(Tag.generalizedTime, ascii('20500102030405Z'))).toISOString()).toBe('2050-01-02T03:04:05.000Z')
    expect(() => timeOf(node(Tag.utcTime, ascii('yesterday')))).toThrow(DerError)
    expect(() => timeOf(node(Tag.utf8, ascii('250102030405Z')))).toThrow(DerError)
  })

  it('writes hexadecimal and counts bits', () => {
    expect(hexOf(new Uint8Array([0, 0xab, 0x1]))).toBe('00:AB:01')
    expect(bitLength(new Uint8Array([0, 0x80, 0]))).toBe(16)
    expect(bitLength(new Uint8Array([0x01]))).toBe(1)
    expect(bitLength(new Uint8Array([0, 0]))).toBe(0)
    expect(bitLength(new Uint8Array([]))).toBe(0)
  })
})
