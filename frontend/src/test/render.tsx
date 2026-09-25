import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { I18nextProvider } from 'react-i18next'

import { createI18n, type SupportedLanguage } from '@/i18n'
import { HttpTransport, TransportProvider, type Transport } from '@/lib/transport'

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

/**
 * Renders a tree with i18n, a transport talking to the mock server and a fresh query cache.
 *
 * @param ui - The element to render.
 * @param language - Interface language of the instance.
 * @param transport - The transport to use; tests that need the event stream pass one built with a fake WebSocket.
 * @returns The i18n instance.
 */
export async function renderWithProviders(ui: ReactElement, language: SupportedLanguage = 'en', transport: Transport = new HttpTransport()) {
  const i18n = await createI18n(language)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <I18nextProvider i18n={i18n}>
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
      </TransportProvider>
    </I18nextProvider>,
  )
  return i18n
}
