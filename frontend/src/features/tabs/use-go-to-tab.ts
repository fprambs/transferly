import { useNavigate } from '@tanstack/react-router'

import { useTabs, type ConnectionTab } from './tabs-store'

/** Returns what shows a tab: its site at its folder, with its second pane. */
export function useGoToTab(): (tab: ConnectionTab) => void {
  const navigate = useNavigate()
  const activate = useTabs((state) => state.activate)
  return (tab) => {
    activate(tab.id)
    void navigate({ to: '/sites/$siteId', params: { siteId: tab.siteId }, search: { path: tab.path, split: tab.split, splitPath: tab.splitPath } })
  }
}
