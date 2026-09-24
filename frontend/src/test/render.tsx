import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { I18nextProvider } from 'react-i18next'

import { createI18n, type SupportedLanguage } from '@/i18n'

/**
 * Renders a tree inside an isolated i18n instance.
 *
 * @param ui - The element to render.
 * @param language - Interface language of the instance.
 * @returns The i18n instance, to inspect or change the language.
 */
export async function renderWithI18n(ui: ReactElement, language: SupportedLanguage = 'en') {
  const i18n = await createI18n(language)
  render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>)
  return i18n
}
