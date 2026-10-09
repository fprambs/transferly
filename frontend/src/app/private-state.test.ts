import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import { useTransfersStore } from '@/features/transfers/transfers-store'
import { useTabs } from '@/features/tabs/tabs-store'
import { useLocalRoot } from '@/features/local/local-root-store'
import { usePrompts } from '@/features/prompts/prompts-store'
import { useConnectionLog } from '@/features/connlog/connection-log-store'
import { useRecentSites } from '@/features/sites/recent-sites'
import { useConnectionState } from '@/features/toolbar/connection-store'
import { useNotices } from '@/features/notices/notices-store'
import { useDialogsStore } from '@/state/dialogs-store'

import { announceSessionChange, clearPrivateState, ensurePrivateOwner, sessionChangeKey } from './private-state'

/** Verifies that cached data, persistent tabs and browser handles cannot survive an identity transition. */
describe('private state', () => {
  it('records identity and invalidates persistence only on an owner change', () => {
    ensurePrivateOwner('A')
    useTabs.getState().openTab('w', 's')
    ensurePrivateOwner('A')
    expect(useTabs.getState().tabs).toHaveLength(1)
    ensurePrivateOwner('B')
    expect(useTabs.getState().tabs).toEqual([])
    announceSessionChange()
    expect(localStorage.getItem(sessionChangeKey)).toBeTruthy()
  })

  it('clears every private store and cache', () => {
    const cache = new QueryClient()
    cache.setQueryData(['private'], 'A')
    useTabs.getState().openTab('w', 's')
    useLocalRoot.getState().setRoot({} as FileSystemDirectoryHandle)
    useTransfersStore.setState({ items: { old: {} as never } })
    usePrompts.setState({ waiting: [{} as never] })
    useConnectionLog.setState({ lines: { old: [] } })
    useRecentSites.getState().remember('w', 's')
    useConnectionState.getState().focus({ workspaceId: 'w', siteId: 's' })
    useNotices.getState().notify('private')
    useDialogsStore.getState().setQuickConnectOpen(true)
    clearPrivateState(cache)
    expect(cache.getQueryData(['private'])).toBeUndefined()
    expect(useTransfersStore.getState().items).toEqual({})
    expect(useTabs.getState().tabs).toEqual([])
    expect(useLocalRoot.getState().root).toBeNull()
    expect(usePrompts.getState().waiting).toEqual([])
    expect(useConnectionLog.getState().lines).toEqual({})
    expect(useRecentSites.getState().recent).toEqual({})
    expect(useConnectionState.getState().focused).toBeNull()
    expect(useNotices.getState().notices).toEqual([])
    expect(useDialogsStore.getState().quickConnectOpen).toBe(false)
  })
})
