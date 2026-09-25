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
 * - chooses a theme, turns following the system on and toggles the queue
 * - persists only the theme, the follow-system flag and the queue state
 * - restores valid persisted values
 * - ignores persisted values of the wrong type, unknown themes, and works with nothing stored
 * - upgrades documents written by earlier versions, which stored a color scheme instead of a theme
 */
describe('useUiStore', () => {
  beforeEach(() => {
    localStorage.clear()
    useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true })
  })

  it('starts with the default theme and an open queue', () => {
    const { themeId, followSystem, queueOpen } = useUiStore.getState()

    expect({ themeId, followSystem, queueOpen }).toEqual({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true })
  })

  it('chooses a theme, follows the system and toggles the queue', () => {
    useUiStore.getState().setTheme('monokai')
    useUiStore.getState().setFollowSystem(true)
    useUiStore.getState().toggleQueue()

    expect(useUiStore.getState()).toMatchObject({ themeId: 'monokai', followSystem: true, queueOpen: false })
  })

  it('persists only the theme, the follow-system flag and the queue state', () => {
    useUiStore.getState().setTheme('nord')

    const stored = JSON.parse(localStorage.getItem('sentinel-ui') ?? '{}') as { state: object }
    expect(Object.keys(stored.state).sort()).toEqual(['followSystem', 'queueOpen', 'themeId'])
  })

  it('restores valid persisted values', async () => {
    store({ themeId: 'dracula', followSystem: true, queueOpen: false })

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ themeId: 'dracula', followSystem: true, queueOpen: false })
  })

  it('ignores values of the wrong type and unknown themes', async () => {
    store({ themeId: 'no-such-theme', followSystem: 'yes', queueOpen: 'no' })

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ themeId: 'sentinel-dark', followSystem: false, queueOpen: true })
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
