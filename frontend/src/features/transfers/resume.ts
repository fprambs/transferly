import type { Api } from '@/lib/api/use-api'
import { ApiError, type TransferRecord } from '@/lib/transport'

/** Raised when the file chosen to resume an upload is not the one that was being uploaded. */
export class ResumeMismatchError extends Error {
  /** Creates the error. */
  constructor() {
    super('The file is not the one that was being uploaded')
    this.name = 'ResumeMismatchError'
  }
}

/** An older upload used an unowned staging file that this version cannot safely adopt. */
export class LegacyResumeError extends Error {}

/** Statuses of an upload that stopped before it finished, and can be continued. */
const stoppedStatuses: readonly TransferRecord['status'][] = ['failed', 'interrupted', 'canceled']

/** Tells whether a transfer is an upload that stopped before it finished. */
export function isStoppedUpload(record: TransferRecord): boolean {
  return record.direction === 'upload' && stoppedStatuses.includes(record.status)
}

/** Where the rest of the file is sent from, and how. */
interface ResumePlan {
  /** `resume` continues the partial file; `create` starts over because it is gone. */
  mode: 'resume' | 'create'
  /** First byte still to send. */
  offset: number
}

/**
 * Asks the server how much of the file arrived, and plans the rest.
 *
 * An upload stages into an adjacent "transferly-<id>.part" file and only becomes the real file once it finishes, so
 * an interrupted upload is found there, not at the final path; a partial file that is gone means starting over.
 */
async function planResume(api: Api, record: TransferRecord, siteId: string, size: number): Promise<ResumePlan> {
  try {
    const partial = await api.files.stat(record.workspaceId, siteId, `${record.path.slice(0, record.path.lastIndexOf('/') + 1)}transferly-${record.id}.part`)
    return { mode: 'resume', offset: Math.min(partial.size, size) }
  } catch (error) {
    if (error instanceof ApiError && error.code === 'path_not_found') {
      try {
        await api.files.stat(record.workspaceId, siteId, `${record.path}.part`)
      } catch (legacyError) {
        if (legacyError instanceof ApiError && legacyError.code === 'path_not_found') return { mode: 'create', offset: 0 }
        throw legacyError
      }
      throw new LegacyResumeError()
    }
    throw error
  }
}

/**
 * Continues an upload that stopped, sending only the bytes the server does not have yet.
 *
 * The transfer keeps its identifier, so the dock follows it as one transfer. The size the server holds is asked for first,
 * because it may differ from what the last progress report said.
 *
 * @throws {ResumeMismatchError} When the file does not match the transfer by name and, when known, size.
 */
export async function resumeUpload(api: Api, record: TransferRecord, source: { file: File; siteId: string }): Promise<void> {
  const { file, siteId } = source
  if (file.name !== record.name || (record.totalBytes !== undefined && file.size !== record.totalBytes)) {
    throw new ResumeMismatchError()
  }
  const plan = await planResume(api, record, siteId, file.size)
  await api.files.upload(record.workspaceId, siteId, record.path, plan.offset === 0 ? file : file.slice(plan.offset), {
    mode: plan.mode,
    offset: plan.mode === 'resume' ? plan.offset : undefined,
    transferId: record.id,
    size: file.size,
  })
}
