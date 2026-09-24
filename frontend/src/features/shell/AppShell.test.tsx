import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import { bindDocumentLanguage, defaultNS, resources } from '@/i18n'
import { useUiStore } from '@/state/ui-store'
import { renderWithI18n } from '@/test/render'

import { AppShell } from './AppShell'

/**
 * Covers {@link AppShell} and the panels it composes.
 *
 * Scenarios:
 * - exposes the landmark regions: toolbar, sites navigation, file panes and transfers
 * - the theme buttons reflect and change the stored preference
 * - the transfer queue collapses and expands, announcing its state
 * - the interface switches to Spanish from the language selector
 * - a right-to-left language sets the document direction
 */
describe('AppShell', () => {
  beforeEach(() => {
    useUiStore.setState({ theme: 'system', queueOpen: true })
  })

  it('exposes the landmark regions', async () => {
    await renderWithI18n(<AppShell />)

    expect(screen.getByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(screen.getByRole('banner', { name: 'Main toolbar' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Sites' })).toBeInTheDocument()
    expect(screen.getByRole('main', { name: 'File panes' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Local files' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Remote files' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Transfers' })).toBeInTheDocument()
  })

  it('reflects and changes the theme preference', async () => {
    await renderWithI18n(<AppShell />)
    const group = screen.getByRole('group', { name: 'Theme' })

    expect(within(group).getByRole('button', { name: 'System' })).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(within(group).getByRole('button', { name: 'Dark' }))

    expect(useUiStore.getState().theme).toBe('dark')
    expect(within(group).getByRole('button', { name: 'Dark' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(group).getByRole('button', { name: 'System' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('collapses and expands the transfer queue', async () => {
    await renderWithI18n(<AppShell />)
    expect(screen.getByText('No transfers')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Hide transfers' }))
    expect(screen.queryByText('No transfers')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show transfers' })).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(screen.getByRole('button', { name: 'Show transfers' }))
    expect(screen.getByText('No transfers')).toBeInTheDocument()
  })

  it('switches the interface to Spanish from the selector', async () => {
    await renderWithI18n(<AppShell />)

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
        <AppShell />
      </I18nextProvider>,
    )

    expect(document.documentElement.dir).toBe('rtl')
    expect(screen.getByRole('main', { name: 'File panes' })).toBeInTheDocument()
    unbind()
  })
})
