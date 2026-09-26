import type { RemoteEntry } from '@/lib/api'
import type { Api } from '@/lib/api/use-api'
import { ApiError } from '@/lib/transport'
import { uuid } from '@/lib/uuid'

import { explorerErrorKeyOf, type ExplorerErrorKey } from '../explorer/errors'

import { freeLocalName, LocalNameTaken, localNameExists, makeLocalDirectory, saveToLocal } from './local-fs'
import { maxTreeEntries, TreeTooLarge } from './tree'

/** What to do with a file that already exists in the local folder. */
export type LocalPolicy = 'skip' | 'overwrite' | 'rename' | 'fail'

/** A file that could not be saved, and why. */
export interface SaveFailure {
  /** Path of the file below the folder that was downloaded. */
  name: string
  /** Translation key of the reason. */
  key: ExplorerErrorKey | 'localFailure'
}

/** Where a folder comes from and where it goes. */
export interface FolderDownload {
  /** Typed client of the backend. */
  api: Api
  /** Workspace of the connection. */
  workspaceId: string
  /** Saved site or quick connection the folder lives on. */
  siteId: string
  /** Absolute path of the remote folder. */
  source: string
  /** The folder the user chose for the local pane. */
  root: FileSystemDirectoryHandle
  /** Absolute path, below that folder, of the local folder that receives it. */
  destination: string
  /** What to do with files that exist. */
  policy: LocalPolicy
}

/** Joins a folder path and a name. */
function join(directory: string, name: string): string {
  return directory === '/' ? `/${name}` : `${directory}/${name}`
}

/** Explains why a file could not be saved. */
function reason(error: unknown): SaveFailure['key'] {
  if (error instanceof LocalNameTaken) {
    return 'path_exists'
  }
  return error instanceof ApiError ? explorerErrorKeyOf(error) : 'localFailure'
}

/**
 * Downloads a remote folder, with everything below it, into a folder of the user's computer.
 *
 * Each file is fetched as a download of its own, so the transfers dock follows them. Local folders that exist are used, and
 * files that exist follow the policy; nothing outside the destination is touched. The failures are returned, one per file that
 * could not be saved; with the fail policy the first one stops the download.
 */
export async function downloadFolderToLocal(job: FolderDownload): Promise<SaveFailure[]> {
  const failures: SaveFailure[] = []
  const name = job.source.slice(job.source.lastIndexOf('/') + 1)
  let count = 0

  const saveFile = async (remote: RemoteEntry, folder: string): Promise<void> => {
    let target = remote.name
    const exists = await localNameExists(job.root, folder, target)
    if (exists && job.policy === 'skip') {
      return
    }
    if (exists && job.policy === 'rename') {
      target = await freeLocalName(job.root, folder, target)
    }
    const stream = await job.api.files.download(job.workspaceId, job.siteId, remote.path, uuid())
    await saveToLocal(job.root, folder, target, stream, job.policy === 'overwrite')
  }

  const walk = async (remote: string, folder: string, label: string): Promise<boolean> => {
    const entries = await job.api.files.list(job.workspaceId, job.siteId, remote)
    count += entries.length
    if (count > maxTreeEntries) {
      throw new TreeTooLarge()
    }
    for (const entry of entries) {
      const shown = `${label}${entry.name}`
      try {
        if (entry.type === 'directory') {
          await makeLocalDirectory(job.root, folder, entry.name)
          if (!(await walk(entry.path, join(folder, entry.name), `${shown}/`))) {
            return false
          }
        } else if (entry.type === 'file') {
          await saveFile(entry, folder)
        }
      } catch (error) {
        if (error instanceof TreeTooLarge) {
          throw error
        }
        failures.push({ name: shown, key: reason(error) })
        if (job.policy === 'fail') {
          return false
        }
      }
    }
    return true
  }

  try {
    await makeLocalDirectory(job.root, job.destination, name)
    await walk(job.source, join(job.destination, name), '')
  } catch (error) {
    failures.push({ name, key: error instanceof TreeTooLarge ? 'tree_too_large' : reason(error) })
  }
  return failures
}
