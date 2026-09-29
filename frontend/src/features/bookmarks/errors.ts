import { ApiError } from '@/lib/transport'

/** Translation keys of the failures of changing bookmarks. */
export const bookmarkErrorKeys = [
  'bookmark_name_taken',
  'bookmark_not_found',
  'bookmark_limit_reached',
  'invalid_bookmark_name',
  'invalid_bookmark_path',
  'forbidden',
  'network_error',
] as const

/** Translation key of the message that explains a failure. */
export type BookmarkErrorKey = (typeof bookmarkErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function bookmarkErrorKeyOf(error: unknown): BookmarkErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return bookmarkErrorKeys.find((key) => key === code) ?? 'unknown'
}
