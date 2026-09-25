import { z } from 'zod'

import type { QuickConnectInput } from '@/lib/api'

/** Default port of each protocol. */
export const defaultPorts = { sftp: 22, ftps: 21, ftp: 21 } as const

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

/** Builds the request body of a quick connection; an empty port means the protocol default. */
export function toQuickInput(protocol: QuickConnectInput['protocol'], values: QuickValues): QuickConnectInput {
  return {
    protocol,
    host: values.host.trim(),
    port: values.port.trim() === '' ? defaultPorts[protocol] : Number(values.port),
    username: values.username.trim(),
    authMethod: 'password',
    password: values.password,
  }
}
