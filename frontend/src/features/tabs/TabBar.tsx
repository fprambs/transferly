import { useNavigate } from '@tanstack/react-router'
import { X } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { useOpenConnections } from '../sites/use-open-connections'
import { useSitesData } from '../sites/use-sites-data'
import { useWorkspace } from '../workspace/use-workspace'

import { useTabs, type ConnectionTab } from './tabs-store'
import { useGoToTab } from './use-go-to-tab'

/** Whether a key press happens while typing, where the shortcuts belong to the field. */
function typing(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
}

/**
 * The tabs of the open connections of the workspace, above the log (4.12). Each shows its site with the green dot of an open connection,
 * activates on click, and closes with its button or a middle click; closing does not cut the connection, which the pool closes once idle.
 * Ctrl+Tab and Ctrl+Shift+Tab move between tabs and Ctrl+W closes the active one where the browser allows it, and Alt+Page Down, Alt+Page Up
 * and Alt+W everywhere.
 */
export function TabBar() {
  const { t } = useTranslation('explorer')
  const workspace = useWorkspace()
  const navigate = useNavigate()
  const goTo = useGoToTab()
  const allTabs = useTabs((state) => state.tabs)
  const activeId = useTabs((state) => state.activeId)
  const closeTab = useTabs((state) => state.close)
  const { sites, quick } = useSitesData(workspace?.id)
  const connected = useOpenConnections(workspace?.id)
  const tabs = allTabs.filter((tab) => tab.workspaceId === workspace?.id)

  const close = (tab: ConnectionTab): void => {
    const next = closeTab(tab.id)
    if (tab.id !== activeId) {
      return
    }
    if (next === undefined) {
      void navigate({ to: '/' })
    } else {
      goTo(next)
    }
  }

  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => {
      if (typing(event.target) || tabs.length === 0) {
        return
      }
      const index = tabs.findIndex((tab) => tab.id === activeId)
      const step = (event.ctrlKey && event.key === 'Tab') || (event.altKey && (event.key === 'PageDown' || event.key === 'PageUp'))
      const shut = (event.ctrlKey || event.altKey) && event.key.toLowerCase() === 'w'
      if (step) {
        event.preventDefault()
        const back = event.shiftKey || event.key === 'PageUp'
        goTo(tabs[(index + (back ? tabs.length - 1 : 1) + tabs.length) % tabs.length])
      } else if (shut && index >= 0) {
        event.preventDefault()
        close(tabs[index])
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  if (tabs.length === 0) {
    return null
  }
  return (
    <nav aria-label={t(($) => $.tabs.label)} className="flex h-9 flex-none items-end gap-0.5 overflow-x-auto border-b bg-card px-2">
      {tabs.map((tab) => {
        const name = [...sites, ...quick].find((connection) => connection.id === tab.siteId)?.name ?? tab.siteId
        const active = tab.id === activeId
        return (
          <div
            key={tab.id}
            className={cn(
              'group flex h-8 max-w-56 min-w-0 shrink-0 items-center gap-1 rounded-t-md border border-b-0 ps-2.5 pe-1 text-xs transition-colors',
              active ? 'border-border bg-background font-medium text-foreground' : 'border-transparent text-muted-foreground hover:bg-muted/60 hover:text-foreground',
            )}
          >
            <button
              type="button"
              aria-current={active ? 'page' : undefined}
              onClick={() => goTo(tab)}
              onAuxClick={(event) => event.button === 1 && close(tab)}
              className="flex min-w-0 items-center gap-2 outline-none focus-visible:underline"
            >
              <span className={cn('size-2 shrink-0 rounded-full', connected.has(tab.siteId) ? 'bg-ok ring-3 ring-ok/20' : 'bg-muted-foreground/40')}>
                {connected.has(tab.siteId) && <span className="sr-only">{t(($) => $.tabs.connected)} </span>}
              </span>
              <span className="truncate">{name}</span>
            </button>
            <button
              type="button"
              aria-label={t(($) => $.tabs.close, { name })}
              onClick={() => close(tab)}
              className="flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-60 hover:bg-muted hover:text-foreground hover:opacity-100 focus-visible:opacity-100"
            >
              <X aria-hidden="true" className="size-3.5" />
            </button>
          </div>
        )
      })}
    </nav>
  )
}
