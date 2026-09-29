import { getRouteApi } from '@tanstack/react-router'
import { ArrowLeftRight, Columns2, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Group, layoutStorage, Panel, ResizeHandle, useCollapsible, useDefaultLayout } from '@/components/ui/resizable'
import { NativeSelect } from '@/components/ui/native-select'
import { useUiStore } from '@/state/ui-store'

import { ConnectionLog } from '../connlog/ConnectionLog'
import { LocalExplorer } from '../local/LocalExplorer'
import { localSiteId, localSupported } from '../local/local-fs'
import { useLocalRoot } from '../local/local-root-store'
import { useSitesData } from '../sites/use-sites-data'
import { CompareDialog } from '../sync/CompareDialog'
import type { SyncSide } from '../sync/sides'
import { useTabs } from '../tabs/tabs-store'
import { useConnectionState } from '../toolbar/connection-store'
import { useDisconnect } from '../toolbar/use-disconnect'
import { useWorkspace } from '../workspace/use-workspace'

import { Explorer } from './Explorer'

/** Height of the log panel while collapsed: its header, which keeps the button that opens it again. */
const logHeaderPx = 33

/** Access to the address of the site route: its site, its folder and the second pane. */
const siteRoute = getRouteApi('/shell/sites/$siteId')

/** Explorer of the site in the address, with a second pane beside it when the address asks for one. */
export function SiteExplorer() {
  const { t } = useTranslation('explorer')
  const { siteId } = siteRoute.useParams()
  const { path, split, splitPath } = siteRoute.useSearch()
  const navigate = siteRoute.useNavigate()
  const workspace = useWorkspace()
  const { sites, quick } = useSitesData(workspace?.id)
  const localRoot = useLocalRoot((state) => state.root)
  const [comparing, setComparing] = useState(false)
  const focus = useConnectionState((state) => state.focus)
  const disconnect = useDisconnect()
  const logOpen = useUiStore((state) => state.logOpen)
  const setLogOpen = useUiStore((state) => state.setLogOpen)
  const log = useCollapsible(logOpen, setLogOpen)
  const stack = useDefaultLayout({ id: 'sentinel-explorer', storage: layoutStorage })
  const panes = useDefaultLayout({ id: 'sentinel-split', panelIds: split === undefined ? ['left'] : ['left', 'right'], storage: layoutStorage })
  const workspaceId = workspace?.id

  const follow = useTabs((state) => state.follow)
  const selectInTab = useTabs((state) => state.select)
  const active = useTabs((state) => state.tabs.find((candidate) => candidate.id === state.activeId))
  const tab = active?.siteId === siteId && active.workspaceId === workspaceId ? active : undefined

  useEffect(() => {
    if (workspaceId !== undefined) {
      focus({ workspaceId, siteId })
      follow({ workspaceId, siteId, path, split, splitPath })
    }
  }, [focus, follow, workspaceId, siteId, path, split, splitPath])

  if (workspace === undefined) {
    return null
  }
  const connections = [...sites, ...quick]
  const otherName = (id: string): string => connections.find((connection) => connection.id === id)?.name ?? t(($) => $.local.option)
  const otherSide: SyncSide | null =
    split === undefined ? null : split === localSiteId ? (localRoot === null ? null : { kind: 'local', root: localRoot, path: splitPath ?? '/' }) : { kind: 'remote', workspaceId: workspace.id, siteId: split, path: splitPath ?? '/' }
  const rightLabel = split === undefined ? '' : otherName(split)
  const paneActions = (
    <>
      {otherSide !== null && (
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.split.compare)} onClick={() => setComparing(true)}>
          <ArrowLeftRight />
        </Button>
      )}
      <NativeSelect
        aria-label={t(($) => $.split.site)}
        containerClassName="w-40"
        className="h-7 text-xs"
        value={split}
        onChange={(event) => void navigate({ search: (previous) => ({ ...previous, split: event.target.value, splitPath: undefined }) })}
      >
        {connections.map((connection) => (
          <option key={connection.id} value={connection.id}>
            {connection.name}
          </option>
        ))}
        {localSupported() || split === localSiteId ? <option value={localSiteId}>{t(($) => $.local.option)}</option> : null}
      </NativeSelect>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t(($) => $.split.close)}
        onClick={() => void navigate({ search: (previous) => ({ ...previous, split: undefined, splitPath: undefined }) })}
      >
        <X />
      </Button>
    </>
  )
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <Group orientation="vertical" id="sentinel-explorer" className="min-h-0 flex-1" {...stack}>
        <Panel id="log" collapsible collapsedSize={logHeaderPx} defaultSize={150} minSize={64} maxSize="60%" groupResizeBehavior="preserve-pixel-size" className="flex h-full flex-col" {...log}>
          <ConnectionLog workspaceId={workspace.id} siteId={siteId} />
        </Panel>
        <ResizeHandle aria-label={t(($) => $.panels.log)} />
        <Panel id="files" minSize={160} className="flex h-full min-h-0 flex-col">
          <Group id="sentinel-split" className="min-h-0 flex-1" {...panes}>
            <Panel id="left" minSize="20%" className="flex h-full min-h-0 min-w-0 flex-col">
              <Explorer
                key={`${workspace.id}/${siteId}`}
                pane="main"
                initialSelection={tab?.selection}
                onSelectionChange={(selection) => tab !== undefined && selectInTab(tab.id, selection)}
                workspaceId={workspace.id}
                siteId={siteId}
                path={path}
                onDisconnect={() => void disconnect({ workspaceId: workspace.id, siteId })}
                onNavigate={(next) => void navigate({ search: (previous) => ({ ...previous, path: next }) })}
                actions={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t(($) => $.split.open)}
                    aria-pressed={split !== undefined}
                    onClick={() => void navigate({ search: (previous) => ({ ...previous, split: split === undefined ? siteId : undefined, splitPath: undefined }) })}
                  >
                    <Columns2 />
                  </Button>
                }
              />
            </Panel>
            {split === undefined ? null : <ResizeHandle aria-label={t(($) => $.panels.split)} />}
            {split === undefined ? null : (
              <Panel id="right" minSize="20%" className="flex h-full min-h-0 min-w-0 flex-col">
                <div role="group" aria-label={t(($) => $.split.pane)} className="flex min-h-0 min-w-0 flex-1 flex-col">
                  {split === localSiteId ? (
                    <LocalExplorer workspaceId={workspace.id} path={splitPath ?? '/'} onNavigate={(next) => void navigate({ search: (previous) => ({ ...previous, splitPath: next }) })} actions={paneActions} />
                  ) : (
                    <Explorer
                      key={split}
                      pane="second"
                      workspaceId={workspace.id}
                      siteId={split}
                      path={splitPath ?? '/'}
                      onDisconnect={() => void disconnect({ workspaceId: workspace.id, siteId: split })}
                      onNavigate={(next) => void navigate({ search: (previous) => ({ ...previous, splitPath: next }) })}
                      actions={paneActions}
                    />
                  )}
                </div>
              </Panel>
            )}
          </Group>
        </Panel>
      </Group>
      {comparing && otherSide !== null && (
        <CompareDialog
          left={{ kind: 'remote', workspaceId: workspace.id, siteId, path }}
          right={otherSide}
          leftLabel={otherName(siteId)}
          rightLabel={rightLabel}
          onClose={() => setComparing(false)}
        />
      )}
    </div>
  )
}
