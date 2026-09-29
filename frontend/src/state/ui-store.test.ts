import { beforeEach, describe, expect, it } from 'vitest'

import { useUiStore } from './ui-store'

/** Stores a document the way the persistence layer does. */
function store(state: object): void {
  localStorage.setItem('sentinel-ui', JSON.stringify({ state, version: 0 }))
}

/**
 * Covers {@link useUiStore}.
 *
 * Scenarios:
 * - starts with the default dark theme, not following the system, and an open queue
 * - chooses a theme, turns following the system on, toggles the queue and chooses a workspace
 * - the queue tab is chosen and opening the active transfers shows a closed queue on its active tab and leaves an open queue on the tab it has, without being kept across sessions
 * - persists only the theme, the follow-system flag, the queue state and the workspace
 * - restores valid persisted values
 * - ignores persisted values of the wrong type, unknown themes, and works with nothing stored
 * - upgrades documents written by earlier versions, which stored a color scheme instead of a theme
 */
describe('useUiStore', () => {
  beforeEach(() => {
    localStorage.clear()
    useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true, workspaceId: null, fileDoubleClick: 'download', preferencesSynced: false, logOpen: true, sitesOpen: true, detailsOpen: true })
  })

  it('starts with the default theme and an open queue', () => {
    const { themeId, followSystem, queueOpen } = useUiStore.getState()

    expect({ themeId, followSystem, queueOpen }).toEqual({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true })
  })

  it('chooses a theme, follows the system, toggles the queue and the log, and chooses what a double click on a file does', () => {
    useUiStore.getState().setTheme('monokai')
    useUiStore.getState().setFollowSystem(true)
    useUiStore.getState().toggleQueue()
    useUiStore.getState().setWorkspace('w1')
    useUiStore.getState().setFileDoubleClick('edit')
    useUiStore.getState().toggleLog()
    useUiStore.getState().setSitesOpen(false)
    useUiStore.getState().setDetailsOpen(false)

    expect(useUiStore.getState()).toMatchObject({ themeId: 'monokai', followSystem: true, queueOpen: false, workspaceId: 'w1', fileDoubleClick: 'edit', logOpen: false, sitesOpen: false, detailsOpen: false })
  })

  it('goes back to the personal workspace', () => {
    useUiStore.getState().setWorkspace('w1')
    useUiStore.getState().clearWorkspace()

    expect(useUiStore.getState().workspaceId).toBeNull()
  })

  it('chooses the queue tab and shows the active transfers', () => {
    useUiStore.setState({ queueOpen: false, queueTab: 'active' })

    useUiStore.getState().setQueueTab('finished')
    expect(useUiStore.getState().queueTab).toBe('finished')

    useUiStore.getState().showActiveTransfers()
    expect(useUiStore.getState()).toMatchObject({ queueOpen: true, queueTab: 'active' })

    useUiStore.getState().setQueueTab('finished')
    useUiStore.getState().showActiveTransfers()
    expect(useUiStore.getState()).toMatchObject({ queueOpen: true, queueTab: 'finished' })
    const stored = JSON.parse(localStorage.getItem('sentinel-ui') ?? '{}') as { state: object }
    expect(Object.keys(stored.state)).not.toContain('queueTab')
  })

  it('persists only the theme, the follow-system flag, the panels shown, the workspace and the double click action', () => {
    useUiStore.getState().setTheme('nord')

    const stored = JSON.parse(localStorage.getItem('sentinel-ui') ?? '{}') as { state: object }
    expect(Object.keys(stored.state).sort()).toEqual(['detailsOpen', 'fileDoubleClick', 'followSystem', 'logOpen', 'preferencesSynced', 'queueOpen', 'sitesOpen', 'themeId', 'workspaceId'])
  })

  it('restores valid persisted values', async () => {
    store({ themeId: 'dracula', followSystem: true, queueOpen: false, workspaceId: 'w2', fileDoubleClick: 'edit', preferencesSynced: true, logOpen: false, sitesOpen: false, detailsOpen: false })

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ themeId: 'dracula', followSystem: true, queueOpen: false, workspaceId: 'w2', fileDoubleClick: 'edit', preferencesSynced: true, logOpen: false, sitesOpen: false, detailsOpen: false })
  })

  it('ignores values of the wrong type and unknown themes', async () => {
    store({ themeId: 'no-such-theme', followSystem: 'yes', queueOpen: 'no', workspaceId: 7, fileDoubleClick: 'open', preferencesSynced: 'yes', logOpen: 'no', sitesOpen: 1, detailsOpen: null })

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true, workspaceId: null, fileDoubleClick: 'download', preferencesSynced: false, logOpen: true, sitesOpen: true, detailsOpen: true })
  })

  it('works with nothing stored', async () => {
    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ themeId: 'sentinel-dark', queueOpen: true })
  })

  it.each([
    ['light', { themeId: 'sentinel-light', followSystem: false }],
    ['dark', { themeId: 'sentinel-dark', followSystem: false }],
    ['system', { themeId: 'sentinel-dark', followSystem: true }],
  ])('upgrades a stored color scheme %s', async (theme, expected) => {
    store({ theme, queueOpen: true })

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject(expected)
  })

  it('ignores an unknown stored color scheme', async () => {
    store({ theme: 'sepia' })

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState().themeId).toBe('sentinel-dark')
  })
})
