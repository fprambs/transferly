import type { ArchiveFormat } from '@/lib/api'

/** The extensions of the archives a server packs and unpacks, by format, the first one the one an archive gets (ADR-0027). */
const extensions: Record<ArchiveFormat, readonly string[]> = { zip: ['.zip'], 'tar.gz': ['.tar.gz', '.tgz'] }

/** Returns the format a file name says, or `undefined` when it is not an archive a server unpacks. */
export function archiveFormatOf(name: string): ArchiveFormat | undefined {
  const lower = name.toLowerCase()
  return (Object.keys(extensions) as ArchiveFormat[]).find((format) => extensions[format].some((suffix) => lower.endsWith(suffix) && lower.length > suffix.length))
}

/** Returns the name of an archive of the entry called name, in format. */
export function archiveName(name: string, format: ArchiveFormat): string {
  return name + extensions[format][0]
}

/** Reports whether name is a name for an archive in format: one that ends with its extension, with something before it and no slash. */
export function isArchiveName(name: string, format: ArchiveFormat): boolean {
  return !name.includes('/') && archiveFormatOf(name) === format
}
