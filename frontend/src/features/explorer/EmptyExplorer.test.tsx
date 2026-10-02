import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import type { QuickConnection, Site } from '@/lib/api'
import { useDialogsStore } from '@/state/dialogs-store'
import { adminSession, mockSession, mockVault, mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { renderApp } from '@/test/render-app'
import { teamWorkspace } from '@/test/teams'

import { recentLimit, useRecentSites } from '../sites/recent-sites'

/** Builds a saved site. */
function site(id: string, name: string, color?: Site['color']): Site {
  return {
    id,
    name,
    protocol: 'sftp',
    host: `${id}.example.com`,
    port: 22,
    username: 'deploy',
    authMethod: 'password',
    listTimezone: 'UTC',
    charset: 'utf-8',
    speedLimitBps: 0,
    secrets: [],
    color,
    remoteDir: id === 's1' ? '/var/www' : undefined,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

/** A quick connection. */
const quick: QuickConnection = { id: 'q1', name: 'ftp.example.com', protocol: 'ftp', host: 'ftp.example.com', port: 21, username: 'anonymous', authMethod: 'password', createdAt: '2026-01-01T00:00:00Z' } as QuickConnection

/** The section of the first steps. */
function steps(): HTMLElement {
  return screen.getByRole('region', { name: 'First steps' })
}

/**
 * Covers {@link EmptyExplorer} and the recent sites it offers.
 *
 * Scenarios:
 * - it invites the user to pick a site, and opens quick connect and the command palette, in English and in Spanish
 * - the sites opened last in the workspace show newest first, saved sites and quick connections alike, and one opens at its folder;
 *   the list keeps the newest few and forgets sites that are gone
 * - the first steps check what is done and offer what is not: set up the vault, add a site, create a team; once all are done they leave
 */
describe('EmptyExplorer', () => {
  beforeEach(() => {
    mockSession(adminSession)
    useRecentSites.setState({ recent: {} })
    useDialogsStore.setState({ quickConnectOpen: false, paletteOpen: false })
  })

  it('invites the user to pick a site and opens quick connect and the palette', async () => {
    mockWorkspaceData()
    const user = userEvent.setup()
    await renderApp({ path: '/' })

    expect(await screen.findByRole('heading', { name: 'Pick a site to start' })).toBeInTheDocument()
    const start = screen.getByRole('main')
    await user.click(within(start).getByRole('button', { name: /^Quick connect/ }))
    expect(useDialogsStore.getState().quickConnectOpen).toBe(true)
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await user.click(within(screen.getByRole('main')).getByRole('button', { name: /^Search or run a command/ }))
    expect(useDialogsStore.getState().paletteOpen).toBe(true)
  })

  it('speaks Spanish', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, locale: 'es' } })
    mockWorkspaceData()
    await renderApp({ path: '/', language: 'es' })

    expect(await screen.findByRole('heading', { name: 'Elige un sitio para empezar' })).toBeInTheDocument()
  })

  it('offers the recent sites, newest first, and opens one at its folder', async () => {
    mockWorkspaceData({ sites: [site('s1', 'Prod', 'green'), site('s2', 'Backup')], quick: [quick] })
    useRecentSites.setState({ recent: { [personalWorkspace.id]: ['q1', 'gone', 's1'], other: ['s2'] } })
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })

    const recent = await screen.findByRole('region', { name: 'Recent' })
    const cards = await within(recent).findAllByRole('button')
    expect(cards.map((card) => card.textContent)).toEqual(['ftp.example.comFTP · ftp.example.com', 'ProdSFTP · s1.example.com'])

    await user.click(cards[1])
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s1'))
    expect(history.location.search).toContain('path=%2Fvar%2Fwww')
    expect(useRecentSites.getState().recent[personalWorkspace.id][0]).toBe('s1')
  })

  it('remembers only the newest sites', () => {
    for (let index = 0; index < recentLimit + 2; index++) {
      useRecentSites.getState().remember('w', `s${index}`)
    }
    useRecentSites.getState().remember('w', 's3')

    expect(useRecentSites.getState().recent.w).toEqual(['s3', 's7', 's6', 's5', 's4', 's2'])
  })

  it('walks through the first steps until they are done', async () => {
    mockWorkspaceData()
    mockVault({ initialized: false, unlocked: false })
    const user = userEvent.setup()
    await renderApp({ path: '/' })

    await waitFor(() => expect(within(steps()).getAllByRole('listitem')).toHaveLength(3))
    await user.click(within(steps()).getByRole('button', { name: 'Set up' }))
    expect(await screen.findByRole('dialog', { name: /vault/i })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await user.click(within(steps()).getByRole('button', { name: 'Add' }))
    expect(await screen.findByRole('dialog', { name: 'New site' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await user.click(within(steps()).getByRole('button', { name: 'Create a team' }))
    const team = await screen.findByRole('dialog', { name: 'New team workspace' })
    await user.click(within(team).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('checks the steps that are done and leaves when all are', async () => {
    mockWorkspaceData({ sites: [site('s1', 'Prod')] })
    await renderApp({ path: '/' })

    await waitFor(() => expect(within(steps()).getAllByRole('button')).toHaveLength(1))
    expect(within(steps()).getByText('Save your first site')).toHaveClass('line-through')
    expect(within(steps()).getByText('Set up the vault that keeps your passwords and keys')).toHaveClass('line-through')
  })

  it('leaves the add button out of the first steps for a viewer', async () => {
    mockWorkspaceData({ workspaces: [{ ...personalWorkspace, role: 'viewer' }, teamWorkspace('owner')] })
    mockVault({ initialized: false, unlocked: false })
    await renderApp({ path: '/' })

    await waitFor(() => expect(within(steps()).getAllByRole('listitem')).toHaveLength(3))
    expect(within(steps()).queryByRole('button', { name: 'Add' })).not.toBeInTheDocument()
  })

  it('leaves the first steps out once all are done', async () => {
    mockWorkspaceData({ workspaces: [personalWorkspace, teamWorkspace('owner')], sites: [site('s1', 'Prod')] })
    await renderApp({ path: '/' })

    await screen.findByRole('heading', { name: 'Pick a site to start' })
    await screen.findByRole('treeitem', { name: /Prod/ })
    expect(screen.queryByRole('region', { name: 'First steps' })).not.toBeInTheDocument()
  })
})
