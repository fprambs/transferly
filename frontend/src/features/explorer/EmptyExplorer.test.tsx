import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithI18n } from '@/test/render'

import { EmptyExplorer } from './EmptyExplorer'

/**
 * Covers {@link EmptyExplorer}.
 *
 * Scenarios:
 * - it invites the user to pick a site, in English and in Spanish
 */
describe('EmptyExplorer', () => {
  it('invites the user to pick a site', async () => {
    await renderWithI18n(<EmptyExplorer />)

    expect(screen.getByRole('heading', { name: 'Pick a site to start' })).toBeInTheDocument()
  })

  it('speaks Spanish', async () => {
    await renderWithI18n(<EmptyExplorer />, 'es')

    expect(screen.getByRole('heading', { name: 'Elige un sitio para empezar' })).toBeInTheDocument()
  })
})
