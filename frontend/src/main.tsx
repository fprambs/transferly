import './index.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { I18nextProvider } from 'react-i18next'

import App from './App'
import { bindDocumentLanguage, createI18n } from './i18n'
import { HttpTransport, TransportProvider } from './lib/transport'

const container = document.getElementById('root')
if (container === null) {
  throw new Error('Root element #root not found')
}

const i18n = await createI18n()
bindDocumentLanguage(i18n)

createRoot(container).render(
  <StrictMode>
    <I18nextProvider i18n={i18n}>
      <TransportProvider transport={new HttpTransport()}>
        <App />
      </TransportProvider>
    </I18nextProvider>
  </StrictMode>,
)
