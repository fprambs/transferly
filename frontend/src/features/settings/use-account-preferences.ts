import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { sessionKey } from '@/app/session'
import { supportedLanguages, type SupportedLanguage } from '@/i18n'
import type { Preferences } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { findTheme } from '@/lib/theme'
import { useUiStore } from '@/state/ui-store'

import { useSession } from '../auth/use-session'

/** Language of a new account, set by the server before the person chooses one. */
const accountDefaultLanguage: SupportedLanguage = 'en'

/** The interface language a BCP 47 tag asks for, such as es for es-CL, or `undefined` when the interface is not translated into it. */
export function interfaceLanguageOf(tag: string | undefined): SupportedLanguage | undefined {
  const base = tag?.split('-')[0].toLowerCase()
  return supportedLanguages.find((language) => language === base)
}

/**
 * Wears the preferences saved in the account when the session becomes known or they change: the theme, the interface language and what a
 * double click on a file does (4.14a). The account wins over what this browser remembered, so signing in on another browser brings the same
 * setup.
 *
 * The language and the double click action lived in the browser before 4.14a, and every account starts in English. The first time an
 * account is seen here, what this browser had that the account still has at its default (another language, opening files in the editor)
 * is saved to the account instead of being lost. From then on the account alone decides, each time what the account says changes: a
 * choice made here keeps applying while it is on its way to the account.
 */
export function useAccountPreferences(): void {
  const session = useSession()
  const api = useApi()
  const queryClient = useQueryClient()
  const { i18n } = useTranslation()
  const setTheme = useUiStore((state) => state.setTheme)
  const setFileDoubleClick = useUiStore((state) => state.setFileDoubleClick)
  const markSynced = useUiStore((state) => state.markPreferencesSynced)
  const accountTheme = session?.user.theme
  const accountLocale = session?.user.locale
  const accountDoubleClick = session?.user.fileDoubleClick
  const applied = useRef<string | null>(null)

  useEffect(() => {
    if (accountTheme !== undefined && findTheme(accountTheme) !== undefined) {
      setTheme(accountTheme)
    }
  }, [accountTheme, setTheme])

  useEffect(() => {
    const account = `${accountLocale}/${accountDoubleClick}`
    if (accountLocale === undefined || accountDoubleClick === undefined || applied.current === account) {
      return
    }
    applied.current = account
    const { fileDoubleClick, preferencesSynced } = useUiStore.getState()
    const browserLanguage = i18n.resolvedLanguage as SupportedLanguage
    const accountLanguage = interfaceLanguageOf(accountLocale)
    markSynced()
    const moved: Preferences = {}
    if (!preferencesSynced && fileDoubleClick === 'edit' && accountDoubleClick === 'download') {
      moved.fileDoubleClick = 'edit'
    }
    if (!preferencesSynced && accountLocale === accountDefaultLanguage && browserLanguage !== accountDefaultLanguage) {
      moved.locale = browserLanguage
    }
    if (moved.fileDoubleClick === undefined) {
      setFileDoubleClick(accountDoubleClick)
    }
    if (moved.locale === undefined && accountLanguage !== undefined && accountLanguage !== browserLanguage) {
      void i18n.changeLanguage(accountLanguage)
    }
    if (Object.keys(moved).length > 0) {
      void api.auth
        .updateMe(moved)
        .then(() => queryClient.invalidateQueries({ queryKey: sessionKey }))
        .catch(() => undefined)
    }
  }, [accountLocale, accountDoubleClick, api, queryClient, i18n, setFileDoubleClick, markSynced])
}
