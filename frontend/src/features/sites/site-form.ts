import { z } from 'zod'

import type { Charset, Site, SiteInput } from '@/lib/api'

/** Default port of each protocol; FTPS on port 990 starts with TLS, on any other port it upgrades. */
export const defaultPorts = { sftp: 22, ftps: 21, ftp: 21 } as const

/** Highest valid TCP port. */
const maxPort = 65535

/** The character sets a site can use for file names, with the text each one is shown as. */
export const charsetOptions: ReadonlyArray<{ value: Charset; label: string }> = [
  { value: 'utf-8', label: 'UTF-8' },
  { value: 'iso-8859-1', label: 'ISO-8859-1 (Western European)' },
  { value: 'iso-8859-15', label: 'ISO-8859-15 (Western European with euro)' },
  { value: 'iso-8859-2', label: 'ISO-8859-2 (Central European)' },
  { value: 'windows-1252', label: 'Windows-1252 (Western European)' },
  { value: 'windows-1250', label: 'Windows-1250 (Central European)' },
  { value: 'windows-1251', label: 'Windows-1251 (Cyrillic)' },
  { value: 'koi8-r', label: 'KOI8-R (Cyrillic)' },
  { value: 'shift_jis', label: 'Shift_JIS (Japanese)' },
  { value: 'euc-jp', label: 'EUC-JP (Japanese)' },
  { value: 'gbk', label: 'GBK (Simplified Chinese)' },
  { value: 'big5', label: 'Big5 (Traditional Chinese)' },
  { value: 'euc-kr', label: 'EUC-KR (Korean)' },
]

/** Validation of the site form; messages are translation keys. */
export const siteSchema = z.object({
  name: z.string().trim().min(1, 'nameRequired'),
  collectionId: z.string(),
  protocol: z.enum(['sftp', 'ftps', 'ftp']),
  host: z.string().trim().min(1, 'hostRequired'),
  port: z.string().trim().refine((value) => value === '' || (/^\d{1,5}$/.test(value) && Number(value) >= 1 && Number(value) <= maxPort), 'portInvalid'),
  username: z.string().trim().min(1, 'usernameRequired'),
  authMethod: z.enum(['password', 'key']),
  password: z.string(),
  privateKey: z.string(),
  passphrase: z.string(),
  listTimezone: z.string().trim(),
  charset: z.string(),
})

/** Values of the site form. */
export type SiteValues = z.infer<typeof siteSchema>

/** Initial values of the form for a new site, or for an existing one; secrets are never prefilled. */
export function siteDefaults(site: Site | undefined, collectionId: string | undefined): SiteValues {
  return {
    name: site?.name ?? '',
    collectionId: site?.collectionId ?? collectionId ?? '',
    protocol: site?.protocol ?? 'sftp',
    host: site?.host ?? '',
    port: site === undefined ? '' : String(site.port),
    username: site?.username ?? '',
    authMethod: site?.authMethod ?? 'password',
    password: '',
    privateKey: '',
    passphrase: '',
    listTimezone: site?.listTimezone ?? 'UTC',
    charset: site?.charset ?? 'utf-8',
  }
}

/** Turns a blank secret into "not provided", which keeps the stored one. */
function secret(value: string): string | undefined {
  return value === '' ? undefined : value
}

/** Builds the request body of a site: an empty port means the default and blank secrets are left out. */
export function toSiteInput(values: SiteValues): SiteInput {
  const usesKey = values.protocol === 'sftp' && values.authMethod === 'key'
  return {
    name: values.name.trim(),
    collectionId: values.collectionId === '' ? undefined : values.collectionId,
    protocol: values.protocol,
    host: values.host.trim(),
    port: values.port.trim() === '' ? defaultPorts[values.protocol] : Number(values.port),
    username: values.username.trim(),
    authMethod: usesKey ? 'key' : 'password',
    listTimezone: values.listTimezone === '' ? 'UTC' : values.listTimezone,
    charset: values.protocol === 'sftp' ? 'utf-8' : (values.charset as Charset),
    password: usesKey ? undefined : secret(values.password),
    privateKey: usesKey ? secret(values.privateKey) : undefined,
    passphrase: usesKey ? secret(values.passphrase) : undefined,
  }
}
