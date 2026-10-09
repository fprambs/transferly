import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { useApi } from '@/lib/api/use-api'
import type { TransferRecord } from '@/lib/transport'

import { parentPath } from '../explorer/entries'
import { filesKey } from '../explorer/files-key'

import { resumeUpload, ResumeMismatchError, LegacyResumeError } from './resume'
import { forgetUpload, recallUpload } from './upload-sources'

/** Why a resume did not go through. */
export type ResumeFailure = 'mismatch' | 'legacy' | 'unknown'

/** What {@link useResume} offers. */
export interface Resume {
  /** Continues an upload; returns `true` when the user must first choose the file again. */
  resume: (record: TransferRecord) => boolean
  /** Continues the upload waiting for a file with the one the user chose. */
  chooseFile: (file: File) => void
  /** Why the last resume failed, if it did. */
  failure: ResumeFailure | null
  /** Clears the failure. */
  clearFailure: () => void
}

/** Tells whether an upload can be resumed at all: this page still has its file, or the file can be chosen again for a saved site. */
export function canResume(record: TransferRecord): boolean {
  return recallUpload(record.id) !== undefined || record.siteId !== undefined
}

/**
 * Resumes uploads that stopped.
 *
 * When this page still holds the file (the upload started here, and the page was not reloaded) it continues at once.
 * Otherwise the user chooses the file again, which is checked against the transfer before anything is sent.
 */
export function useResume(): Resume {
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<ResumeFailure | null>(null)
  const [awaiting, setAwaiting] = useState<TransferRecord | null>(null)

  const run = async (record: TransferRecord, file: File, siteId: string): Promise<void> => {
    setFailure(null)
    try {
      await resumeUpload(api, record, { file, siteId })
      forgetUpload(record.id)
      await queryClient.invalidateQueries({ queryKey: filesKey(record.workspaceId, siteId, parentPath(record.path)) })
    } catch (error) {
      setFailure(error instanceof ResumeMismatchError ? 'mismatch' : error instanceof LegacyResumeError ? 'legacy' : 'unknown')
    }
  }

  const resume = (record: TransferRecord): boolean => {
    const known = recallUpload(record.id)
    if (known === undefined) {
      setAwaiting(record)
      return true
    }
    void run(record, known.file, known.siteId)
    return false
  }

  const chooseFile = (file: File): void => {
    if (awaiting?.siteId !== undefined) {
      void run(awaiting, file, awaiting.siteId)
    }
    setAwaiting(null)
  }

  return { resume, chooseFile, failure, clearFailure: () => setFailure(null) }
}
