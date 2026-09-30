import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { readCertificate, readKeyFile, type CertificateInfo } from './x509'

/** Reads a certificate sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/certificates', name)))
}

/** A digest as colon hexadecimal, as the reader writes fingerprints. */
function digest(algorithm: string, bytes: Uint8Array): string {
  return (createHash(algorithm).update(bytes).digest('hex').toUpperCase().match(/../g) as string[]).join(':')
}

/** Wraps DER in a PEM block. */
function pem(label: string, bytes: Uint8Array | string): string {
  const body = typeof bytes === 'string' ? bytes : Buffer.from(bytes).toString('base64')
  return `-----BEGIN ${label}-----\n${body}\n-----END ${label}-----\n`
}

/** The DER of the first certificate of a PEM sample. */
function firstDer(name: string): Uint8Array {
  const text = new TextDecoder().decode(sample(name))
  const body = /-----BEGIN CERTIFICATE-----([\s\S]*?)-----END CERTIFICATE-----/.exec(text)?.[1] as string
  return new Uint8Array(Buffer.from(body.replace(/\s+/g, ''), 'base64'))
}

/**
 * Covers the reader of certificates and keys (D2.7).
 *
 * Scenarios:
 * - a chain in PEM tells each certificate: subject, issuer, serial, validity, key, signature, other names, usages, whether it is an
 *   authority and self-signed, and fingerprints that match the digests of its DER
 * - a certificate in DER, an elliptic curve certificate and an expired one are read
 * - a public key and an OpenSSH key tell their algorithm, size and fingerprint, as ssh-keygen writes it
 * - private keys are told by their label and never read; damaged, unknown and mismatched blocks are unreadable, and a file of no known
 *   form is an unreadable DER
 * - an Ed25519 certificate tells unusual names, usages and attributes, a DSA one its algorithms by their identifiers, and a key line
 *   without its data is unreadable
 * - RSA public keys in PKCS #1, certificate requests, RSA and ECDSA OpenSSH keys, keys of unknown algorithms, and certificates without
 *   extensions or with unknown ones are read
 */
describe('readKeyFile', () => {
  it('reads a chain of certificates', () => {
    const [leaf, root] = readKeyFile(sample('chain.pem')) as CertificateInfo[]
    expect(leaf.subject).toEqual([
      { name: 'C', value: 'CL' },
      { name: 'ST', value: 'RM' },
      { name: 'L', value: 'Santiago' },
      { name: 'O', value: 'Sentinel Samples' },
      { name: 'CN', value: 'files.example.com' },
    ])
    expect(leaf.issuer.at(-1)).toEqual({ name: 'CN', value: 'Sentinel Sample Root CA' })
    expect(leaf.key).toEqual({ algorithm: 'RSA', bits: 2048 })
    expect(leaf.signature).toBe('SHA-256 with RSA')
    expect(leaf.altNames).toEqual(['files.example.com', '*.files.example.com', '192.0.2.10'])
    expect(leaf.keyUsage).toEqual(['digitalSignature', 'keyEncipherment'])
    expect(leaf.extendedKeyUsage).toEqual(['serverAuth'])
    expect([leaf.authority, leaf.selfSigned]).toEqual([false, false])
    expect(leaf.notAfter.getTime() - leaf.notBefore.getTime()).toBe(825 * 86_400_000)
    expect(leaf.serial).toMatch(/^[0-9A-F]{2}(:[0-9A-F]{2})+$/)
    expect([root.authority, root.selfSigned]).toEqual([true, true])
    const der = sample('leaf.cer')
    const fromDer = readKeyFile(der)[0] as CertificateInfo
    expect([fromDer.sha256, fromDer.sha1]).toEqual([digest('sha256', der), digest('sha1', der)])
    expect(fromDer.serial).toBe(leaf.serial)
  })

  it('reads elliptic curve and expired certificates', () => {
    const [ec] = readKeyFile(sample('ec.crt')) as CertificateInfo[]
    expect([ec.key, ec.signature]).toEqual([{ algorithm: 'EC', curve: 'P-256' }, 'ECDSA with SHA-256'])
    const [expired] = readKeyFile(sample('expired.pem')) as CertificateInfo[]
    expect(expired.notAfter.toISOString()).toBe('2021-01-01T00:00:00.000Z')
    expect(expired.altNames).toEqual([])
  })

  it('reads public keys and hides private ones', () => {
    const publicKey = readKeyFile(sample('public-key.pem'))[0]
    expect(publicKey).toMatchObject({ kind: 'publicKey', key: { algorithm: 'EC', curve: 'P-256' } })
    const line = new TextDecoder().decode(sample('id_ed25519.pub'))
    const blob = Buffer.from(line.split(' ')[1], 'base64')
    expect(readKeyFile(sample('id_ed25519.pub'))).toEqual([
      { kind: 'sshKey', type: 'ssh-ed25519', key: { algorithm: 'Ed25519', bits: 256 }, comment: 'sample@sentinel', fingerprint: `SHA256:${createHash('sha256').update(blob).digest('base64').replace(/=+$/, '')}` },
    ])
    expect(readKeyFile(sample('private-key.pem'))).toEqual([{ kind: 'privateKey', label: 'PRIVATE KEY' }])
    expect(readKeyFile(sample('broken.pem'))).toEqual([{ kind: 'unreadable', label: 'CERTIFICATE' }])
    expect(readKeyFile(new TextEncoder().encode(pem('X509 CRL', 'AAAA')))).toEqual([{ kind: 'unreadable', label: 'X509 CRL' }])
    expect(readKeyFile(new TextEncoder().encode(pem('CERTIFICATE', 'AAAA')))).toEqual([{ kind: 'unreadable', label: 'CERTIFICATE' }])
    expect(readKeyFile(new TextEncoder().encode('ssh-rsa !!notbase64'))).toEqual([{ kind: 'unreadable', label: 'ssh-rsa' }])
    expect(readKeyFile(new TextEncoder().encode(`ssh-rsa ${line.split(' ')[1]}`))).toEqual([{ kind: 'unreadable', label: 'ssh-rsa' }])
    expect(readKeyFile(new Uint8Array([1, 2, 3]))).toEqual([{ kind: 'unreadable', label: 'DER' }])
  })

  it('reads unusual certificates', () => {
    const [unusual] = readKeyFile(sample('unusual.pem')) as CertificateInfo[]
    expect(unusual.subject[0]).toEqual({ name: '2.5.4.97', value: 'VATCL-12345678' })
    expect(unusual.key).toEqual({ algorithm: 'Ed25519', bits: 256 })
    expect(unusual.altNames).toEqual(['ops@example.com', 'https://example.com/id', '20:01:0D:B8:00:00:00:00:00:00:00:00:00:00:00:01'])
    expect(unusual.extendedKeyUsage).toEqual(['1.2.3.4'])
    expect(unusual.authority).toBeUndefined()
    const [dsa] = readKeyFile(sample('dsa.pem')) as CertificateInfo[]
    expect([dsa.signature, dsa.key]).toEqual(['2.16.840.1.101.3.4.3.2', { algorithm: '1.2.840.10040.4.1', bits: undefined }])
    expect(readKeyFile(new TextEncoder().encode('ssh-ed25519'))).toEqual([{ kind: 'unreadable', label: 'ssh-ed25519' }])
  })

  it('reads the other kinds of keys and requests', () => {
    const der = firstDer('chain.pem')
    const certificate = readCertificate(der)
    const rsaKey = extractRsa(der)
    expect(readKeyFile(new TextEncoder().encode(pem('RSA PUBLIC KEY', rsaKey)))[0]).toMatchObject({ kind: 'publicKey', key: { algorithm: 'RSA', bits: 2048 } })
    expect(readKeyFile(new TextEncoder().encode(pem('CERTIFICATE REQUEST', request(der))))[0]).toEqual({ kind: 'request', subject: certificate.subject, key: certificate.key })
    const rsaBlob = sshBlob(['ssh-rsa', new Uint8Array([1, 0, 1]), new Uint8Array([0, 0xc0, ...new Array(255).fill(1)])])
    expect(readKeyFile(new TextEncoder().encode(`ssh-rsa ${rsaBlob}\n# comment\n`))[0]).toMatchObject({ kind: 'sshKey', key: { algorithm: 'RSA', bits: 2048 }, comment: '' })
    const ecBlob = sshBlob(['ecdsa-sha2-nistp256', 'nistp256', new Uint8Array(65)])
    expect(readKeyFile(new TextEncoder().encode(`ecdsa-sha2-nistp256 ${ecBlob} a b`))[0]).toMatchObject({ key: { algorithm: 'ECDSA', curve: 'nistp256' }, comment: 'a b' })
    const skBlob = sshBlob(['sk-ssh-ed25519@openssh.com', new Uint8Array(32)])
    expect(readKeyFile(new TextEncoder().encode(`sk-ssh-ed25519@openssh.com ${skBlob}`))[0]).toMatchObject({ key: { algorithm: 'sk-ssh-ed25519@openssh.com' } })
    const unknownKey = spki([0x2b, 0x65, 0x71], [])
    expect(readKeyFile(new TextEncoder().encode(pem('PUBLIC KEY', unknownKey)))[0]).toMatchObject({ key: { algorithm: 'Ed448' } })
    const oddKey = spki([0x2a, 0x03], [])
    expect(readKeyFile(new TextEncoder().encode(pem('PUBLIC KEY', oddKey)))[0]).toMatchObject({ key: { algorithm: '1.2.3' } })
    const namedCurve = spki([0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01], [0x06, 0x05, 0x2b, 0x81, 0x04, 0x00, 0x22])
    expect(readKeyFile(new TextEncoder().encode(pem('PUBLIC KEY', namedCurve)))[0]).toMatchObject({ key: { algorithm: 'EC', curve: 'P-384' } })
    const otherCurve = spki([0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01], [0x06, 0x02, 0x2a, 0x03])
    expect(readKeyFile(new TextEncoder().encode(pem('PUBLIC KEY', otherCurve)))[0]).toMatchObject({ key: { algorithm: 'EC', curve: '1.2.3' } })
    const implicitCurve = spki([0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01], [0x05, 0x00])
    expect(readKeyFile(new TextEncoder().encode(pem('PUBLIC KEY', implicitCurve)))[0]).toMatchObject({ key: { algorithm: 'EC', curve: undefined } })
  })
})

/** Encodes a value of DER. */
function tlv(tag: number, value: number[]): number[] {
  const length = value.length < 128 ? [value.length] : value.length < 256 ? [0x81, value.length] : [0x82, value.length >> 8, value.length & 0xff]
  return [tag, ...length, ...value]
}

/** A SubjectPublicKeyInfo of an algorithm, with optional parameters and an empty key. */
function spki(oid: number[], parameters: number[]): Uint8Array {
  return new Uint8Array(tlv(0x30, [...tlv(0x30, [...tlv(0x06, oid), ...parameters]), ...tlv(0x03, [0])]))
}

/** Reads the RSA key of a certificate as PKCS #1. */
function extractRsa(der: Uint8Array): Uint8Array {
  const text = Buffer.from(der)
  const marker = Buffer.from([0x03, 0x82, 0x01, 0x0f, 0x00])
  const at = text.indexOf(marker)
  return new Uint8Array(text.subarray(at + marker.length, at + marker.length + 0x10e))
}

/** Builds a certificate request from the fields of a certificate: version, subject and key, with a dummy signature. */
function request(der: Uint8Array): Uint8Array {
  const certificate = Buffer.from(der)
  const subjectStart = certificate.indexOf(Buffer.from([0x31, 0x0b, 0x30, 0x09, 0x06, 0x03, 0x55, 0x04, 0x06, 0x13, 0x02, 0x43, 0x4c, 0x31, 0x0b]))
  const subject = readSequence(certificate, subjectStart - 2)
  const key = readSequence(certificate, subjectStart - 2 + subject.length)
  const info = tlv(0x30, [...tlv(0x02, [0]), ...subject, ...key, 0xa0, 0x00])
  return new Uint8Array(tlv(0x30, [...info, ...tlv(0x30, [...tlv(0x06, [0x2a, 0x03])]), ...tlv(0x03, [0])]))
}

/** Reads the whole encoding of the sequence at `at`. */
function readSequence(bytes: Buffer, at: number): number[] {
  const first = bytes[at + 1]
  const header = first & 0x80 ? 2 + (first & 0x7f) : 2
  const length = first & 0x80 ? bytes.subarray(at + 2, at + header).reduce((sum, byte) => sum * 256 + byte, 0) : first
  return Array.from(bytes.subarray(at, at + header + length))
}

/** An OpenSSH key blob of the given fields, in base64. */
function sshBlob(fields: Array<string | Uint8Array>): string {
  const parts = fields.map((field) => (typeof field === 'string' ? new TextEncoder().encode(field) : field))
  const out: number[] = []
  for (const part of parts) {
    out.push(part.length >>> 24, (part.length >>> 16) & 0xff, (part.length >>> 8) & 0xff, part.length & 0xff, ...part)
  }
  return Buffer.from(out).toString('base64')
}
