import { createInstance, type i18n as I18n } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import enCommon from './locales/en/common.json'
import esCommon from './locales/es/common.json'

/** Languages the interface is translated into. English is the source language. */
export const supportedLanguages = ['en', 'es'] as const

/** A language code the interface is translated into. */
export type SupportedLanguage = (typeof supportedLanguages)[number]

/** Namespace used when a component does not ask for another one. */
export const defaultNS = 'common'

/** Translation resources by language and namespace. */
export const resources = {
  en: { common: enCommon },
  es: { common: esCommon },
} as const

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
      supportedLngs: supportedLanguages,
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
