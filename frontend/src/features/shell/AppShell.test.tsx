import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { initReactI18next } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import { bindDocumentLanguage, defaultNS, resources } from '@/i18n'
import { adminSession, mockSession, mockUpdateMe, mockWorkspaceData } from '@/test/handlers'
import { useUiStore } from '@/state/ui-store'
import { renderApp } from '@/test/render-app'


/** Renders the application signed in and waits for the shell. */
async function renderShell() {
  await renderApp()
  await screen.findByRole('main', { name: 'Explorer' })
}

/**
 * Covers {@link AppShell} and the panels it composes.
 *
 * Scenarios:
 * - exposes the landmark regions: toolbar, sites navigation, explorer and transfers
 * - the transfer queue collapses and expands, announcing its state
 * - a right-to-left language sets the document direction
 */
describe('AppShell', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockUpdateMe()
    mockWorkspaceData()
    useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true })
  })

  it('exposes the landmark regions', async () => {
    await renderShell()

    expect(screen.getByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(screen.getByRole('banner', { name: 'Main toolbar' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Sites' })).toBeInTheDocument()
    expect(screen.getByRole('main', { name: 'Explorer' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Pick a site to start' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Transfers' })).toBeInTheDocument()
  })

  it('collapses and expands the transfer queue', async () => {
    await renderShell()
    expect(screen.getByText('Nothing is transferring.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Hide transfers' }))
    expect(screen.queryByText('Nothing is transferring.')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show transfers' })).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(screen.getByRole('button', { name: 'Show transfers' }))
    expect(screen.getByText('Nothing is transferring.')).toBeInTheDocument()
  })

  it('sets a right-to-left document direction for a right-to-left language', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, locale: 'ar' } })
    const i18n = createInstance()
    await i18n.use(initReactI18next).init({
      lng: 'ar',
      fallbackLng: 'en',
      defaultNS,
      resources: { ar: resources.en, en: resources.en },
      interpolation: { escapeValue: false },
    })
    const unbind = bindDocumentLanguage(i18n)

    await renderApp({ i18n })
    await screen.findByRole('main', { name: 'Explorer' })

    expect(document.documentElement.dir).toBe('rtl')
    unbind()
  })
})
