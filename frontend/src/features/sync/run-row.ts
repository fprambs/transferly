import type { Api } from '@/lib/api/use-api'
import { ApiError } from '@/lib/transport'
import { uuid } from '@/lib/uuid'

import { pathSegments, parentPath } from '../explorer/entries'
import { makeLocalDirectory, readLocalFile, removeLocal, saveToLocal } from '../local/local-fs'

import type { SyncRow } from './compare'
import { sideAbsolutePath, type SyncSide } from './sides'

/** Which folders were already made sure to exist during one run, so the same one is not asked for twice: remote folders keyed by
 * `workspaceId:siteId`, local folders by their absolute path. */
export interface EnsuredDirectories {
  /** Remote folders already made sure to exist, by site. */
  remote: Map<string, Set<string>>
  /** Local folders already made sure to exist. */
  local: Set<string>
}

/** Returns an empty tracker of the folders a run has already made sure to exist. */
export function newEnsuredDirectories(): EnsuredDirectories {
  return { remote: new Map(), local: new Set() }
}

/** Tells whether a failure means the path already exists, which an ensured folder is content with. */
function alreadyExists(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'path_exists'
}

/** Makes sure a remote folder and its ancestors exist, asking only for the ones this run has not already made. */
async function ensureRemoteFolder(api: Api, workspaceId: string, siteId: string, path: string, ensured: EnsuredDirectories): Promise<void> {
  const made = ensured.remote.get(siteId) ?? new Set<string>()
  ensured.remote.set(siteId, made)
  for (const segment of pathSegments(path).slice(1)) {
    if (made.has(segment.path)) {
      continue
    }
    try {
      await api.files.mkdir(workspaceId, siteId, segment.path)
    } catch (error) {
      if (!alreadyExists(error)) {
        throw error
      }
    }
    made.add(segment.path)
  }
}

/** Makes sure a local folder and its ancestors exist, asking only for the ones this run has not already made. */
async function ensureLocalFolder(root: FileSystemDirectoryHandle, path: string, ensured: EnsuredDirectories): Promise<void> {
  for (const segment of pathSegments(path).slice(1)) {
    if (ensured.local.has(segment.path)) {
      continue
    }
    await makeLocalDirectory(root, parentPath(segment.path), segment.name)
    ensured.local.add(segment.path)
  }
}

/** Removes the file of a delete row from the side it no longer belongs on. */
async function deleteFrom(api: Api, target: SyncSide, row: SyncRow): Promise<void> {
  const absolute = sideAbsolutePath(target, row.relativePath)
  const name = absolute.slice(absolute.lastIndexOf('/') + 1)
  if (target.kind === 'remote') {
    await api.files.remove(target.workspaceId, target.siteId, absolute, false)
    return
  }
  await removeLocal(target.root, parentPath(absolute), name)
}

/** How long to wait between two checks of a copy still in progress, in milliseconds. */
const pollEvery = 150

/** Waits for a site-to-site copy to leave the server's queue, since the request that starts it only accepts the job; throws with the
 * copy's own outcome when it did not finish. */
async function waitForTransfer(api: Api, transferId: string): Promise<void> {
  for (;;) {
    const transfer = await api.transfers.get(transferId)
    if (transfer.status === 'done') {
      return
    }
    if (transfer.status !== 'queued' && transfer.status !== 'running') {
      throw new ApiError({ type: 'about:blank', title: `the copy did not finish: ${transfer.status}`, status: 422, code: transfer.errorCode || 'unknown' })
    }
    await new Promise((resolve) => setTimeout(resolve, pollEvery))
  }
}

/** Writes a file from the source side onto the target side, making its folder first. A dual pane never opens two local folders at
 * once (ADR-0008), so that combination is refused rather than silently handled. */
async function writeInto(api: Api, source: SyncSide, target: SyncSide, row: SyncRow): Promise<void> {
  const sourcePath = sideAbsolutePath(source, row.relativePath)
  const targetPath = sideAbsolutePath(target, row.relativePath)
  const overwrite = row.action === 'update'
  if (target.kind === 'remote' && source.kind === 'remote') {
    const transferId = uuid()
    await api.copies.create(target.workspaceId, { sourceSiteId: source.siteId, sourcePath, targetSiteId: target.siteId, targetPath, policy: 'overwrite', verify: false, transferId })
    await waitForTransfer(api, transferId)
    return
  }
  if (target.kind === 'remote' && source.kind === 'local') {
    const file = await readLocalFile(source.root, sourcePath)
    await api.files.upload(target.workspaceId, target.siteId, targetPath, file, { mode: overwrite ? 'overwrite' : 'create' })
    return
  }
  if (target.kind === 'local' && source.kind === 'remote') {
    const name = targetPath.slice(targetPath.lastIndexOf('/') + 1)
    const stream = await api.files.download(source.workspaceId, source.siteId, sourcePath, uuid())
    await saveToLocal(target.root, parentPath(targetPath), name, stream, overwrite)
    return
  }
  throw new Error('cannot sync between two local folders')
}

/**
 * Runs one row of a sync plan: creates or updates the file on the target side from the source side, making the target folder
 * first, or removes it from the target side for a delete row. `ensured` is shared across a whole run so the same folder is
 * never asked for twice.
 */
export async function runSyncRow(api: Api, source: SyncSide, target: SyncSide, row: SyncRow, ensured: EnsuredDirectories): Promise<void> {
  if (row.action === 'delete') {
    await deleteFrom(api, target, row)
    return
  }
  const folder = parentPath(sideAbsolutePath(target, row.relativePath))
  if (target.kind === 'remote') {
    await ensureRemoteFolder(api, target.workspaceId, target.siteId, folder, ensured)
  } else {
    await ensureLocalFolder(target.root, folder, ensured)
  }
  await writeInto(api, source, target, row)
}
