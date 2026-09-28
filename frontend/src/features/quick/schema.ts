import { z } from 'zod'

import type { QuickConnectInput } from '@/lib/api'

/** The protocols a quick connection offers: FTP upgrades to TLS when the server offers it, as FileZilla's quick connect does. */
export type QuickProtocol = 'sftp' | 'ftp'

/** Default port of each protocol. */
export const defaultPorts = { sftp: 22, ftp: 21 } as const

/** Highest valid TCP port. */
const maxPort = 65535

/** Validation of the quick connect form; messages are translation keys. */
export const quickSchema = z.object({
  host: z.string().trim().min(1, 'hostRequired'),
  port: z.string().trim().refine((value) => value === '' || (/^\d{1,5}$/.test(value) && Number(value) >= 1 && Number(value) <= maxPort), 'portInvalid'),
  username: z.string().trim().min(1, 'usernameRequired'),
  password: z.string().min(1, 'passwordRequired'),
})

/** Values of the quick connect form. */
export type QuickValues = z.infer<typeof quickSchema>

/**
 * Builds the request body of a quick connection; an empty port means the protocol default. FTP uses TLS when the server offers it and clear
 * text otherwise (ADR-0012), since a quick connection has no encryption to choose; a saved site can require TLS.
 */
export function toQuickInput(protocol: QuickProtocol, values: QuickValues): QuickConnectInput {
  return {
    protocol,
    ftpEncryption: protocol === 'ftp' ? 'optional' : undefined,
    host: values.host.trim(),
    port: values.port.trim() === '' ? defaultPorts[protocol] : Number(values.port),
    username: values.username.trim(),
    authMethod: 'password',
    password: values.password,
  }
}
