import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { dayOf, emptyFilter, facetCount, filterEntries, isActive, kindGroupOf, matchRanges, type EntryFilter } from './entry-filter'

/** The moment the tests run at: noon of 2026-09-29 in UTC. */
const now = new Date('2026-09-29T12:00:00Z')

/** Builds an entry. */
function entry(name: string, overrides: Partial<RemoteEntry> = {}): RemoteEntry {
  return { name, path: `/${name}`, type: 'file', size: 10, mode: '-rw-r--r--', modifiedAt: '2026-09-29T08:00:00Z', modifiedPrecision: 'minute', ...overrides }
}

/** A folder with one entry of each kind, sizes and dates spread out. */
const folder: RemoteEntry[] = [
  entry('logs', { type: 'directory', size: 0 }),
  entry('Canción.mp3', { size: 5 * 1024 * 1024, modifiedAt: '2026-09-25T08:00:00Z' }),
  entry('informe-2026.pdf', { size: 200 * 1024 * 1024, modifiedAt: '2026-09-01T08:00:00Z' }),
  entry('access.log', { modifiedAt: '2025-12-31T23:00:00Z' }),
  entry('deploy.sh'),
  entry('backup.tar.gz'),
  entry('photo.png'),
  entry('notes'),
]

/** The names a filter keeps. */
function kept(filter: Partial<EntryFilter>, timeZone = 'UTC'): string[] {
  return filterEntries(folder, { ...emptyFilter, ...filter }, now, timeZone).map((item) => item.name)
}

/**
 * Covers the instant filter of a folder (4.14d).
 *
 * Scenarios:
 * - an empty filter keeps everything and is not active; facets are counted for the badge
 * - words match anywhere in the name without case or accents, all of them must match, and `-` excludes a word
 * - `*` and `?` match the whole name, and other signs in a pattern are literal
 * - entries fall in the groups of their types, and several groups keep any of them
 * - the modified presets count days in the time zone of the account, and a custom range may be open at either end
 * - the size presets keep only files within their bounds
 * - the plain words of the text are found in the name to highlight them, merged when they overlap, accents and wide characters included
 */
describe('entry filter', () => {
  it('keeps everything when empty and counts its facets', () => {
    expect(kept({})).toHaveLength(folder.length)
    expect(isActive(emptyFilter)).toBe(false)
    expect(isActive({ ...emptyFilter, text: '  ' })).toBe(false)
    expect(isActive({ ...emptyFilter, text: 'log' })).toBe(true)
    expect(facetCount({ ...emptyFilter, kinds: ['images'], modified: 'today', size: 'small' })).toBe(3)
    expect(isActive({ ...emptyFilter, size: 'large' })).toBe(true)
  })

  it('matches words without case or accents, all of them, and excludes', () => {
    expect(kept({ text: 'CANCION' })).toEqual(['Canción.mp3'])
    expect(kept({ text: 'log' })).toEqual(['logs', 'access.log'])
    expect(kept({ text: 'log access' })).toEqual(['access.log'])
    expect(kept({ text: 'log -access' })).toEqual(['logs'])
    expect(kept({ text: '- ' })).toHaveLength(folder.length)
  })

  it('matches wildcards against the whole name', () => {
    expect(kept({ text: '*.log' })).toEqual(['access.log'])
    expect(kept({ text: 'informe-202?.pdf' })).toEqual(['informe-2026.pdf'])
    expect(kept({ text: '*.tar.gz' })).toEqual(['backup.tar.gz'])
    expect(kept({ text: 'p*' })).toEqual(['photo.png'])
    expect(kept({ text: '-*.*' })).toEqual(['logs', 'notes'])
    expect(kept({ text: '(*' })).toEqual([])
  })

  it('keeps the groups of types asked for', () => {
    expect(kindGroupOf({ name: 'x', type: 'symlink' })).toBe('other')
    expect(kept({ kinds: ['folders'] })).toEqual(['logs'])
    expect(kept({ kinds: ['media', 'images'] })).toEqual(['Canción.mp3', 'photo.png'])
    expect(kept({ kinds: ['documents'] })).toEqual(['informe-2026.pdf', 'access.log'])
    expect(kept({ kinds: ['code', 'archives'] })).toEqual(['deploy.sh', 'backup.tar.gz'])
    expect(kept({ kinds: ['other'] })).toEqual(['notes'])
  })

  it('reads how recently entries changed in the time zone of the account', () => {
    expect(kept({ modified: 'today' })).toEqual(['logs', 'deploy.sh', 'backup.tar.gz', 'photo.png', 'notes'])
    expect(kept({ modified: 'week' })).toContain('Canción.mp3')
    expect(kept({ modified: 'week' })).not.toContain('informe-2026.pdf')
    expect(kept({ modified: 'month' })).toContain('informe-2026.pdf')
    expect(kept({ modified: 'year' })).not.toContain('access.log')
    expect(kept({ modified: 'custom', from: '2026-09-01', to: '2026-09-25' })).toEqual(['Canción.mp3', 'informe-2026.pdf'])
    expect(kept({ modified: 'custom', from: '2026-09-26' })).toHaveLength(5)
    expect(kept({ modified: 'custom', to: '2025-12-31' })).toEqual(['access.log'])
    expect(kept({ modified: 'custom', to: '2025-12-31' }, 'Asia/Tokyo')).toEqual([])
    expect(dayOf(new Date('2026-01-01T02:00:00Z'), 'America/Santiago')).toBe('2025-12-31')
  })

  it('keeps files of the size asked for', () => {
    expect(kept({ size: 'small' })).toEqual(['access.log', 'deploy.sh', 'backup.tar.gz', 'photo.png', 'notes'])
    expect(kept({ size: 'medium' })).toEqual(['Canción.mp3'])
    expect(kept({ size: 'large' })).toEqual(['informe-2026.pdf'])
  })

  it('finds the words to highlight', () => {
    expect(matchRanges('access.log', 'log')).toEqual([[7, 10]])
    expect(matchRanges('Canción.mp3', 'cion')).toEqual([[3, 7]])
    expect(matchRanges('lolol', 'lol ol')).toEqual([[0, 5]])
    expect(matchRanges('a-b-a', 'a')).toEqual([[0, 1], [4, 5]])
    expect(matchRanges('photo.png', '*.png -photo')).toEqual([])
    expect(matchRanges('😀smile', '😀')).toEqual([[0, 2]])
  })
})
