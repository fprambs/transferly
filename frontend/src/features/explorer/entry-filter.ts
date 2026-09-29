import type { RemoteEntry } from '@/lib/api'

import { fileTypeOf, type FileType } from './file-types'

/** Groups of types the filter offers, built on the file types of 4.14e. */
export const kindGroups = ['folders', 'images', 'media', 'documents', 'code', 'archives', 'other'] as const

/** A group of types the filter offers. */
export type KindGroup = (typeof kindGroups)[number]

/** How recently the entries were changed. */
export const modifiedPresets = ['any', 'today', 'week', 'month', 'year', 'custom'] as const

/** How recently the entries were changed. */
export type ModifiedPreset = (typeof modifiedPresets)[number]

/** How big the files are. */
export const sizePresets = ['any', 'small', 'medium', 'large'] as const

/** How big the files are. */
export type SizePreset = (typeof sizePresets)[number]

/** What the filter of a folder asks for (4.14d). */
export interface EntryFilter {
  /** Words and patterns the name must match. */
  text: string
  /** Groups of types to keep; none keeps every type. */
  kinds: readonly KindGroup[]
  /** How recently the entries were changed. */
  modified: ModifiedPreset
  /** First day of a custom range, as YYYY-MM-DD, or empty. */
  from: string
  /** Last day of a custom range, as YYYY-MM-DD, or empty. */
  to: string
  /** How big the files are. */
  size: SizePreset
}

/** A filter that keeps everything. */
export const emptyFilter: EntryFilter = { text: '', kinds: [], modified: 'any', from: '', to: '', size: 'any' }

/** One mebibyte, the unit of the size presets. */
const mebibyte = 1024 * 1024

/** Group of each file kind. */
const groupOfKind: Partial<Record<FileType['kind'], KindGroup>> = {
  directory: 'folders',
  image: 'images',
  audio: 'media',
  video: 'media',
  document: 'documents',
  spreadsheet: 'documents',
  presentation: 'documents',
  text: 'documents',
  log: 'documents',
  code: 'code',
  script: 'code',
  data: 'code',
  config: 'code',
  named: 'code',
  archive: 'archives',
  package: 'archives',
  disk: 'archives',
}

/** The group of types an entry belongs to. */
export function kindGroupOf(entry: Pick<RemoteEntry, 'name' | 'type'>): KindGroup {
  return groupOfKind[fileTypeOf(entry).kind] ?? 'other'
}

/** How many facets besides the text a filter uses, for the badge of its button. */
export function facetCount(filter: EntryFilter): number {
  return (filter.kinds.length > 0 ? 1 : 0) + (filter.modified !== 'any' ? 1 : 0) + (filter.size !== 'any' ? 1 : 0)
}

/** Whether a filter hides anything. */
export function isActive(filter: EntryFilter): boolean {
  return filter.text.trim() !== '' || facetCount(filter) > 0
}

/** A name folded for matching, lower case and without accents, with the index in the name of each folded character. */
interface Folded {
  /** The folded text. */
  text: string
  /** Index in the original name of each folded character. */
  origin: number[]
}

/** Folds a text for matching: lower case, accents removed, so "Canción" matches "cancion". */
function fold(value: string): Folded {
  const origin: number[] = []
  let text = ''
  Array.from(value).reduce((at, char) => {
    const plain = char.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()
    for (let i = 0; i < plain.length; i++) {
      origin.push(at)
    }
    text += plain
    return at + char.length
  }, 0)
  return { text, origin }
}

/** One term of the text of a filter. */
interface Term {
  /** Whether the name must not match it. */
  exclude: boolean
  /** A pattern with wildcards, matched against the whole name, or `undefined` for a plain word. */
  pattern?: RegExp
  /** The folded word, for plain words. */
  word: string
}

/** Reads the terms of the text of a filter: words separated by spaces, `*` and `?` as wildcards, `-` before a term to exclude it. */
function termsOf(text: string): Term[] {
  return text
    .split(/\s+/)
    .filter((raw) => raw !== '' && raw !== '-')
    .map((raw) => {
      const exclude = raw.startsWith('-')
      const word = fold(exclude ? raw.slice(1) : raw).text
      if (!/[*?]/.test(word)) {
        return { exclude, word }
      }
      const source = word.replace(/[.+^${}()|[\]\\]/g, '\\$&').replaceAll('*', '.*').replaceAll('?', '.')
      return { exclude, word, pattern: new RegExp(`^${source}$`, 'su') }
    })
}

/** Whether a folded name matches a term. */
function matchesTerm(name: string, term: Term): boolean {
  return term.pattern === undefined ? name.includes(term.word) : term.pattern.test(name)
}

/**
 * Where the plain words of the text of a filter appear in a name, to highlight them, as sorted, merged [start, end) ranges of the name.
 * Patterns and excluded words are not highlighted.
 */
export function matchRanges(name: string, text: string): Array<[number, number]> {
  const folded = fold(name)
  const ranges: Array<[number, number]> = []
  for (const term of termsOf(text)) {
    if (term.exclude || term.pattern !== undefined) {
      continue
    }
    for (let at = folded.text.indexOf(term.word); at !== -1; at = folded.text.indexOf(term.word, at + term.word.length)) {
      const last = folded.origin[at + term.word.length - 1]
      ranges.push([folded.origin[at], last + (name.codePointAt(last) as number > 0xffff ? 2 : 1)])
    }
  }
  ranges.sort((a, b) => a[0] - b[0])
  return ranges.reduce<Array<[number, number]>>((merged, range) => {
    const previous = merged.at(-1)
    if (previous !== undefined && range[0] <= previous[1]) {
      previous[1] = Math.max(previous[1], range[1])
    } else {
      merged.push([...range])
    }
    return merged
  }, [])
}

/** The day of an instant in a time zone, as YYYY-MM-DD, which sorts as it reads. */
export function dayOf(instant: Date, timeZone: string): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(instant)
}

/** Whether an entry was changed as recently as the filter asks, days read in the time zone of the account. */
function matchesModified(entry: RemoteEntry, filter: EntryFilter, now: Date, timeZone: string): boolean {
  const changed = new Date(entry.modifiedAt)
  const day = dayOf(changed, timeZone)
  const days = (count: number): boolean => now.getTime() - changed.getTime() <= count * 86_400_000
  const checks: Record<ModifiedPreset, () => boolean> = {
    any: () => true,
    today: () => day === dayOf(now, timeZone),
    week: () => days(7),
    month: () => days(30),
    year: () => day.slice(0, 4) === dayOf(now, timeZone).slice(0, 4),
    custom: () => (filter.from === '' || day >= filter.from) && (filter.to === '' || day <= filter.to),
  }
  return checks[filter.modified]()
}

/** Whether a file is as big as the filter asks; any size filter leaves folders and other entries out, since they have no size. */
function matchesSize(entry: RemoteEntry, size: SizePreset): boolean {
  if (size === 'any') {
    return true
  }
  if (entry.type !== 'file') {
    return false
  }
  const limits: Record<Exclude<SizePreset, 'any'>, readonly [number, number]> = {
    small: [0, mebibyte],
    medium: [mebibyte, 100 * mebibyte],
    large: [100 * mebibyte, Number.POSITIVE_INFINITY],
  }
  const [min, max] = limits[size]
  return entry.size >= min && entry.size < max
}

/**
 * The entries of a folder that a filter keeps, in their order (4.14d). Nothing is asked of the server: the filter reads the listing in
 * view, and every action on what it shows still goes to the server by path.
 */
export function filterEntries(entries: readonly RemoteEntry[], filter: EntryFilter, now: Date, timeZone: string): RemoteEntry[] {
  const terms = termsOf(filter.text)
  return entries.filter((entry) => {
    const name = fold(entry.name).text
    return (
      terms.every((term) => matchesTerm(name, term) !== term.exclude) &&
      (filter.kinds.length === 0 || filter.kinds.includes(kindGroupOf(entry))) &&
      matchesModified(entry, filter, now, timeZone) &&
      matchesSize(entry, filter.size)
    )
  })
}
