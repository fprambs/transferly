import PostalMime, { type Address, type Attachment } from 'postal-mime'

/** An attachment of a message, ready to show or save. */
export interface MailAttachment {
  /** Its name, or a name made from its position when it has none. */
  name: string
  /** Its media type. */
  type: string
  /** Its bytes. */
  data: Uint8Array
}

/** A message read for the viewer. */
export interface Mail {
  /** Its subject. */
  subject: string
  /** Who sent it, as text. */
  from: string
  /** Whom it was sent to, as text. */
  to: string
  /** Who got a copy, as text. */
  cc: string
  /** When it was sent, as the message says it. */
  date?: string
  /** Its HTML body, with pictures of the message put in place of their `cid:` references, when it has one. */
  html?: string
  /** Whether its HTML asks for anything from the network, which the viewer never loads. */
  remote: boolean
  /** Its plain text body, when it has one. */
  text?: string
  /** Its header lines, as they are written. */
  headers: string
  /** Its attachments, the pictures its HTML shows aside. */
  attachments: MailAttachment[]
}

/** The largest picture of a message put into its HTML, since it goes as a data address. */
const maxInlinePicture = 5 * 1024 * 1024

/** Writes addresses as text, with their names when they have one, and groups with their members. */
export function addressText(addresses: Address[] | Address | undefined): string {
  const list = addresses === undefined ? [] : Array.isArray(addresses) ? addresses : [addresses]
  return list
    .map((address) =>
      address.group !== undefined
        ? `${address.name}: ${addressText(address.group)}`
        : address.name === '' ? address.address : `${address.name} <${address.address}>`,
    )
    .join(', ')
}

/** The bytes of an attachment, which postal-mime reads as an ArrayBuffer when asked to. */
function bytesOf(attachment: Attachment): Uint8Array {
  return new Uint8Array(attachment.content as ArrayBuffer)
}

/** A data address for bytes of a media type. */
function dataAddress(type: string, data: Uint8Array): string {
  let binary = ''
  for (let at = 0; at < data.length; at += 0x8000) {
    binary += String.fromCharCode(...data.subarray(at, at + 0x8000))
  }
  return `data:${type};base64,${btoa(binary)}`
}

/**
 * Reads a message (D2.7) with postal-mime: its subject, addresses, date, bodies, header lines and attachments. Pictures its HTML shows by
 * `cid:` references are put in place as data addresses, up to 5 MiB each, and left out of the attachments; the HTML is not cleaned here,
 * since the isolated page cleans it before drawing it. Whether the HTML asks for anything from the network is told, for the viewer to say
 * it was not loaded.
 */
export async function readMail(bytes: Uint8Array): Promise<Mail> {
  const email = await PostalMime.parse(bytes, { attachmentEncoding: 'arraybuffer' })
  let html = email.html
  const shown = new Set<Attachment>()
  for (const attachment of email.attachments) {
    const id = attachment.contentId?.replace(/^<|>$/g, '')
    const data = bytesOf(attachment)
    if (html !== undefined && id !== undefined && attachment.mimeType.startsWith('image/') && data.length <= maxInlinePicture && html.includes(`cid:${id}`)) {
      html = html.replaceAll(`cid:${id}`, dataAddress(attachment.mimeType, data))
      shown.add(attachment)
    }
  }
  return {
    subject: email.subject ?? '',
    from: addressText(email.from),
    to: addressText(email.to),
    cc: addressText(email.cc),
    date: email.date,
    html,
    remote: html !== undefined && /(?:src|href|background|poster)\s*=\s*["']?\s*https?:|url\(\s*["']?\s*https?:/i.test(html),
    text: email.text,
    headers: email.headerLines.map((header) => header.line).join('\n'),
    attachments: email.attachments
      .filter((attachment) => !shown.has(attachment))
      .map((attachment, index) => ({ name: attachment.filename ?? `attachment-${index + 1}`, type: attachment.mimeType, data: bytesOf(attachment) })),
  }
}
