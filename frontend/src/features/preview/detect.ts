/** How many leading bytes the detection reads. */
export const headLength = 4096

/**
 * The viewer a file opens in (ADR-0018). `binary` is every file no other viewer takes, shown by the hexadecimal viewer, which is also the
 * fallback of every other kind when its file is too large or cannot be decoded.
 */
export type PreviewKind =
  | 'text'
  | 'markdown'
  | 'json'
  | 'yaml'
  | 'xml'
  | 'csv'
  | 'image'
  | 'svg'
  | 'audio'
  | 'video'
  | 'pdf'
  | 'archive'
  | 'document'
  | 'spreadsheet'
  | 'presentation'
  | 'sqlite'
  | 'font'
  | 'certificate'
  | 'email'
  | 'binary'

/** A signature at the start of a file, or at an offset, and the kind it proves. */
interface Signature {
  /** The bytes, with `null` for a byte that may be anything. */
  bytes: readonly (number | null)[]
  /** Where the bytes start. */
  offset?: number
  /** The kind the bytes prove. */
  kind: PreviewKind
}

/** Turns text into its bytes, for signatures written as text. */
function ascii(text: string): number[] {
  return [...text].map((char) => char.charCodeAt(0))
}

/** Signatures of the formats a file can be recognized by, whatever its extension. Containers shared by several formats, such as ZIP, are
 * refined by the extension afterwards. */
const signatures: readonly Signature[] = [
  { bytes: ascii('%PDF-'), kind: 'pdf' },
  { bytes: [0x89, ...ascii('PNG')], kind: 'image' },
  { bytes: [0xff, 0xd8, 0xff], kind: 'image' },
  { bytes: ascii('GIF8'), kind: 'image' },
  { bytes: [...ascii('RIFF'), null, null, null, null, ...ascii('WEBP')], kind: 'image' },
  { bytes: [null, null, null, null, ...ascii('ftypavif')], kind: 'image' },
  { bytes: ascii('BM'), kind: 'image' },
  { bytes: [0x00, 0x00, 0x01, 0x00], kind: 'image' },
  { bytes: [0x49, 0x49, 0x2a, 0x00], kind: 'image' },
  { bytes: [0x4d, 0x4d, 0x00, 0x2a], kind: 'image' },
  { bytes: ascii('ID3'), kind: 'audio' },
  { bytes: [0xff, 0xfb], kind: 'audio' },
  { bytes: ascii('OggS'), kind: 'audio' },
  { bytes: ascii('fLaC'), kind: 'audio' },
  { bytes: [...ascii('RIFF'), null, null, null, null, ...ascii('WAVE')], kind: 'audio' },
  { bytes: [...ascii('RIFF'), null, null, null, null, ...ascii('AVI ')], kind: 'video' },
  { bytes: [null, null, null, null, ...ascii('ftypM4A')], kind: 'audio' },
  { bytes: [null, null, null, null, ...ascii('ftyp')], kind: 'video' },
  { bytes: [0x1a, 0x45, 0xdf, 0xa3], kind: 'video' },
  { bytes: [...ascii('PK'), 0x03, 0x04], kind: 'archive' },
  { bytes: [...ascii('PK'), 0x05, 0x06], kind: 'archive' },
  { bytes: [0x1f, 0x8b], kind: 'archive' },
  { bytes: ascii('BZh'), kind: 'archive' },
  { bytes: [0xfd, ...ascii('7zXZ'), 0x00], kind: 'archive' },
  { bytes: [0x28, 0xb5, 0x2f, 0xfd], kind: 'archive' },
  { bytes: [...ascii('7z'), 0xbc, 0xaf, 0x27, 0x1c], kind: 'archive' },
  { bytes: ascii('Rar!'), kind: 'archive' },
  { bytes: ascii('ustar'), offset: 257, kind: 'archive' },
  { bytes: [...ascii('SQLite format 3'), 0x00], kind: 'sqlite' },
  { bytes: ascii('wOFF'), kind: 'font' },
  { bytes: ascii('wOF2'), kind: 'font' },
  { bytes: ascii('OTTO'), kind: 'font' },
  { bytes: [0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1], kind: 'binary' },
]

/** Kinds told by the extension alone, lower case. */
const byExtension: Readonly<Record<string, PreviewKind>> = {
  md: 'markdown', markdown: 'markdown',
  json: 'json', jsonl: 'json', geojson: 'json',
  yaml: 'yaml', yml: 'yaml',
  xml: 'xml', xsd: 'xml', xsl: 'xml', plist: 'xml', rss: 'xml',
  csv: 'csv', tsv: 'csv',
  svg: 'svg',
  png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image', avif: 'image', bmp: 'image', ico: 'image', tif: 'image', tiff: 'image',
  mp3: 'audio', wav: 'audio', ogg: 'audio', oga: 'audio', opus: 'audio', flac: 'audio', m4a: 'audio', aac: 'audio',
  mp4: 'video', m4v: 'video', webm: 'video', mov: 'video', mkv: 'video', avi: 'video',
  pdf: 'pdf',
  zip: 'archive', jar: 'archive', war: 'archive', apk: 'archive', tar: 'archive', tgz: 'archive', gz: 'archive', bz2: 'archive', xz: 'archive', zst: 'archive', '7z': 'archive', rar: 'archive',
  docx: 'document', odt: 'document',
  xlsx: 'spreadsheet', xls: 'spreadsheet', ods: 'spreadsheet',
  pptx: 'presentation', odp: 'presentation',
  sqlite: 'sqlite', sqlite3: 'sqlite', db: 'sqlite',
  ttf: 'font', otf: 'font', woff: 'font', woff2: 'font',
  pem: 'certificate', crt: 'certificate', cer: 'certificate', der: 'certificate',
  eml: 'email',
}

/** Kinds that a ZIP container holds under another name. */
const zipBased: ReadonlySet<PreviewKind> = new Set(['document', 'spreadsheet', 'presentation'])

/** Kinds whose files are not text, which a binary file keeps from its extension when no signature tells otherwise. */
const binaryKinds: ReadonlySet<PreviewKind> = new Set(['image', 'audio', 'video', 'pdf', 'archive', 'document', 'spreadsheet', 'presentation', 'sqlite', 'font', 'certificate'])

/** Kinds a file keeps from its extension when its content reads as text. */
const textKinds: ReadonlySet<PreviewKind> = new Set(['markdown', 'json', 'yaml', 'xml', 'csv', 'svg', 'email', 'certificate'])

/** Returns the extension of a name, lower case, or an empty string. */
function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot <= 0 ? '' : name.slice(dot + 1).toLowerCase()
}

/** Tells whether the bytes of a signature are at its place in `head`. */
function matches(head: Uint8Array, signature: Signature): boolean {
  const offset = signature.offset ?? 0
  return signature.bytes.every((byte, index) => byte === null || head[offset + index] === byte)
}

/** Tells whether leading bytes read as text: no NUL byte and valid UTF-8, a truncated last character allowed. */
function looksLikeText(head: Uint8Array): boolean {
  if (head.includes(0)) {
    return false
  }
  try {
    new TextDecoder('utf-8', { fatal: true }).decode(head, { stream: true })
    return true
  } catch {
    return false
  }
}

/**
 * Tells the viewer of a file from its name and its first bytes (ADR-0018). The content wins over the extension, so a file with a lying
 * extension opens as what it is: a signature decides, refined by the extension for containers shared by several formats (a ZIP also holds
 * Office documents, an OLE file an old Excel workbook). Without a signature, text keeps a text kind of its extension (Markdown, JSON, YAML,
 * XML, CSV, SVG, mail, PEM) or is plain text, and other bytes keep a binary kind of their extension or are binary; the viewer of a kind
 * falls back to the hexadecimal one when it cannot decode the file.
 */
export function detectKind(name: string, head: Uint8Array): PreviewKind {
  const named = byExtension[extensionOf(name)] as PreviewKind | undefined
  const signed = signatures.find((signature) => matches(head, signature))
  if (signed !== undefined) {
    const refined = (signed.kind === 'archive' && named !== undefined && zipBased.has(named)) || (signed.kind === 'binary' && named === 'spreadsheet')
    return refined ? (named as PreviewKind) : signed.kind
  }
  if (!looksLikeText(head)) {
    return named !== undefined && binaryKinds.has(named) ? named : 'binary'
  }
  if (new TextDecoder().decode(head.subarray(0, 16)).startsWith('-----BEGIN ')) {
    return 'certificate'
  }
  return named !== undefined && textKinds.has(named) ? named : 'text'
}
