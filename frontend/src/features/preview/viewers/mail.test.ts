import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { addressText, readMail } from './mail'

/** Reads a mail sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/mail', name)))
}

/** Writes a message by hand from its lines. */
function message(...lines: string[]): Uint8Array {
  return new TextEncoder().encode(lines.join('\r\n'))
}

/**
 * Covers {@link readMail} and {@link addressText} (D2.7).
 *
 * Scenarios:
 * - a newsletter tells its decoded subject, addresses, date, HTML with its picture put in place of its cid reference, text, header lines
 *   and the attachments its HTML does not show
 * - a hostile message tells that its HTML asks for the network
 * - pictures too large to put in place, or not shown by the HTML, stay attachments, and attachments without a name get one
 * - a plain message without a subject has neither subject nor HTML
 * - addresses are written with their names, without them, and as groups; missing addresses are empty
 */
describe('readMail', () => {
  it('reads a newsletter', async () => {
    const mail = await readMail(sample('newsletter.eml'))
    expect(mail.subject).toBe('Informe de septiembre · 📊 resultados')
    expect(mail.from).toBe('Transferly Samples <samples@example.com>')
    expect(mail.to).toBe('Ana <ana@example.com>, bruno@example.com')
    expect(mail.cc).toBe('Carla <carla@example.com>')
    expect(new Date(mail.date as string).toISOString()).toBe('2026-09-30T12:00:00.000Z')
    expect(mail.html).toMatch(/src='data:image\/png;base64,/)
    expect(mail.html).not.toContain('cid:')
    expect(mail.remote).toBe(false)
    expect(mail.text).toContain('Hola Ana')
    expect(mail.headers).toContain('Subject:')
    expect(mail.attachments.map((attachment) => [attachment.name, attachment.type])).toEqual([
      ['notas.txt', 'text/plain'],
      ['grafico.png', 'image/png'],
    ])
    expect(mail.attachments[1].data.length).toBeGreaterThan(1000)
  })

  it('tells when the HTML asks for the network', async () => {
    const mail = await readMail(sample('hostile.eml'))
    expect(mail.remote).toBe(true)
    expect(mail.subject).toBe('<img src=x onerror=alert(1)> urgent')
  })

  it('keeps large and unshown pictures as attachments', async () => {
    const big = Buffer.from(new Uint8Array(5 * 1024 * 1024 + 1)).toString('base64')
    const mail = await readMail(
      message(
        'From: undisclosed-recipients:;',
        'Subject: pictures',
        'MIME-Version: 1.0',
        'Content-Type: multipart/related; boundary="b"',
        '',
        '--b',
        'Content-Type: text/html',
        '',
        '<p><img src="cid:big@x"></p>',
        '--b',
        'Content-Type: image/png',
        'Content-ID: <big@x>',
        'Content-Transfer-Encoding: base64',
        '',
        big,
        '--b',
        'Content-Type: application/octet-stream',
        'Content-Disposition: attachment',
        'Content-Transfer-Encoding: base64',
        '',
        'AAAAAAAA',
        '--b--',
      ),
    )
    expect(mail.html).toContain('cid:big@x')
    expect(mail.attachments.map((attachment) => attachment.name)).toEqual(['attachment-1', 'attachment-2'])
    expect(mail.from).toBe('undisclosed-recipients: ')
    expect([mail.to, mail.cc, mail.text]).toEqual(['', '', undefined])
  })

  it('reads a plain message without a subject', async () => {
    const mail = await readMail(message('From: a@b', '', 'just text'))
    expect([mail.subject, mail.html, mail.remote, mail.text?.trim()]).toEqual(['', undefined, false, 'just text'])
  })

  it('writes addresses', () => {
    expect(addressText({ name: '', address: 'a@b' })).toBe('a@b')
    expect(addressText([{ name: 'Team', group: [{ name: 'Ana', address: 'ana@x' }, { name: '', address: 'bo@x' }] }])).toBe('Team: Ana <ana@x>, bo@x')
    expect(addressText(undefined)).toBe('')
  })

  it('reads a header of many encoded words in linear time, so a hostile message does not freeze the page', async () => {
    const words = '=?utf-8?B?QUJD?= '.repeat(100_000)
    const started = performance.now()

    const mail = await readMail(message(`Subject: ${words}`, '', 'x'))

    expect(mail.subject.startsWith('ABCABC')).toBe(true)
    expect(performance.now() - started).toBeLessThan(5_000)
  })
})
