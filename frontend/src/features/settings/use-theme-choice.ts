import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { sessionKey } from '@/app/session'
import { useApi } from '@/lib/api/use-api'
import { useUiStore } from '@/state/ui-store'

/** What {@link useThemeChoice} offers. */
export interface ThemeChoice {
  /** Applies a theme at once and saves it to the account. */
  choose: (themeId: string) => Promise<void>
  /** Whether the last choice could not be saved to the account, though it is applied here. */
  saveFailed: boolean
}

/** Chooses the interface theme: it is applied immediately and saved to the account so other browsers get it too. */
export function useThemeChoice(): ThemeChoice {
  const api = useApi()
  const queryClient = useQueryClient()
  const setTheme = useUiStore((state) => state.setTheme)
  const [saveFailed, setSaveFailed] = useState(false)

  const choose = async (themeId: string): Promise<void> => {
    setTheme(themeId)
    setSaveFailed(false)
    try {
      await api.auth.updateMe({ theme: themeId })
      await queryClient.invalidateQueries({ queryKey: sessionKey })
    } catch {
      setSaveFailed(true)
    }
  }

  return { choose, saveFailed }
}
