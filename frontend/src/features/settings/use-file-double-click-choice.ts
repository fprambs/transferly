import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { sessionKey } from '@/app/session'
import { useApi } from '@/lib/api/use-api'
import { useUiStore, type FileDoubleClick } from '@/state/ui-store'

/** What {@link useFileDoubleClickChoice} offers. */
export interface FileDoubleClickChoice {
  /** Applies the action at once and saves it to the account. */
  choose: (action: FileDoubleClick) => Promise<void>
  /** Whether the last choice could not be saved to the account, though it applies in this browser. */
  saveFailed: boolean
}

/** Chooses what a double click on a file does: it applies immediately and is saved to the account so other browsers get it too (4.14a). */
export function useFileDoubleClickChoice(): FileDoubleClickChoice {
  const api = useApi()
  const queryClient = useQueryClient()
  const setFileDoubleClick = useUiStore((state) => state.setFileDoubleClick)
  const [saveFailed, setSaveFailed] = useState(false)

  const choose = async (action: FileDoubleClick): Promise<void> => {
    setFileDoubleClick(action)
    setSaveFailed(false)
    try {
      await api.auth.updateMe({ fileDoubleClick: action })
      await queryClient.invalidateQueries({ queryKey: sessionKey })
    } catch {
      setSaveFailed(true)
    }
  }

  return { choose, saveFailed }
}
