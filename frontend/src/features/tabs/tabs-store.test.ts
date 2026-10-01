import { beforeEach, describe, expect, it } from 'vitest'

import { useTabs } from './tabs-store'

/** The sites of the tabs, in order, with the active one marked by an asterisk. */
function summary(): string {
  const { tabs, activeId } = useTabs.getState()
  return tabs.map((tab) => `${tab.siteId}${tab.id === activeId ? '*' : ''}`).join(' ')
}

/** The tab of a site. */
function tabOf(siteId: string) {
  return useTabs.getState().tabs.find((tab) => tab.siteId === siteId)
}

/**
 * Covers the connection tabs store (4.12).
 *
 * Scenarios:
 * - following the address opens a tab for the first site, updates the active tab while it shows the same site, makes it show another site
 *   (forgetting its selection), and activates the tab that already shows a site instead of repeating it
 * - with no active tab, or one of another workspace, following activates the tab of the site or opens one
 * - opening a tab for a site adds it at the end, or activates the one that shows it
 * - closing the active tab activates its neighbor, the last one leaves none active, and closing another keeps the active one
 * - the selection of a tab is remembered, and leaving the sites makes no tab active
 * - a tab opens at the folder asked for, and restarting a site sends the tabs that show it, in either pane of its workspace, back to a
 *   folder and forgets their selection
 * - stored tabs come back, dropping malformed ones and an active tab that no longer exists
 */
describe('tabs store', () => {
  beforeEach(() => {
    localStorage.clear()
    useTabs.setState({ tabs: [], activeId: null })
  })

  it('follows the address of the site view', () => {
    const { follow } = useTabs.getState()
    follow({ workspaceId: 'w1', siteId: 'a', path: '/' })
    expect(summary()).toBe('a*')
    follow({ workspaceId: 'w1', siteId: 'a', path: '/docs', split: 'b', splitPath: '/x' })
    expect(tabOf('a')).toMatchObject({ path: '/docs', split: 'b', splitPath: '/x' })

    useTabs.getState().select(tabOf('a')?.id as string, '/docs/readme')
    follow({ workspaceId: 'w1', siteId: 'a', path: '/' })
    expect(tabOf('a')?.selection).toBe('/docs/readme')
    follow({ workspaceId: 'w1', siteId: 'b', path: '/' })
    expect(summary()).toBe('b*')
    expect(tabOf('b')?.selection).toBeUndefined()

    useTabs.getState().openTab('w1', 'c')
    expect(summary()).toBe('b c*')
    useTabs.getState().select(tabOf('c')?.id as string, '/c.txt')
    expect(tabOf('b')?.selection).toBeUndefined()
    follow({ workspaceId: 'w1', siteId: 'b', path: '/deep' })
    expect(summary()).toBe('b* c')
    expect(tabOf('b')?.path).toBe('/deep')
  })

  it('opens or activates a tab when none of the workspace is active', () => {
    const { follow, deactivate } = useTabs.getState()
    follow({ workspaceId: 'w1', siteId: 'a', path: '/' })
    deactivate()
    follow({ workspaceId: 'w1', siteId: 'a', path: '/again' })
    expect(summary()).toBe('a*')
    follow({ workspaceId: 'w2', siteId: 'z', path: '/' })
    expect(summary()).toBe('a z*')
  })

  it('opens, activates and closes tabs', () => {
    const { openTab, close, activate } = useTabs.getState()
    const a = openTab('w1', 'a')
    openTab('w1', 'b')
    const c = openTab('w1', 'c')
    expect(openTab('w1', 'a')).toBe(a)
    expect(summary()).toBe('a* b c')

    activate(tabOf('b')?.id as string)
    expect(close(c.id)?.siteId).toBe('b')
    expect(summary()).toBe('a b*')
    expect(close(tabOf('b')?.id as string)?.siteId).toBe('a')
    expect(summary()).toBe('a*')
    expect(close(a.id)).toBeUndefined()
    expect(useTabs.getState().activeId).toBeNull()
  })

  it('opens tabs at a folder and restarts the tabs of a site', () => {
    const { openTab, restart } = useTabs.getState()
    expect(openTab('w1', 'a', '/srv').path).toBe('/srv')
    useTabs.setState({
      tabs: [
        { id: 't1', workspaceId: 'w1', siteId: 'a', path: '/deep', selection: '/deep/x' },
        { id: 't2', workspaceId: 'w1', siteId: 'b', path: '/', split: 'a', splitPath: '/deep' },
        { id: 't3', workspaceId: 'w2', siteId: 'a', path: '/deep' },
      ],
    })
    restart('w1', 'a', '/srv')
    const [first, second, third] = useTabs.getState().tabs
    expect(first).toMatchObject({ path: '/srv', selection: undefined })
    expect(second).toMatchObject({ path: '/', splitPath: '/srv' })
    expect(third.path).toBe('/deep')
  })

  it('brings back stored tabs and drops what is malformed', async () => {
    localStorage.setItem(
      'transferly-tabs',
      JSON.stringify({
        state: { tabs: [{ id: 't1', workspaceId: 'w1', siteId: 'a', path: '/' }, { id: 't2', siteId: 'b' }, 'nonsense'], activeId: 't1' },
        version: 0,
      }),
    )
    await useTabs.persist.rehydrate()
    expect(summary()).toBe('a*')

    localStorage.setItem('transferly-tabs', JSON.stringify({ state: { tabs: 'nope', activeId: 'gone' }, version: 0 }))
    await useTabs.persist.rehydrate()
    expect(useTabs.getState()).toMatchObject({ tabs: [], activeId: null })

    localStorage.removeItem('transferly-tabs')
    await useTabs.persist.rehydrate()
    expect(useTabs.getState().tabs).toEqual([])
  })
})
