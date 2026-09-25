import { describe, expect, it } from 'vitest'

import type { Collection, QuickConnection, Site } from '@/lib/api'

import { buildTree } from './tree'

/** Builds a collection. */
function collection(id: string, name: string, parentId?: string): Collection {
  return { id, name, parentId }
}

/** Builds a site. */
function site(id: string, name: string, collectionId?: string): Site {
  return {
    id,
    name,
    collectionId,
    protocol: 'sftp',
    host: `${id}.example.com`,
    port: 22,
    username: 'u',
    authMethod: 'password',
    listTimezone: 'UTC',
    secrets: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  } as Site
}

/** Builds a quick connection. */
function quick(id: string, name: string): QuickConnection {
  return {
    id,
    name,
    protocol: 'sftp',
    host: name,
    port: 22,
    username: 'u',
    authMethod: 'password',
    listTimezone: 'UTC',
    createdAt: '2026-01-01T00:00:00Z',
  }
}

/** Summarizes rows as `kind:name@depth`. */
function shape(rows: ReturnType<typeof buildTree>): string[] {
  return rows.map((row) => `${row.kind}:${row.name}@${row.depth}`)
}

/**
 * Covers {@link buildTree}.
 *
 * Scenarios:
 * - quick connections come first, then collections before sites, each sorted by name
 * - closed collections hide their children; open ones show them one level deeper
 * - a collection counts the sites of every level below it
 * - a collection or site whose parent is unknown is shown at the top
 * - an empty workspace has no rows
 */
describe('buildTree', () => {
  const collections = [collection('c2', 'Work'), collection('c1', 'client'), collection('c3', 'Staging', 'c2')]
  const sites = [site('s1', 'zeta'), site('s2', 'Alpha', 'c2'), site('s3', 'Beta', 'c3'), site('s4', 'Loose')]

  it('orders quick connections, collections and sites', () => {
    const rows = buildTree({ collections, sites, quick: [quick('q2', 'b.host'), quick('q1', 'a.host')], expanded: new Set() })

    expect(shape(rows)).toEqual([
      'quick:a.host@0',
      'quick:b.host@0',
      'collection:client@0',
      'collection:Work@0',
      'site:Loose@0',
      'site:zeta@0',
    ])
  })

  it('shows the children of open collections', () => {
    const rows = buildTree({ collections, sites, quick: [], expanded: new Set(['c2', 'c3']) })

    expect(shape(rows)).toEqual([
      'collection:client@0',
      'collection:Work@0',
      'collection:Staging@1',
      'site:Beta@2',
      'site:Alpha@1',
      'site:Loose@0',
      'site:zeta@0',
    ])
  })

  it('counts nested sites and reports the open state', () => {
    const rows = buildTree({ collections, sites, quick: [], expanded: new Set(['c2']) })

    expect(rows.find((row) => row.name === 'Work')).toMatchObject({ siteCount: 2, expanded: true })
    expect(rows.find((row) => row.name === 'client')).toMatchObject({ siteCount: 0, expanded: false })
  })

  it('shows orphans at the top', () => {
    const rows = buildTree({
      collections: [collection('c1', 'Orphan', 'gone')],
      sites: [site('s1', 'Lost', 'gone')],
      quick: [],
      expanded: new Set(),
    })

    expect(shape(rows)).toEqual(['collection:Orphan@0', 'site:Lost@0'])
  })

  it('is empty for an empty workspace', () => {
    expect(buildTree({ collections: [], sites: [], quick: [], expanded: new Set() })).toEqual([])
  })
})
