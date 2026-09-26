import { useEffect } from 'react'

import { findTheme } from '@/lib/theme'
import { useUiStore } from '@/state/ui-store'

import { useSession } from '../auth/use-session'

/**
 * Wears the theme saved in the account when the session becomes known or the account's theme changes.
 *
 * The account wins over what this browser remembered, so signing in on another browser brings the same look.
 */
export function useAccountTheme(): void {
  const session = useSession()
  const setTheme = useUiStore((state) => state.setTheme)
  const accountTheme = session?.user.theme

  useEffect(() => {
    if (accountTheme !== undefined && findTheme(accountTheme) !== undefined) {
      setTheme(accountTheme)
    }
  }, [accountTheme, setTheme])
}
