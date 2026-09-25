import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the site and collection dialogs explain. */
const siteErrorKeys = [
  'collection_name_taken',
  'collection_cycle',
  'collection_too_deep',
  'collection_not_empty',
  'collection_not_found',
  'forbidden',
  'network_error',
] as const

/** Translation key of the message that explains a failure. */
export type SiteErrorKey = (typeof siteErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function siteErrorKeyOf(error: unknown): SiteErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return siteErrorKeys.find((key) => key === code) ?? 'unknown'
}
