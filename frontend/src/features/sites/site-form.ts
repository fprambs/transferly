import type { FieldErrors } from 'react-hook-form'
import { z } from 'zod'

import type { Charset, Site, SiteInput } from '@/lib/api'

/** Default port of each protocol; an FTP site with implicit TLS defaults to {@link implicitPort} instead. */
export const defaultPorts = { sftp: 22, ftp: 21 } as const

/** Port of implicit FTPS, the default of FTP sites that use it. */
export const implicitPort = 990

/** How an FTP site protects the connection, as FileZilla offers it (ADR-0012). */
export type FtpEncryption = 'optional' | 'explicit' | 'implicit' | 'plain'

/** The FTP encryptions in the order FileZilla lists them. */
export const ftpEncryptions: readonly FtpEncryption[] = ['optional', 'explicit', 'implicit', 'plain']

/** The encryption of a new FTP site, or of an SFTP site switched to FTP: TLS required, never a silent fall back. */
export const defaultFtpEncryption: FtpEncryption = 'explicit'

/** The default port of a site, given its protocol and, for FTP, its encryption. */
export function defaultPortOf(protocol: SiteValues['protocol'], encryption: FtpEncryption): number {
  return protocol === 'ftp' && encryption === 'implicit' ? implicitPort : defaultPorts[protocol]
}

/** Highest valid TCP port. */
const maxPort = 65535

/** Bytes in a mebibyte, the unit the site's speed limit is shown in. */
const mebibyte = 1024 * 1024

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

/** How the site signs in, as FileZilla names it: with a stored password, with a password asked for when connecting (ADR-0010), with a
 * private key (SFTP only), anonymously with no secret (the SSH "none" method first for SFTP), or with a password and an account that
 * some FTP servers ask for (ACCT, FTP only, 4.8e). */
export type AccessMode = 'normal' | 'ask' | 'key' | 'anonymous' | 'account'

/** The access modes a protocol offers, in the order they are listed. */
export function accessModesOf(protocol: SiteValues['protocol']): readonly AccessMode[] {
  return protocol === 'sftp' ? ['normal', 'ask', 'key', 'anonymous'] : ['normal', 'ask', 'account', 'anonymous']
}

/** The access mode a site really uses: one its protocol does not offer falls back to a password. */
export function effectiveMode(values: Pick<SiteValues, 'protocol' | 'accessMode'>): AccessMode {
  return accessModesOf(values.protocol).includes(values.accessMode) ? values.accessMode : 'normal'
}

/** The user name of every anonymous site. */
export const anonymousUser = 'anonymous'

/** Whether a port, given as text, is a valid one; an empty text is valid too and means the default. */
function validPort(value: string): boolean {
  return value === '' || (/^\d{1,5}$/.test(value) && Number(value) >= 1 && Number(value) <= maxPort)
}

/** Validation of the site form; messages are translation keys. */
export const siteSchema = z
  .object({
    name: z.string().trim().min(1, 'nameRequired'),
    collectionId: z.string(),
    protocol: z.enum(['sftp', 'ftp']),
    ftpEncryption: z.enum(['optional', 'explicit', 'implicit', 'plain']),
    host: z.string().trim().min(1, 'hostRequired'),
    port: z.string().trim().refine(validPort, 'portInvalid'),
    username: z.string().trim(),
    accessMode: z.enum(['normal', 'ask', 'key', 'anonymous', 'account']),
    password: z.string(),
    account: z.string(),
    privateKey: z.string(),
    passphrase: z.string(),
    listTimezone: z.string().trim(),
    remoteDir: z.string().trim().refine((value) => value === '' || value.startsWith('/'), 'remoteDirInvalid'),
    charset: z.string(),
    speedLimit: z.string().trim().refine((value) => value === '' || (/^\d+(\.\d+)?$/.test(value) && Number(value) > 0), 'speedLimitInvalid'),
    proxyKind: z.enum(['none', 'socks5', 'http']),
    proxyHost: z.string().trim(),
    proxyPort: z.string().trim(),
    proxyUsername: z.string().trim(),
    proxyPassword: z.string(),
    jumpHost: z.string().trim(),
    jumpPort: z.string().trim(),
    jumpUsername: z.string().trim(),
    jumpAuthMethod: z.enum(['password', 'key']),
    jumpPassword: z.string(),
    jumpPrivateKey: z.string(),
    jumpPassphrase: z.string(),
  })
  .superRefine((values, ctx) => {
    if (values.username === '' && effectiveMode(values) !== 'anonymous') {
      ctx.addIssue({ code: 'custom', path: ['username'], message: 'usernameRequired' })
    }
    if (values.proxyKind !== 'none') {
      if (values.proxyHost === '') {
        ctx.addIssue({ code: 'custom', path: ['proxyHost'], message: 'hostRequired' })
      }
      if (!validPort(values.proxyPort)) {
        ctx.addIssue({ code: 'custom', path: ['proxyPort'], message: 'portInvalid' })
      }
    }
    if (values.jumpHost !== '') {
      if (!validPort(values.jumpPort)) {
        ctx.addIssue({ code: 'custom', path: ['jumpPort'], message: 'portInvalid' })
      }
      if (values.jumpUsername === '') {
        ctx.addIssue({ code: 'custom', path: ['jumpUsername'], message: 'usernameRequired' })
      }
    }
  })

/** Values of the site form. */
export type SiteValues = z.infer<typeof siteSchema>

/** The access mode of a saved site, from its logon type: a password is the normal mode. */
function accessModeOf(site: Site | undefined): AccessMode {
  return site === undefined || site.authMethod === 'password' ? 'normal' : site.authMethod
}

/** Initial values of the form for a new site, or for an existing one; secrets are never prefilled. */
export function siteDefaults(site: Site | undefined, collectionId: string | undefined): SiteValues {
  return {
    name: site?.name ?? '',
    collectionId: site?.collectionId ?? collectionId ?? '',
    protocol: site === undefined || site.protocol === 'sftp' ? 'sftp' : 'ftp',
    ftpEncryption: site?.ftpEncryption ?? defaultFtpEncryption,
    host: site?.host ?? '',
    port: site === undefined ? '' : String(site.port),
    username: site?.username ?? '',
    accessMode: accessModeOf(site),
    password: '',
    account: '',
    privateKey: '',
    passphrase: '',
    listTimezone: site?.listTimezone ?? 'UTC',
    remoteDir: site?.remoteDir ?? '',
    charset: site?.charset ?? 'utf-8',
    speedLimit: site?.speedLimitBps ? String(site.speedLimitBps / mebibyte) : '',
    proxyKind: site?.proxy?.kind ?? 'none',
    proxyHost: site?.proxy?.host ?? '',
    proxyPort: site?.proxy ? String(site.proxy.port) : '',
    proxyUsername: site?.proxy?.username ?? '',
    proxyPassword: '',
    jumpHost: site?.jump?.host ?? '',
    jumpPort: site?.jump ? String(site.jump.port) : '',
    jumpUsername: site?.jump?.username ?? '',
    jumpAuthMethod: site?.jump?.authMethod ?? 'password',
    jumpPassword: '',
    jumpPrivateKey: '',
    jumpPassphrase: '',
  }
}

/** Turns a blank secret into "not provided", which keeps the stored one. */
function secret(value: string): string | undefined {
  return value === '' ? undefined : value
}

/** Default port of a proxy that did not say its own. */
export const defaultProxyPort = 1080

/** Default port of a jump host that did not say its own: the SSH port, since only SFTP sites may have one. */
export const defaultJumpPort = 22

/** Builds the request body of a site: an empty port means the default and blank secrets are left out; a site that asks for its
 * password clears the stored one, and the server drops every secret of an anonymous one. */
export function toSiteInput(values: SiteValues): SiteInput {
  const mode = effectiveMode(values)
  const usesKey = mode === 'key'
  const jumpUsesKey = values.jumpAuthMethod === 'key'
  return {
    name: values.name.trim(),
    collectionId: values.collectionId === '' ? undefined : values.collectionId,
    protocol: values.protocol,
    ftpEncryption: values.protocol === 'ftp' ? values.ftpEncryption : undefined,
    host: values.host.trim(),
    port: values.port.trim() === '' ? defaultPortOf(values.protocol, values.ftpEncryption) : Number(values.port),
    username: mode === 'anonymous' ? anonymousUser : values.username.trim(),
    authMethod: mode === 'normal' ? 'password' : mode,
    listTimezone: values.listTimezone === '' ? 'UTC' : values.listTimezone,
    remoteDir: values.remoteDir,
    charset: values.protocol === 'sftp' ? 'utf-8' : (values.charset as Charset),
    speedLimitBps: values.speedLimit.trim() === '' ? 0 : Math.round(Number(values.speedLimit) * mebibyte),
    password: mode === 'normal' || mode === 'account' ? secret(values.password) : mode === 'ask' ? '' : undefined,
    account: mode === 'account' ? secret(values.account) : undefined,
    privateKey: usesKey ? secret(values.privateKey) : undefined,
    passphrase: usesKey ? secret(values.passphrase) : undefined,
    ...proxyInput(values),
    ...jumpInput(values, jumpUsesKey),
  }
}

/** Builds the proxy fields of the request body; none of them are sent when no proxy is set. */
function proxyInput(values: SiteValues): Pick<SiteInput, 'proxy' | 'proxyPassword'> {
  if (values.proxyKind === 'none') {
    return { proxy: undefined, proxyPassword: undefined }
  }
  const username = values.proxyUsername.trim()
  return {
    proxy: {
      kind: values.proxyKind,
      host: values.proxyHost.trim(),
      port: values.proxyPort.trim() === '' ? defaultProxyPort : Number(values.proxyPort),
      username: username === '' ? undefined : username,
    },
    proxyPassword: secret(values.proxyPassword),
  }
}

/** Builds the jump host fields of the request body; none of them are sent when no jump host is set. */
function jumpInput(values: SiteValues, usesKey: boolean): Pick<SiteInput, 'jump' | 'jumpPassword' | 'jumpPrivateKey' | 'jumpPassphrase'> {
  if (values.jumpHost === '') {
    return { jump: undefined, jumpPassword: undefined, jumpPrivateKey: undefined, jumpPassphrase: undefined }
  }
  return {
    jump: {
      host: values.jumpHost,
      port: values.jumpPort.trim() === '' ? defaultJumpPort : Number(values.jumpPort),
      username: values.jumpUsername,
      authMethod: usesKey ? 'key' : 'password',
    },
    jumpPassword: usesKey ? undefined : secret(values.jumpPassword),
    jumpPrivateKey: usesKey ? secret(values.jumpPrivateKey) : undefined,
    jumpPassphrase: usesKey ? secret(values.jumpPassphrase) : undefined,
  }
}

/** The tabs of the site form, as FileZilla splits it: what is needed to connect, the listing time zone, the speed limit, the character set of
 * file names, and how the connection is routed. */
export type SiteTab = 'general' | 'advanced' | 'transfer' | 'charset' | 'network'

/** The tabs in the order they are drawn. */
export const siteTabs: readonly SiteTab[] = ['general', 'advanced', 'transfer', 'charset', 'network']

/** The tabs a protocol shows: the character set is chosen for FTP and FTPS only, SFTP always uses UTF-8. */
export function tabsOf(protocol: SiteValues['protocol']): readonly SiteTab[] {
  return protocol === 'sftp' ? siteTabs.filter((tab) => tab !== 'charset') : siteTabs
}

/** The tab each field of the form is on. */
const fieldTabs: Record<keyof SiteValues, SiteTab> = {
  name: 'general',
  collectionId: 'general',
  protocol: 'general',
  ftpEncryption: 'general',
  host: 'general',
  port: 'general',
  username: 'general',
  accessMode: 'general',
  password: 'general',
  account: 'general',
  privateKey: 'general',
  passphrase: 'general',
  charset: 'charset',
  listTimezone: 'advanced',
  remoteDir: 'advanced',
  speedLimit: 'transfer',
  proxyKind: 'network',
  proxyHost: 'network',
  proxyPort: 'network',
  proxyUsername: 'network',
  proxyPassword: 'network',
  jumpHost: 'network',
  jumpPort: 'network',
  jumpUsername: 'network',
  jumpAuthMethod: 'network',
  jumpPassword: 'network',
  jumpPrivateKey: 'network',
  jumpPassphrase: 'network',
}

/** The tabs that hold at least one field with a validation error. */
export function tabsWithErrors(errors: FieldErrors<SiteValues>): Set<SiteTab> {
  return new Set((Object.keys(errors) as (keyof SiteValues)[]).map((field) => fieldTabs[field]))
}
