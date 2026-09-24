import { screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import App from './App'
import { useUiStore } from './state/ui-store'
import { renderWithI18n } from './test/render'

/**
 * Covers {@link App}.
 *
 * Scenarios:
 * - renders the application layout
 * - applies the stored dark theme to the document
 */
describe('App', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    useUiStore.setState({ theme: 'system', queueOpen: true })
  })

  it('renders the application layout', async () => {
    await renderWithI18n(<App />)

    expect(screen.getByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
  })

  it('applies the stored dark theme to the document', async () => {
    useUiStore.setState({ theme: 'dark' })

    await renderWithI18n(<App />)

    expect(document.documentElement).toHaveClass('dark')
  })
})
