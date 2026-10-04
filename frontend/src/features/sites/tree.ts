import { transportOf, type Transport } from '@/components/brand/transport'
import type { Collection, QuickConnection, Site, SiteColor } from '@/lib/api'
import { hostOf } from '@/features/connectors/settings'

/** A collection row of the sites tree. */
export interface CollectionNode {
  /** Row kind. */
  kind: 'collection'
  /** Collection identifier. */
  id: string
  /** Collection name. */
  name: string
  /** Nesting level, zero at the top. */
  depth: number
  /** Whether its children are listed. */
  expanded: boolean
  /** Number of sites inside it, at any depth. */
  siteCount: number
}

/** A saved site row of the sites tree. */
export interface SiteNode {
  /** Row kind. */
  kind: 'site'
  /** Site identifier. */
  id: string
  /** Site name. */
  name: string
  /** Nesting level, zero at the top. */
  depth: number
  /** Host shown next to the name. */
  host: string
  /** The type of the site's connector, whose icon the row shows. */
  connector: string
  /** How the site reaches its server, shown by the badge of its icon. */
  transport: Transport
  /** The label color of the site, left out when it has none. */
  color?: SiteColor
  /** Collection that holds the site, left out at the top level. */
  collectionId?: string
}

/** A quick connection row of the sites tree; it is not saved. */
export interface QuickNode {
  /** Row kind. */
  kind: 'quick'
  /** Quick connection identifier. */
  id: string
  /** Host of the connection. */
  name: string
  /** Nesting level, always zero. */
  depth: number
}

/** One visible row of the sites tree. */
export type TreeNode = CollectionNode | SiteNode | QuickNode

/** What the tree is built from. */
export interface TreeInput {
  /** Every collection of the workspace. */
  collections: readonly Collection[]
  /** Every saved site of the workspace. */
  sites: readonly Site[]
  /** Quick connections that are open. */
  quick: readonly QuickConnection[]
  /** Identifiers of the collections that are open. */
  expanded: ReadonlySet<string>
}

/** Compares two named things alphabetically, ignoring case and accents. */
function byName(a: { name: string }, b: { name: string }): number {
  return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
}

/** Counts the sites below a collection at any depth. */
function countSites(id: string, children: Map<string | undefined, Collection[]>, sites: Map<string | undefined, Site[]>): number {
  const own = sites.get(id)?.length ?? 0
  return (children.get(id) ?? []).reduce((total, child) => total + countSites(child.id, children, sites), own)
}

/** Groups items by a key, keeping them sorted by name. */
function group<T extends { name: string }>(items: readonly T[], key: (item: T) => string | undefined): Map<string | undefined, T[]> {
  const groups = new Map<string | undefined, T[]>()
  for (const item of [...items].sort(byName)) {
    groups.set(key(item), [...(groups.get(key(item)) ?? []), item])
  }
  return groups
}

/** Appends the rows of one level and, for open collections, of the levels below. */
function visit(
  parent: string | undefined,
  depth: number,
  input: TreeInput,
  children: Map<string | undefined, Collection[]>,
  sites: Map<string | undefined, Site[]>,
  rows: TreeNode[],
): void {
  for (const collection of children.get(parent) ?? []) {
    const expanded = input.expanded.has(collection.id)
    rows.push({
      kind: 'collection',
      id: collection.id,
      name: collection.name,
      depth,
      expanded,
      siteCount: countSites(collection.id, children, sites),
    })
    if (expanded) {
      visit(collection.id, depth + 1, input, children, sites, rows)
    }
  }
  for (const site of sites.get(parent) ?? []) {
    rows.push({ kind: 'site', id: site.id, name: site.name, depth, host: hostOf(site), connector: site.connector, transport: transportOf(site), color: site.color, collectionId: site.collectionId })
  }
}

/**
 * Flattens collections, sites and quick connections into the rows the sidebar shows.
 *
 * Quick connections come first. Then, per level, collections come before sites, each sorted by name.
 * A collection whose parent is unknown is shown at the top, so no site is ever hidden.
 */
export function buildTree(input: TreeInput): TreeNode[] {
  const known = new Set(input.collections.map((collection) => collection.id))
  const children = group(input.collections, (collection) =>
    collection.parentId !== undefined && known.has(collection.parentId) ? collection.parentId : undefined,
  )
  const sites = group(input.sites, (site) =>
    site.collectionId !== undefined && known.has(site.collectionId) ? site.collectionId : undefined,
  )
  const rows: TreeNode[] = [...input.quick].sort(byName).map((connection) => ({
    kind: 'quick',
    id: connection.id,
    name: connection.name,
    depth: 0,
  }))
  visit(undefined, 0, input, children, sites, rows)
  return rows
}
