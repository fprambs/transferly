import { createInstance, type i18n as I18n } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import enAdmin from './locales/en/admin.json'
import enAuth from './locales/en/auth.json'
import enCommon from './locales/en/common.json'
import enEditor from './locales/en/editor.json'
import enErrors from './locales/en/errors.json'
import enExplorer from './locales/en/explorer.json'
import enQuick from './locales/en/quick.json'
import enTeams from './locales/en/teams.json'
import enTransfers from './locales/en/transfers.json'
import enVault from './locales/en/vault.json'
import enSites from './locales/en/sites.json'
import enSync from './locales/en/sync.json'
import enSecurity from './locales/en/security.json'
import enSettings from './locales/en/settings.json'
import enShell from './locales/en/shell.json'

import { languages } from './languages'

export { languages, type Language } from './languages'

/** A language code the interface is translated into. */
export type SupportedLanguage = (typeof languages)[number]['code']

/** Codes of the languages the interface is translated into. English is the source language. */
export const supportedLanguages: readonly SupportedLanguage[] = languages.map((language) => language.code)

/** Namespace used when a component does not ask for another one. */
export const defaultNS = 'common'

/** The English messages by namespace, the source every other language translates; they type the keys of `t`. */
const english = {
  admin: enAdmin, auth: enAuth, common: enCommon, editor: enEditor, errors: enErrors, explorer: enExplorer, quick: enQuick, security: enSecurity,
  settings: enSettings, shell: enShell, sites: enSites, sync: enSync, teams: enTeams, transfers: enTransfers, vault: enVault,
} as const

/** The messages of a language by namespace, shaped as the English ones. */
export type Messages = typeof english

/** Every translation file under `locales/`, by path, such as `./locales/es/sites.json`. */
const files = import.meta.glob<object>('./locales/*/*.json', { eager: true, import: 'default' })

/** Groups the translation files of the languages other than English by language and namespace. */
function translations(): Record<string, Messages> {
  const byLanguage: Record<string, Record<string, object>> = {}
  for (const [path, messages] of Object.entries(files)) {
    const [, code, namespace] = /^\.\/locales\/([^/]+)\/([^/]+)\.json$/.exec(path) as RegExpExecArray
    if (code !== 'en') {
      byLanguage[code] = { ...byLanguage[code], [namespace]: messages }
    }
  }
  return byLanguage as Record<string, Messages>
}

/** Translation resources by language and namespace: English, then every folder of `locales/`. */
export const resources: { en: Messages } & Record<string, Messages> = { ...translations(), en: english }

/** Subset of an i18next instance needed to describe the document language. */
export type LanguageSource = Pick<I18n, 'resolvedLanguage' | 'language' | 'dir'>

/**
 * Creates an initialized, isolated i18next instance.
 *
 * @param language - Language to force; when omitted it is detected from local storage, then the browser.
 * @returns The initialized instance, ready for `I18nextProvider`.
 */
export async function createI18n(language?: SupportedLanguage): Promise<I18n> {
  const instance = createInstance()
  await instance
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
      resources,
      defaultNS,
      lng: language,
      fallbackLng: 'en',
      supportedLngs: [...supportedLanguages],
      nonExplicitSupportedLngs: true,
      interpolation: { escapeValue: false },
      detection: { order: ['localStorage', 'navigator'], caches: ['localStorage'] },
    })
  return instance
}

/**
 * Applies the active language and its writing direction to the document root.
 *
 * @param source - The i18next instance, or any object exposing the same language members.
 * @param root - Element receiving the `lang` and `dir` attributes; defaults to `<html>`.
 */
export function applyDocumentLanguage(
  source: LanguageSource,
  root: HTMLElement = document.documentElement,
): void {
  const language = source.resolvedLanguage ?? source.language
  root.lang = language
  root.dir = source.dir(language)
}

/**
 * Keeps the document language and direction in sync with the active language.
 *
 * @param instance - The i18next instance to observe.
 * @returns A function that stops the synchronization.
 */
export function bindDocumentLanguage(instance: I18n): () => void {
  const apply = () => applyDocumentLanguage(instance)
  apply()
  instance.on('languageChanged', apply)
  return () => instance.off('languageChanged', apply)
}
