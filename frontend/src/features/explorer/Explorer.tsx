import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUp, RefreshCw, ShieldQuestion } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { resolveTimeZone } from '@/lib/datetime'
import { useApi } from '@/lib/api/use-api'

import { useSession } from '../auth/use-session'
import { unknownHostKeyOf, type UnknownHostKey } from '../quick/errors'
import { useSitesData } from '../sites/use-sites-data'

import { Breadcrumbs } from './Breadcrumbs'
import { EntryTable } from './EntryTable'
import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'
import { parentPath, sortEntries } from './entries'
import { filesKey } from './files-key'

/** Props of {@link Explorer}. */
export interface ExplorerProps {
  /** Workspace of the connection. */
  workspaceId: string
  /** Saved site or quick connection to browse. */
  siteId: string
  /** Absolute path of the folder in view. */
  path: string
  /** Called with the folder the user moves to. */
  onNavigate: (path: string) => void
}

/** Browser of the files of one connection: path, toolbar and a table of the folder in view. */
export function Explorer({ workspaceId, siteId, path, onNavigate }: ExplorerProps) {
  const { t, i18n } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const session = useSession()
  const { sites, quick } = useSitesData(workspaceId)
  const [trustFailure, setTrustFailure] = useState<ExplorerErrorKey | null>(null)
  const listing = useQuery({ queryKey: filesKey(workspaceId, siteId, path), queryFn: () => api.files.list(workspaceId, siteId, path) })
  const entries = useMemo(() => sortEntries(listing.data ?? []), [listing.data])
  const target = [...sites, ...quick].find((connection) => connection.id === siteId)
  const hostKey = unknownHostKeyOf(listing.error)
  const failure = listing.error === null ? null : explorerErrorKeyOf(listing.error)

  const trust = async (key: UnknownHostKey, connection: { host: string; port: number }): Promise<void> => {
    try {
      await api.hostKeys.trust(workspaceId, { host: connection.host, port: connection.port, ...key })
    } catch (error) {
      setTrustFailure(explorerErrorKeyOf(error))
      return
    }
    setTrustFailure(null)
    await listing.refetch()
  }

  const open = (entry: { type: string; path: string }): void => {
    if (entry.type === 'directory') {
      onNavigate(entry.path)
    }
  }

  return (
    <section aria-label={t(($) => $.list)} className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-11 shrink-0 items-center gap-2 border-b px-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.up)} disabled={path === '/'} onClick={() => onNavigate(parentPath(path))}>
          <ArrowUp />
        </Button>
        <Breadcrumbs path={path} onNavigate={onNavigate} />
        <span className="font-mono text-xs text-muted-foreground">{t(($) => $.rows, { count: entries.length })}</span>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.refresh)}
          onClick={() => void queryClient.invalidateQueries({ queryKey: filesKey(workspaceId, siteId, path) })}
        >
          <RefreshCw />
        </Button>
      </div>
      {listing.isPending && <p className="p-4 text-sm text-muted-foreground">{t(($) => $.loading)}</p>}
      {failure !== null && hostKey !== undefined && target !== undefined && (
        <div role="alert" className="m-4 flex max-w-xl flex-col gap-3 rounded-xl border bg-card p-4 text-sm">
          <p className="flex items-center gap-2 font-medium">
            <ShieldQuestion className="size-5 text-warn" />
            {t(($) => $.trust.title)}
          </p>
          <p className="text-muted-foreground">{t(($) => $.trust.body)}</p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
            <dt className="text-muted-foreground">{t(($) => $.trust.keyType)}</dt>
            <dd className="font-mono">{hostKey.keyType}</dd>
            <dt className="text-muted-foreground">{t(($) => $.trust.fingerprint)}</dt>
            <dd className="font-mono break-all">{hostKey.fingerprint}</dd>
          </dl>
          {trustFailure !== null && <p className="text-destructive">{t(($) => $.errors[trustFailure])}</p>}
          <Button className="self-start" onClick={() => trust(hostKey, target)}>
            {t(($) => $.trust.confirm)}
          </Button>
        </div>
      )}
      {failure !== null && (hostKey === undefined || target === undefined) && (
        <div role="alert" className="m-4 flex max-w-xl flex-col gap-3 rounded-xl border border-destructive/40 bg-destructive/10 p-4 text-sm">
          <p className="text-destructive">{t(($) => $.errors[failure])}</p>
          <Button variant="outline" className="self-start" onClick={() => void listing.refetch()}>
            {t(($) => $.retry)}
          </Button>
        </div>
      )}
      {listing.isSuccess && entries.length === 0 && <p className="p-4 text-sm text-muted-foreground">{t(($) => $.emptyDir)}</p>}
      {listing.isSuccess && entries.length > 0 && (
        <EntryTable
          key={path}
          entries={entries}
          locale={i18n.language}
          timeZone={session?.user.timezone ?? resolveTimeZone()}
          onOpen={open}
          onUp={() => onNavigate(parentPath(path))}
        />
      )}
    </section>
  )
}
