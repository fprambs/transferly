import { useState } from 'react'

import type { Api } from '@/lib/api/use-api'
import { ApiError } from '@/lib/transport'
import { uuid } from '@/lib/uuid'

import type { UploadTree } from '../local/tree'
import { forgetUpload, rememberUpload } from '../transfers/upload-sources'

import { freeName } from './free-name'
import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'
import { joinPath } from './entries'

/** What to do with a file that already exists when a folder is uploaded. */
export type TreePolicy = 'skip' | 'overwrite' | 'rename' | 'fail'

/** A file that could not be uploaded, and why. */
export interface UploadFailure {
  /** Name of the file. */
  name: string
  /** Translation key of the reason. */
  key: ExplorerErrorKey
}

/** A file that already exists on the server, waiting for the user to decide. */
export interface UploadConflict {
  /** The file the user wants to upload. */
  file: File
  /** Answers the conflict: overwrite the file on the server, or skip it. */
  decide: (overwrite: boolean) => void
}

/** What {@link useUploads} offers. */
export interface Uploads {
  /** Uploads files one after another into the folder in view. */
  upload: (files: readonly File[]) => Promise<void>
  /** Uploads a folder, with everything below it, into the folder in view; the policy says what to do with files that exist. */
  uploadFolder: (tree: UploadTree, policy: TreePolicy) => Promise<void>
  /** Files that failed since the list was last cleared. */
  failures: UploadFailure[]
  /** The file that needs an overwrite decision, if any. */
  conflict: UploadConflict | null
  /** Clears the list of failures. */
  clearFailures: () => void
}

/** Where the uploads go and what to do afterwards. */
export interface UploadTarget {
  /** Typed client of the backend. */
  api: Api
  /** Workspace of the connection. */
  workspaceId: string
  /** Saved site or quick connection. */
  siteId: string
  /** Folder that receives the files. */
  path: string
  /** Called after each file finished, so the listing can reload. */
  onUploaded: () => Promise<void>
}

/** Tells whether an error means the file already exists on the server. */
function isExisting(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'path_exists'
}

/**
 * Uploads files to the folder in view, one after another.
 *
 * A file that exists is never overwritten silently: the upload waits for {@link Uploads.resolve}. Progress is reported
 * by the server over the event stream, so the transfers dock follows it without help from here. A file whose upload
 * fails stays in the upload sources, so the dock can resume it.
 */
export function useUploads({ api, workspaceId, siteId, path, onUploaded }: UploadTarget): Uploads {
  const [failures, setFailures] = useState<UploadFailure[]>([])
  const [conflict, setConflict] = useState<UploadConflict | null>(null)

  const ask = (file: File): Promise<boolean> =>
    new Promise((resolve) => {
      setConflict({
        file,
        decide: (overwrite) => {
          setConflict(null)
          resolve(overwrite)
        },
      })
    })

  const send = async (file: File, mode: 'create' | 'overwrite', transferId: string): Promise<void> => {
    rememberUpload(transferId, { file, workspaceId, siteId })
    await api.files.upload(workspaceId, siteId, joinPath(path, file.name), file, { mode, transferId, size: file.size })
    forgetUpload(transferId)
  }

  const one = async (file: File): Promise<void> => {
    try {
      const first = uuid()
      try {
        await send(file, 'create', first)
      } catch (error) {
        if (!isExisting(error)) {
          throw error
        }
        forgetUpload(first)
        if (!(await ask(file))) {
          return
        }
        await send(file, 'overwrite', uuid())
      }
      await onUploaded()
    } catch (error) {
      setFailures((current) => [...current, { name: file.name, key: explorerErrorKeyOf(error) }])
    }
  }

  const upload = async (files: readonly File[]): Promise<void> => {
    for (const file of files) {
      await one(file)
    }
  }

  const fail = (name: string, error: unknown): void => setFailures((current) => [...current, { name, key: explorerErrorKeyOf(error) }])

  const exists = async (target: string): Promise<boolean> => {
    try {
      await api.files.stat(workspaceId, siteId, target)
      return true
    } catch (error) {
      if (error instanceof ApiError && error.code === 'path_not_found') {
        return false
      }
      throw error
    }
  }

  const sendTo = async (file: File, target: string, mode: 'create' | 'overwrite'): Promise<void> => {
    const transferId = uuid()
    rememberUpload(transferId, { file, workspaceId, siteId })
    await api.files.upload(workspaceId, siteId, target, file, { mode, transferId, size: file.size })
    forgetUpload(transferId)
  }

  const ensureFolder = async (target: string): Promise<void> => {
    try {
      await api.files.mkdir(workspaceId, siteId, target)
    } catch (error) {
      if (!isExisting(error)) {
        throw error
      }
    }
  }

  const uploadFolder = async (tree: UploadTree, policy: TreePolicy): Promise<void> => {
    const base = joinPath(path, tree.name)
    try {
      await ensureFolder(base)
      for (const dir of tree.dirs) {
        await ensureFolder(joinPath(base, dir))
      }
    } catch (error) {
      fail(tree.name, error)
      return
    }
    for (const { path: relative, file } of tree.files) {
      const target = joinPath(base, relative)
      try {
        try {
          await sendTo(file, target, policy === 'overwrite' ? 'overwrite' : 'create')
        } catch (error) {
          if (!isExisting(error) || policy === 'fail') {
            throw error
          }
          if (policy === 'rename') {
            const renamed = await freeName(exists, target)
            if (renamed === undefined) {
              throw new ApiError({ type: 'about:blank', title: 'Conflict', status: 409, code: 'path_exists' })
            }
            await sendTo(file, renamed, 'create')
          }
        }
      } catch (error) {
        fail(relative, error)
        if (policy === 'fail') {
          break
        }
      }
    }
    await onUploaded()
  }

  return { upload, uploadFolder, failures, conflict, clearFailures: () => setFailures([]) }
}
