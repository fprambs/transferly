/** A language of the interface. */
export interface Language {
  /** Its BCP 47 tag, the name of its folder under `locales/`. */
  code: string
  /** Its name written in itself, as the language menu shows it to someone who reads it. */
  name: string
}

/**
 * The languages of the interface, English first as the source. Adding a language is adding its folder under `locales/`, a copy of `en`
 * translated, and a line here; the tests of the translations tell what is missing (docs/guides/i18n.md).
 */
export const languages = [
  { code: 'en', name: 'English' },
  { code: 'es', name: 'Español' },
] as const satisfies readonly Language[]
