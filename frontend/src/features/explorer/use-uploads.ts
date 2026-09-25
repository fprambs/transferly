import { useState } from 'react'

import type { Api } from '@/lib/api/use-api'
import { ApiError } from '@/lib/transport'
import { uuid } from '@/lib/uuid'

import { forgetUpload, rememberUpload } from '../transfers/upload-sources'

import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'
import { joinPath } from './entries'

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

  return { upload, failures, conflict, clearFailures: () => setFailures([]) }
}
