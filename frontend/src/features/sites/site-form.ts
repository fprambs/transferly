import { z } from 'zod'

import type { Site, SiteInput } from '@/lib/api'

/** Default SFTP port. */
const sftpPort = 22

/** Highest valid TCP port. */
const maxPort = 65535

/** Validation of the site form; messages are translation keys. */
export const siteSchema = z.object({
  name: z.string().trim().min(1, 'nameRequired'),
  collectionId: z.string(),
  host: z.string().trim().min(1, 'hostRequired'),
  port: z.string().trim().refine((value) => value === '' || (/^\d{1,5}$/.test(value) && Number(value) >= 1 && Number(value) <= maxPort), 'portInvalid'),
  username: z.string().trim().min(1, 'usernameRequired'),
  authMethod: z.enum(['password', 'key']),
  password: z.string(),
  privateKey: z.string(),
  passphrase: z.string(),
  listTimezone: z.string().trim(),
})

/** Values of the site form. */
export type SiteValues = z.infer<typeof siteSchema>

/** Initial values of the form for a new site, or for an existing one; secrets are never prefilled. */
export function siteDefaults(site: Site | undefined, collectionId: string | undefined): SiteValues {
  return {
    name: site?.name ?? '',
    collectionId: site?.collectionId ?? collectionId ?? '',
    host: site?.host ?? '',
    port: site === undefined ? '' : String(site.port),
    username: site?.username ?? '',
    authMethod: site?.authMethod ?? 'password',
    password: '',
    privateKey: '',
    passphrase: '',
    listTimezone: site?.listTimezone ?? 'UTC',
  }
}

/** Turns a blank secret into "not provided", which keeps the stored one. */
function secret(value: string): string | undefined {
  return value === '' ? undefined : value
}

/** Builds the request body of a site: an empty port means the default and blank secrets are left out. */
export function toSiteInput(values: SiteValues): SiteInput {
  const usesKey = values.authMethod === 'key'
  return {
    name: values.name.trim(),
    collectionId: values.collectionId === '' ? undefined : values.collectionId,
    protocol: 'sftp',
    host: values.host.trim(),
    port: values.port.trim() === '' ? sftpPort : Number(values.port),
    username: values.username.trim(),
    authMethod: values.authMethod,
    listTimezone: values.listTimezone === '' ? 'UTC' : values.listTimezone,
    password: usesKey ? undefined : secret(values.password),
    privateKey: usesKey ? secret(values.privateKey) : undefined,
    passphrase: usesKey ? secret(values.passphrase) : undefined,
  }
}
