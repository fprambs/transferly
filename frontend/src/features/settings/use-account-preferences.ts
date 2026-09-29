import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'

import { sessionKey } from '@/app/session'
import { useApi } from '@/lib/api/use-api'
import { findTheme } from '@/lib/theme'
import { useUiStore } from '@/state/ui-store'

import { useSession } from '../auth/use-session'

/**
 * Wears the preferences saved in the account when the session becomes known or they change: the theme and what a double click on a file
 * does (4.14a). The account wins over what this browser remembered, so signing in on another browser brings the same setup.
 *
 * The double click action lived in the browser before 4.14a. The first time an account is seen here, a choice to open files in the editor
 * that the account does not have yet is saved to it instead of being lost; download, the default, needs no moving.
 */
export function useAccountPreferences(): void {
  const session = useSession()
  const api = useApi()
  const queryClient = useQueryClient()
  const setTheme = useUiStore((state) => state.setTheme)
  const setFileDoubleClick = useUiStore((state) => state.setFileDoubleClick)
  const markSynced = useUiStore((state) => state.markFileDoubleClickSynced)
  const accountTheme = session?.user.theme
  const accountDoubleClick = session?.user.fileDoubleClick

  useEffect(() => {
    if (accountTheme !== undefined && findTheme(accountTheme) !== undefined) {
      setTheme(accountTheme)
    }
  }, [accountTheme, setTheme])

  useEffect(() => {
    if (accountDoubleClick === undefined) {
      return
    }
    const { fileDoubleClick, fileDoubleClickSynced } = useUiStore.getState()
    markSynced()
    if (!fileDoubleClickSynced && fileDoubleClick === 'edit' && accountDoubleClick === 'download') {
      void api.auth
        .updateMe({ fileDoubleClick: 'edit' })
        .then(() => queryClient.invalidateQueries({ queryKey: sessionKey }))
        .catch(() => undefined)
      return
    }
    setFileDoubleClick(accountDoubleClick)
  }, [accountDoubleClick, api, queryClient, setFileDoubleClick, markSynced])
}
