import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { sessionKey } from '@/app/session'
import type { Preferences } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** What {@link useSavePreferences} offers. */
export interface SavePreferences {
  /** Saves preferences to the account; the caller has already applied them in this browser. */
  save: (preferences: Preferences) => Promise<void>
  /** Whether the last save failed, though what was chosen applies in this browser. */
  saveFailed: boolean
}

/** Saves preferences chosen on the settings page to the account, so other browsers get them too (4.14a). */
export function useSavePreferences(): SavePreferences {
  const api = useApi()
  const queryClient = useQueryClient()
  const [saveFailed, setSaveFailed] = useState(false)

  const save = async (preferences: Preferences): Promise<void> => {
    setSaveFailed(false)
    try {
      await api.auth.updateMe(preferences)
      await queryClient.invalidateQueries({ queryKey: sessionKey })
    } catch {
      setSaveFailed(true)
    }
  }

  return { save, saveFailed }
}
