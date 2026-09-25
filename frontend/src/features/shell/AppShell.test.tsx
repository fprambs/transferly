import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import { bindDocumentLanguage, defaultNS, resources } from '@/i18n'
import { mockWorkspaceData } from '@/test/handlers'
import { HttpTransport, TransportProvider } from '@/lib/transport'
import { useUiStore } from '@/state/ui-store'
import { renderWithProviders } from '@/test/render'

import { AppShell } from './AppShell'

/**
 * Covers {@link AppShell} and the panels it composes.
 *
 * Scenarios:
 * - exposes the landmark regions: toolbar, sites navigation, file panes and transfers
 * - the theme list offers every theme grouped by scheme and changes the stored choice
 * - following the system disables the theme list
 * - the transfer queue collapses and expands, announcing its state
 * - the interface switches to Spanish from the language selector
 * - a right-to-left language sets the document direction
 */
describe('AppShell', () => {
  beforeEach(() => {
    mockWorkspaceData()
    useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true })
  })

  it('exposes the landmark regions', async () => {
    await renderWithProviders(<AppShell />)

    expect(screen.getByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(screen.getByRole('banner', { name: 'Main toolbar' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Sites' })).toBeInTheDocument()
    expect(screen.getByRole('main', { name: 'File panes' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Local files' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Remote files' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Transfers' })).toBeInTheDocument()
  })

  it('lists the themes by scheme and changes the chosen one', async () => {
    await renderWithProviders(<AppShell />)
    const select = screen.getByRole('combobox', { name: 'Theme' })

    expect(select).toHaveValue('sentinel-dark')
    expect(within(select).getAllByRole('option')).toHaveLength(12)
    expect(screen.getByRole('group', { name: 'Dark' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Light' })).toBeInTheDocument()

    await userEvent.selectOptions(select, 'monokai')

    expect(useUiStore.getState().themeId).toBe('monokai')
    expect(select).toHaveValue('monokai')
  })

  it('follows the system instead of the chosen theme when asked to', async () => {
    await renderWithProviders(<AppShell />)
    const select = screen.getByRole('combobox', { name: 'Theme' })

    await userEvent.click(screen.getByRole('checkbox', { name: 'Follow the system' }))

    expect(useUiStore.getState().followSystem).toBe(true)
    expect(select).toBeDisabled()
  })

  it('collapses and expands the transfer queue', async () => {
    await renderWithProviders(<AppShell />)
    expect(screen.getByText('No transfers')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Hide transfers' }))
    expect(screen.queryByText('No transfers')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show transfers' })).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(screen.getByRole('button', { name: 'Show transfers' }))
    expect(screen.getByText('No transfers')).toBeInTheDocument()
  })

  it('switches the interface to Spanish from the selector', async () => {
    await renderWithProviders(<AppShell />)

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Language' }), 'es')

    expect(await screen.findByRole('region', { name: 'Transferencias' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Sitios' })).toBeInTheDocument()
  })

  it('sets a right-to-left document direction for a right-to-left language', async () => {
    const i18n = createInstance()
    await i18n.use(initReactI18next).init({
      lng: 'ar',
      fallbackLng: 'en',
      defaultNS,
      resources: { ar: resources.en, en: resources.en },
      interpolation: { escapeValue: false },
    })
    const unbind = bindDocumentLanguage(i18n)

    render(
      <I18nextProvider i18n={i18n}>
        <TransportProvider transport={new HttpTransport()}>
          <QueryClientProvider client={new QueryClient()}>
            <AppShell />
          </QueryClientProvider>
        </TransportProvider>
      </I18nextProvider>,
    )

    expect(document.documentElement.dir).toBe('rtl')
    expect(screen.getByRole('main', { name: 'File panes' })).toBeInTheDocument()
    unbind()
  })
})
