import { getRouteApi } from '@tanstack/react-router'
import { ArrowLeftRight, Columns2, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'

import { ConnectionLog } from '../connlog/ConnectionLog'
import { LocalExplorer } from '../local/LocalExplorer'
import { localSiteId, localSupported } from '../local/local-fs'
import { useLocalRoot } from '../local/local-root-store'
import { useSitesData } from '../sites/use-sites-data'
import { CompareDialog } from '../sync/CompareDialog'
import type { SyncSide } from '../sync/sides'
import { useWorkspace } from '../workspace/use-workspace'

import { Explorer } from './Explorer'

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
      <ConnectionLog workspaceId={workspace.id} siteId={siteId} />
      <div className="flex min-h-0 min-w-0 flex-1">
        <Explorer
          workspaceId={workspace.id}
          siteId={siteId}
          path={path}
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
        {split === undefined ? null : (
          <div role="group" aria-label={t(($) => $.split.pane)} className="flex min-h-0 min-w-0 flex-1 flex-col border-s">
            {split === localSiteId ? (
              <LocalExplorer workspaceId={workspace.id} path={splitPath ?? '/'} onNavigate={(next) => void navigate({ search: (previous) => ({ ...previous, splitPath: next }) })} actions={paneActions} />
            ) : (
              <Explorer
                key={split}
                workspaceId={workspace.id}
                siteId={split}
                path={splitPath ?? '/'}
                onNavigate={(next) => void navigate({ search: (previous) => ({ ...previous, splitPath: next }) })}
                actions={paneActions}
              />
            )}
          </div>
        )}
      </div>
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
