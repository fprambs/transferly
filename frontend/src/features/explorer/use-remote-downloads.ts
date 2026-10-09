import { useCallback, useState } from 'react'

import type { RemoteEntry } from '@/lib/api'
import type { Api } from '@/lib/api/use-api'
import { uuid } from '@/lib/uuid'

import type { RequestedDownload } from './DownloadNotices'
import { collectFiles, eachDelayMs, maxEachFiles, preparingTimeoutMs, startDownload, TooManyFiles } from './download'
import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'

/** Progress or guided failure of downloading a directory file by file. */
export type DirectoryDownloadState = { count: number } | ExplorerErrorKey | 'tooMany' | null

/** Download operations and their feedback, independent of the explorer's table and dialogs. */
export interface RemoteDownloads {
  /** Browser downloads awaiting a transfer event or their preparation timeout. */
  preparing: RequestedDownload[]
  /** Directory download progress or its failure. */
  each: DirectoryDownloadState
  /** Removes a preparation notice once its transfer appeared or its timeout elapsed. */
  dismissDownload: (id: string) => void
  /** Removes a directory download failure after the user reads it. */
  dismissEach: () => void
  /** Starts one file or a streamed ZIP of a directory. */
  download: (entry: RemoteEntry) => void
  /** Collects a bounded directory tree and starts its files sequentially. */
  downloadEach: (folder: RemoteEntry) => Promise<void>
}

/** Coordinates downloads and their feedback without owning navigation, selection or rendering. */
export function useRemoteDownloads(api: Api, workspaceId: string, siteId: string): RemoteDownloads {
  const [preparing, setPreparing] = useState<RequestedDownload[]>([])
  const [each, setEach] = useState<DirectoryDownloadState>(null)
  const dismissDownload = useCallback((id: string) => setPreparing((list) => list.filter((item) => item.id !== id)), [])
  const download = (entry: RemoteEntry): void => {
    const id = uuid()
    setPreparing((list) => [...list, { id, name: entry.name }])
    setTimeout(() => dismissDownload(id), preparingTimeoutMs)
    startDownload(entry.type === 'directory' ? api.files.archiveUrl(workspaceId, siteId, entry.path, id) : api.files.downloadUrl(workspaceId, siteId, entry.path, id))
  }
  const downloadEach = async (folder: RemoteEntry): Promise<void> => {
    try {
      const found = await collectFiles((dir, signal) => api.files.list(workspaceId, siteId, dir, signal), folder.path, maxEachFiles)
      setEach({ count: found.length })
      for (const file of found) {
        startDownload(api.files.downloadUrl(workspaceId, siteId, file.path, uuid()))
        await new Promise((resolve) => setTimeout(resolve, eachDelayMs))
      }
      setEach(null)
    } catch (error) {
      setEach(error instanceof TooManyFiles ? 'tooMany' : explorerErrorKeyOf(error))
    }
  }
  return { preparing, each, download, downloadEach, dismissDownload, dismissEach: () => setEach(null) }
}
