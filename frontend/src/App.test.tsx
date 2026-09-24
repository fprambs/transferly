import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import App from './App'
import { createI18n, type SupportedLanguage } from './i18n'

/** Renders the application with an isolated i18n instance in the given language. */
async function renderApp(language: SupportedLanguage) {
  const i18n = await createI18n(language)
  render(
    <I18nextProvider i18n={i18n}>
      <App />
    </I18nextProvider>,
  )
  return i18n
}

/**
 * Covers {@link App}.
 *
 * Scenarios:
 * - shows the product name and tagline in the active language
 * - offers every supported language, each named in its own language
 * - switching the language re-renders the interface in that language
 */
describe('App', () => {
  it('shows the product name and tagline in English', async () => {
    await renderApp('en')

    expect(screen.getByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(screen.getByText('Secure file transfer over FTP, FTPS and SFTP')).toBeInTheDocument()
  })

  it('shows the tagline in Spanish', async () => {
    await renderApp('es')

    expect(screen.getByText('Transferencia segura de archivos por FTP, FTPS y SFTP')).toBeInTheDocument()
  })

  it('offers every supported language named in its own language', async () => {
    await renderApp('en')

    const options = screen.getAllByRole('option').map((option) => option.textContent)
    expect(options).toEqual(['English', 'Español'])
  })

  it('switches the interface language from the selector', async () => {
    await renderApp('en')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Language' }), 'es')

    expect(await screen.findByRole('combobox', { name: 'Idioma' })).toHaveValue('es')
    expect(screen.getByText('Transferencia segura de archivos por FTP, FTPS y SFTP')).toBeInTheDocument()
  })
})
