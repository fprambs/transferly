import { createMemoryHistory } from '@tanstack/react-router'
import { render } from '@testing-library/react'
import type { i18n as I18n } from 'i18next'
import { I18nextProvider } from 'react-i18next'

import App from '@/App'
import { createI18n, type SupportedLanguage } from '@/i18n'
import { HttpTransport, TransportProvider } from '@/lib/transport'

/** Options of {@link renderApp}. */
export interface RenderAppOptions {
  /** Location the application starts at. */
  path?: string
  /** Interface language. */
  language?: SupportedLanguage
  /** An i18n instance to use instead of creating one for the language. */
  i18n?: I18n
}

/**
 * Renders the whole application on a memory history, talking to the mock server.
 *
 * @returns The router history, to assert the final location, and the transport in use.
 */
export async function renderApp({ path = '/', language = 'en', i18n: given }: RenderAppOptions = {}) {
  const i18n = given ?? (await createI18n(language))
  const history = createMemoryHistory({ initialEntries: [path] })
  const transport = new HttpTransport()
  render(
    <I18nextProvider i18n={i18n}>
      <TransportProvider transport={transport}>
        <App history={history} />
      </TransportProvider>
    </I18nextProvider>,
  )
  return { history, transport }
}
