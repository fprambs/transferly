import type { ImportResult } from '@/lib/api'

/** How long the master password pauses before it is checked, in milliseconds. */
export const filezillaMasterCheckDelayMs = 500

/** Tells whether a FileZilla site manager uses master-password-protected site passwords. */
export function filezillaUsesCrypt(text: string): boolean {
  return text.includes('encoding="crypt"') || text.includes("encoding='crypt'")
}

/** Tells whether a dry run shows that the FileZilla master password did not open any site password. */
export function filezillaMasterRejected(preview: ImportResult, importPasswords: boolean): boolean {
  if (!importPasswords) {
    return false
  }
  const pending = preview.items.filter((item) => item.status === 'would_create' || item.status === 'created')
  const withPasswordNote = pending.filter((item) => item.passwordStored || item.passwordSkipped)
  if (withPasswordNote.length === 0) {
    return false
  }
  return withPasswordNote.every((item) => item.passwordSkipped)
}
