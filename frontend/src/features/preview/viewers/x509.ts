import { sha1 } from '@noble/hashes/legacy.js'
import { sha256 } from '@noble/hashes/sha2.js'

import { bitLength, childrenOf, DerError, hexOf, oidOf, parseDer, Tag, textOf, timeOf, type DerNode } from './der'

/** An attribute of a distinguished name: its short name, such as CN, and its value. */
export interface NameAttribute {
  /** Short name of the attribute, or its object identifier when it has none. */
  name: string
  /** Its value. */
  value: string
}

/** A public key: its algorithm and size or curve. */
export interface KeyInfo {
  /** The algorithm, such as RSA, EC or Ed25519. */
  algorithm: string
  /** The size in bits, when the algorithm has one. */
  bits?: number
  /** The named curve of an elliptic curve key. */
  curve?: string
}

/** What a certificate says. */
export interface CertificateInfo {
  /** Tells the item apart. */
  kind: 'certificate'
  /** Who it names. */
  subject: NameAttribute[]
  /** Who signed it. */
  issuer: NameAttribute[]
  /** Its serial number, in hexadecimal. */
  serial: string
  /** When it starts being valid. */
  notBefore: Date
  /** When it stops being valid. */
  notAfter: Date
  /** The algorithm of its signature. */
  signature: string
  /** Its public key. */
  key: KeyInfo
  /** The other names it is valid for: DNS names, addresses, mails and URIs. */
  altNames: string[]
  /** What its key may be used for. */
  keyUsage: string[]
  /** The purposes it may serve. */
  extendedKeyUsage: string[]
  /** Whether it may sign other certificates, when it says. */
  authority?: boolean
  /** Whether its subject and issuer are the same. */
  selfSigned: boolean
  /** The SHA-256 fingerprint of the whole certificate. */
  sha256: string
  /** The SHA-1 fingerprint of the whole certificate. */
  sha1: string
}

/** One thing a certificate or key file holds. */
export type KeyFileItem =
  | CertificateInfo
  | { kind: 'publicKey'; key: KeyInfo; sha256: string }
  | { kind: 'request'; subject: NameAttribute[]; key: KeyInfo }
  | { kind: 'sshKey'; type: string; key: KeyInfo; comment: string; fingerprint: string }
  | { kind: 'privateKey'; label: string }
  | { kind: 'unreadable'; label: string }

/** Short names of the attributes of distinguished names. */
const attributes: Record<string, string> = {
  '2.5.4.3': 'CN',
  '2.5.4.5': 'serialNumber',
  '2.5.4.6': 'C',
  '2.5.4.7': 'L',
  '2.5.4.8': 'ST',
  '2.5.4.10': 'O',
  '2.5.4.11': 'OU',
  '1.2.840.113549.1.9.1': 'emailAddress',
}

/** Names of the algorithms of signatures and keys. */
const algorithms: Record<string, string> = {
  '1.2.840.113549.1.1.1': 'RSA',
  '1.2.840.113549.1.1.5': 'SHA-1 with RSA',
  '1.2.840.113549.1.1.10': 'RSA-PSS',
  '1.2.840.113549.1.1.11': 'SHA-256 with RSA',
  '1.2.840.113549.1.1.12': 'SHA-384 with RSA',
  '1.2.840.113549.1.1.13': 'SHA-512 with RSA',
  '1.2.840.10045.2.1': 'EC',
  '1.2.840.10045.4.3.2': 'ECDSA with SHA-256',
  '1.2.840.10045.4.3.3': 'ECDSA with SHA-384',
  '1.2.840.10045.4.3.4': 'ECDSA with SHA-512',
  '1.3.101.112': 'Ed25519',
  '1.3.101.113': 'Ed448',
}

/** Names of the elliptic curves. */
const curves: Record<string, string> = { '1.2.840.10045.3.1.7': 'P-256', '1.3.132.0.34': 'P-384', '1.3.132.0.35': 'P-521' }

/** Names of the purposes of extended key usage. */
const purposes: Record<string, string> = {
  '1.3.6.1.5.5.7.3.1': 'serverAuth',
  '1.3.6.1.5.5.7.3.2': 'clientAuth',
  '1.3.6.1.5.5.7.3.3': 'codeSigning',
  '1.3.6.1.5.5.7.3.4': 'emailProtection',
  '1.3.6.1.5.5.7.3.8': 'timeStamping',
  '1.3.6.1.5.5.7.3.9': 'OCSPSigning',
}

/** Names of the bits of key usage, in order. */
const usages = ['digitalSignature', 'nonRepudiation', 'keyEncipherment', 'dataEncipherment', 'keyAgreement', 'keyCertSign', 'cRLSign', 'encipherOnly', 'decipherOnly']

/** Writes bytes as the hexadecimal fingerprint of a digest. */
function fingerprint(digest: (bytes: Uint8Array) => Uint8Array, bytes: Uint8Array): string {
  return hexOf(digest(bytes))
}

/** Reads a distinguished name into its attributes. */
function readName(node: DerNode): NameAttribute[] {
  return childrenOf(node).flatMap((set) =>
    childrenOf(set).map((pair) => {
      const [type, value] = childrenOf(pair)
      const oid = oidOf(type)
      return { name: attributes[oid] ?? oid, value: textOf(value) }
    }),
  )
}

/** The name of an algorithm identifier, or its object identifier when the reader does not know it. */
function algorithmOf(node: DerNode): string {
  const oid = oidOf(childrenOf(node)[0])
  return algorithms[oid] ?? oid
}

/** Reads a SubjectPublicKeyInfo: its algorithm, and the size of an RSA key or the curve of an EC one. */
export function readPublicKey(node: DerNode): KeyInfo {
  const [identifier, key] = childrenOf(node)
  const [algorithm, parameters] = childrenOf(identifier)
  const name = algorithms[oidOf(algorithm)] ?? oidOf(algorithm)
  if (name === 'RSA') {
    return { algorithm: name, bits: bitLength(childrenOf(parseDer(key.value.subarray(1)))[0].value) }
  }
  if (name === 'EC') {
    const curve = parameters?.tag === Tag.oid ? oidOf(parameters) : undefined
    return { algorithm: name, curve: curve === undefined ? undefined : (curves[curve] ?? curve) }
  }
  return { algorithm: name, bits: name === 'Ed25519' ? 256 : undefined }
}

/** Reads the other names of a certificate: DNS names, addresses, mails and URIs. */
function readAltNames(value: Uint8Array): string[] {
  return childrenOf(parseDer(value)).flatMap((name) => {
    const number = name.tag & 0x1f
    if (number === 7) {
      return [name.value.length === 4 ? Array.from(name.value).join('.') : hexOf(name.value)]
    }
    return number === 1 || number === 2 || number === 6 ? [new TextDecoder().decode(name.value)] : []
  })
}

/** Reads the bits of a key usage extension into their names. */
function readKeyUsage(value: Uint8Array): string[] {
  const bits = parseDer(value).value
  return usages.filter((_, index) => (bits[1 + Math.floor(index / 8)] ?? 0) & (0x80 >> index % 8))
}

/** Reads a certificate from its DER encoding. */
export function readCertificate(bytes: Uint8Array): CertificateInfo {
  const [tbs, signature] = childrenOf(parseDer(bytes))
  const fields = childrenOf(tbs)
  const at = fields[0].tag === 0xa0 ? 1 : 0
  const [serial, , issuer, validity, subject, key] = fields.slice(at)
  const [notBefore, notAfter] = childrenOf(validity)
  const info: CertificateInfo = {
    kind: 'certificate',
    subject: readName(subject),
    issuer: readName(issuer),
    serial: hexOf(serial.value),
    notBefore: timeOf(notBefore),
    notAfter: timeOf(notAfter),
    signature: algorithmOf(signature),
    key: readPublicKey(key),
    altNames: [],
    keyUsage: [],
    extendedKeyUsage: [],
    selfSigned: hexOf(subject.whole) === hexOf(issuer.whole),
    sha256: fingerprint(sha256, bytes),
    sha1: fingerprint(sha1, bytes),
  }
  const extensions = fields.find((field) => field.tag === 0xa3)
  for (const extension of extensions === undefined ? [] : childrenOf(childrenOf(extensions)[0])) {
    const parts = childrenOf(extension)
    const oid = oidOf(parts[0])
    const value = parts[parts.length - 1].value
    if (oid === '2.5.29.17') {
      info.altNames = readAltNames(value)
    } else if (oid === '2.5.29.15') {
      info.keyUsage = readKeyUsage(value)
    } else if (oid === '2.5.29.37') {
      info.extendedKeyUsage = childrenOf(parseDer(value)).map((purpose) => purposes[oidOf(purpose)] ?? oidOf(purpose))
    } else if (oid === '2.5.29.19') {
      const flag = childrenOf(parseDer(value))[0]
      info.authority = flag?.tag === Tag.boolean && flag.value[0] !== 0
    }
  }
  return info
}

/** Reads a certificate request from its DER encoding: whom it names and its key. */
function readRequest(bytes: Uint8Array): KeyFileItem {
  const [info] = childrenOf(parseDer(bytes))
  const [, subject, key] = childrenOf(info)
  return { kind: 'request', subject: readName(subject), key: readPublicKey(key) }
}

/** Reads a PKCS #1 RSA public key: its size. */
function readRsaPublicKey(bytes: Uint8Array): KeyFileItem {
  return { kind: 'publicKey', key: { algorithm: 'RSA', bits: bitLength(childrenOf(parseDer(bytes))[0].value) }, sha256: fingerprint(sha256, bytes) }
}

/** Decodes base64, refusing anything that is not. */
function decodeBase64(text: string): Uint8Array {
  const clean = text.replace(/\s+/g, '')
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(clean)) {
    throw new DerError('not base64')
  }
  return Uint8Array.from(atob(clean), (char) => char.charCodeAt(0))
}

/** Reads one block of a PEM file by its label, never reading a private key. */
function readPemBlock(label: string, body: string): KeyFileItem {
  if (label.includes('PRIVATE KEY')) {
    return { kind: 'privateKey', label }
  }
  try {
    const bytes = decodeBase64(body)
    switch (label) {
      case 'CERTIFICATE':
      case 'TRUSTED CERTIFICATE':
        return readCertificate(bytes)
      case 'PUBLIC KEY':
        return { kind: 'publicKey', key: readPublicKey(parseDer(bytes)), sha256: fingerprint(sha256, bytes) }
      case 'RSA PUBLIC KEY':
        return readRsaPublicKey(bytes)
      case 'CERTIFICATE REQUEST':
      case 'NEW CERTIFICATE REQUEST':
        return readRequest(bytes)
      default:
        return { kind: 'unreadable', label }
    }
  } catch {
    return { kind: 'unreadable', label }
  }
}

/** Reads the fields of an OpenSSH key blob: strings, each with its length first. */
function sshFields(blob: Uint8Array): Uint8Array[] {
  const view = new DataView(blob.buffer, blob.byteOffset, blob.byteLength)
  const fields: Uint8Array[] = []
  for (let offset = 0; offset + 4 <= blob.length; ) {
    const length = view.getUint32(offset)
    fields.push(blob.subarray(offset + 4, offset + 4 + length))
    offset += 4 + length
  }
  return fields
}

/** Reads an OpenSSH public key line: its type, size, comment and SHA-256 fingerprint, as ssh-keygen writes it. */
function readSshKey(line: string): KeyFileItem {
  const [type, data, ...comment] = line.trim().split(/\s+/)
  try {
    const blob = decodeBase64(data ?? '')
    const fields = sshFields(blob)
    if (new TextDecoder().decode(fields[0]) !== type) {
      throw new DerError('type does not match')
    }
    const key: KeyInfo =
      type === 'ssh-rsa'
        ? { algorithm: 'RSA', bits: bitLength(fields[2]) }
        : type === 'ssh-ed25519'
          ? { algorithm: 'Ed25519', bits: 256 }
          : { algorithm: type.includes('ecdsa') ? 'ECDSA' : type, curve: type.startsWith('ecdsa-sha2-') ? new TextDecoder().decode(fields[1]) : undefined }
    const digest = btoa(String.fromCharCode(...sha256(blob))).replace(/=+$/, '')
    return { kind: 'sshKey', type, key, comment: comment.join(' '), fingerprint: `SHA256:${digest}` }
  } catch {
    return { kind: 'unreadable', label: type }
  }
}

/**
 * Reads what a certificate or key file holds (D2.7): every block of a PEM file (certificates, public keys, certificate requests), a
 * certificate in DER, or OpenSSH public keys, one per line. Private keys are recognized but never read: only their label is told. A block
 * that cannot be read is told as unreadable, so one damaged block does not hide the others.
 */
export function readKeyFile(bytes: Uint8Array): KeyFileItem[] {
  const text = new TextDecoder().decode(bytes)
  const blocks = Array.from(text.matchAll(/-----BEGIN ([A-Z0-9 ]+)-----([\s\S]*?)-----END \1-----/g))
  if (blocks.length > 0) {
    return blocks.map(([, label, body]) => readPemBlock(label, body))
  }
  const lines = text.split('\n').filter((line) => /^(ssh-|ecdsa-|sk-)/.test(line.trim()))
  if (lines.length > 0) {
    return lines.map(readSshKey)
  }
  try {
    return [readCertificate(bytes)]
  } catch {
    return [{ kind: 'unreadable', label: 'DER' }]
  }
}
