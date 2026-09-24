import { describe, expect, it } from 'vitest'

import {
  applyDocumentLanguage,
  bindDocumentLanguage,
  createI18n,
  resources,
  supportedLanguages,
  type LanguageSource,
} from './index'

/** Flattens nested translation messages into `path.to.key` entries. */
function flatten(messages: object, prefix = ''): Record<string, string> {
  return Object.entries(messages).reduce<Record<string, string>>((flat, [key, value]) => {
    const path = prefix === '' ? key : `${prefix}.${key}`
    return typeof value === 'string' ? { ...flat, [path]: value } : { ...flat, ...flatten(value, path) }
  }, {})
}

/** Lists the interpolation placeholders of a message in a stable order. */
function placeholders(message: string): string[] {
  return [...message.matchAll(/\{\{\s*(\w+)\s*\}\}/g)].map((match) => match[1]).sort()
}

/**
 * Covers the translation files of every supported language.
 *
 * Scenarios:
 * - every language has the same namespaces and keys as the English source
 * - no message is empty
 * - every message keeps the interpolation placeholders of the English source
 */
describe('translation resources', () => {
  const source = resources.en
  const others = supportedLanguages.filter((language) => language !== 'en')

  it.each(others)('%s has the same namespaces as English', (language) => {
    expect(Object.keys(resources[language]).sort()).toEqual(Object.keys(source).sort())
  })

  it.each(others)('%s has the same keys, no empty messages and the same placeholders', (language) => {
    for (const [namespace, messages] of Object.entries(source)) {
      const reference = flatten(messages)
      const translated = flatten(resources[language][namespace as keyof typeof source])

      expect(Object.keys(translated).sort(), `${language}/${namespace} keys`).toEqual(
        Object.keys(reference).sort(),
      )
      for (const [key, message] of Object.entries(translated)) {
        expect(message.trim(), `${language}/${namespace}.${key}`).not.toBe('')
        expect(placeholders(message), `${language}/${namespace}.${key} placeholders`).toEqual(
          placeholders(reference[key]),
        )
      }
    }
  })
})

/**
 * Covers {@link createI18n}.
 *
 * Scenarios:
 * - a forced language is used and translates messages
 * - regional variants resolve to the supported base language
 * - an unsupported language falls back to English
 * - instances are isolated from one another
 */
describe('createI18n', () => {
  it('translates with the forced language', async () => {
    const instance = await createI18n('es')

    expect(instance.resolvedLanguage).toBe('es')
    expect(instance.t(($) => $.language.label)).toBe('Idioma')
  })

  it('resolves regional variants to the supported language', async () => {
    const instance = await createI18n()
    await instance.changeLanguage('es-CL')

    expect(instance.resolvedLanguage).toBe('es')
  })

  it('falls back to English for unsupported languages', async () => {
    const instance = await createI18n()
    await instance.changeLanguage('fr')

    expect(instance.resolvedLanguage).toBe('en')
    expect(instance.t(($) => $.language.label)).toBe('Language')
  })

  it('does not share language state between instances', async () => {
    const spanish = await createI18n('es')
    const english = await createI18n('en')

    expect(spanish.resolvedLanguage).toBe('es')
    expect(english.resolvedLanguage).toBe('en')
  })
})

/**
 * Covers {@link applyDocumentLanguage} and {@link bindDocumentLanguage}.
 *
 * Scenarios:
 * - the language and a left-to-right direction are written to the root element
 * - the language member is used when no resolved language exists
 * - a right-to-left language sets the direction accordingly
 * - the binding applies the current language and follows later changes
 * - after unbinding, changes are no longer applied
 */
describe('document language', () => {
  it('writes the language and left-to-right direction', () => {
    const root = document.createElement('html')
    const source: LanguageSource = { resolvedLanguage: 'es', language: 'es', dir: () => 'ltr' }

    applyDocumentLanguage(source, root)

    expect(root.lang).toBe('es')
    expect(root.dir).toBe('ltr')
  })

  it('uses the language member when no resolved language exists', () => {
    const root = document.createElement('html')
    const source: LanguageSource = { resolvedLanguage: undefined, language: 'es', dir: () => 'ltr' }

    applyDocumentLanguage(source, root)

    expect(root.lang).toBe('es')
  })

  it('sets a right-to-left direction for right-to-left languages', () => {
    const root = document.createElement('html')
    const source: LanguageSource = { resolvedLanguage: 'ar', language: 'ar', dir: () => 'rtl' }

    applyDocumentLanguage(source, root)

    expect(root.dir).toBe('rtl')
  })

  it('follows language changes until it is unbound', async () => {
    const instance = await createI18n('en')
    const unbind = bindDocumentLanguage(instance)
    expect(document.documentElement.lang).toBe('en')

    await instance.changeLanguage('es')
    expect(document.documentElement.lang).toBe('es')

    unbind()
    await instance.changeLanguage('en')
    expect(document.documentElement.lang).toBe('es')
  })
})
