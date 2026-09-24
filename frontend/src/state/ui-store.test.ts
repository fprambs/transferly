import { beforeEach, describe, expect, it } from 'vitest'

import { useUiStore } from './ui-store'

/**
 * Covers {@link useUiStore}.
 *
 * Scenarios:
 * - starts with the system theme and an open queue
 * - changes the theme and toggles the queue
 * - persists only the theme and the queue state
 * - restores valid persisted values
 * - ignores persisted values of the wrong type, and works with nothing stored
 */
describe('useUiStore', () => {
  beforeEach(() => {
    localStorage.clear()
    useUiStore.setState({ theme: 'system', queueOpen: true })
  })

  it('starts with the system theme and an open queue', () => {
    const { theme, queueOpen } = useUiStore.getState()

    expect({ theme, queueOpen }).toEqual({ theme: 'system', queueOpen: true })
  })

  it('changes the theme and toggles the queue', () => {
    useUiStore.getState().setTheme('dark')
    useUiStore.getState().toggleQueue()

    expect(useUiStore.getState()).toMatchObject({ theme: 'dark', queueOpen: false })
  })

  it('persists only the theme and the queue state', () => {
    useUiStore.getState().setTheme('light')

    const stored = JSON.parse(localStorage.getItem('sentinel-ui') ?? '{}') as { state: object }
    expect(Object.keys(stored.state).sort()).toEqual(['queueOpen', 'theme'])
  })

  it('restores valid persisted values', async () => {
    localStorage.setItem('sentinel-ui', JSON.stringify({ state: { theme: 'dark', queueOpen: false }, version: 0 }))

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ theme: 'dark', queueOpen: false })
  })

  it('ignores persisted values of the wrong type', async () => {
    localStorage.setItem('sentinel-ui', JSON.stringify({ state: { theme: 'neon', queueOpen: 'yes' }, version: 0 }))

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ theme: 'system', queueOpen: true })
  })

  it('keeps the defaults when nothing is stored', async () => {
    localStorage.clear()

    await useUiStore.persist.rehydrate()

    expect(useUiStore.getState()).toMatchObject({ theme: 'system', queueOpen: true })
  })
})
