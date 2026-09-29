import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Loading } from '@/components/ui/loading'
import type { KnownHost } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { formatInstant, resolveTimeZone } from '@/lib/datetime'

import { useSession } from '../auth/use-session'
import { useWorkspace } from '../workspace/use-workspace'

import { forgetErrorOf, type ForgetError } from './errors'

/** Props of {@link ForgetDialog}. */
interface ForgetDialogProps {
  /** The server to forget. */
  host: KnownHost
  /** Workspace the key belongs to. */
  workspaceId: string
  /** Called when the dialog is dismissed or the server is forgotten. */
  onClose: () => void
}

/** Asks for confirmation before forgetting a trusted server key. */
function ForgetDialog({ host, workspaceId, onClose }: ForgetDialogProps) {
  const { t } = useTranslation('settings')
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<ForgetError | null>(null)

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.servers.forgetTitle)}</DialogTitle>
          <DialogDescription>{t(($) => $.servers.forgetBody, { host: `${host.host}:${host.port}`, fingerprint: host.fingerprint })}</DialogDescription>
        </DialogHeader>
        {failure !== null && (
          <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {t(($) => $.servers.errors[failure])}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.servers.cancel)}
          </Button>
          <Button
            variant="destructive"
            onClick={async () => {
              try {
                await api.hostKeys.forget(workspaceId, { host: host.host, port: host.port, keyType: host.keyType, fingerprint: host.fingerprint })
                await queryClient.invalidateQueries({ queryKey: ['workspaces', workspaceId, 'known-hosts'] })
                onClose()
              } catch (error) {
                setFailure(forgetErrorOf(error))
              }
            }}
          >
            {t(($) => $.servers.forgetConfirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Props of {@link TrustedServersList}. */
interface TrustedServersListProps {
  /** Workspace whose trusted servers are shown. */
  workspaceId: string
}

/** The trusted server keys of a workspace, each with the action to forget it; a server may have several keys (4.13b), each forgotten on its own. */
function TrustedServersList({ workspaceId }: TrustedServersListProps) {
  const { t, i18n } = useTranslation('settings')
  const api = useApi()
  const session = useSession()
  const [forgetting, setForgetting] = useState<KnownHost | null>(null)
  const hosts = useQuery({ queryKey: ['workspaces', workspaceId, 'known-hosts'], queryFn: () => api.hostKeys.list(workspaceId) })
  const timeZone = session?.user.timezone ?? resolveTimeZone()

  if (hosts.isPending) {
    return <Loading label={t(($) => $.servers.loading)} />
  }
  if (hosts.isError) {
    return <p role="alert" className="text-sm text-destructive">{t(($) => $.servers.loadFailed)}</p>
  }
  if (hosts.data.length === 0) {
    return <p className="text-sm text-muted-foreground">{t(($) => $.servers.empty)}</p>
  }
  return (
    <>
      <table className="w-full max-w-4xl text-sm">
        <thead>
          <tr className="border-b text-start text-[11px] tracking-wider text-muted-foreground uppercase">
            <th scope="col" className="py-2 pe-3 text-start font-medium">
              {t(($) => $.servers.host)}
            </th>
            <th scope="col" className="py-2 pe-3 text-start font-medium">
              {t(($) => $.servers.keyType)}
            </th>
            <th scope="col" className="py-2 pe-3 text-start font-medium">
              {t(($) => $.servers.fingerprint)}
            </th>
            <th scope="col" className="py-2 pe-3 text-start font-medium">
              {t(($) => $.servers.firstSeen)}
            </th>
            <th scope="col" className="py-2" />
          </tr>
        </thead>
        <tbody>
          {hosts.data.map((host) => (
            <tr key={`${host.host}:${host.port}:${host.keyType}:${host.fingerprint}`} className="border-b last:border-b-0">
              <td className="py-2 pe-3 font-mono">
                {host.host}:{host.port}
              </td>
              <td className="py-2 pe-3 font-mono">{host.keyType}</td>
              <td className="py-2 pe-3 font-mono text-xs break-all">{host.fingerprint}</td>
              <td className="py-2 pe-3 text-muted-foreground">
                {formatInstant(host.firstSeen, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })}
              </td>
              <td className="py-2 text-end">
                <Button variant="ghost" size="sm" aria-label={t(($) => $.servers.forgetLabel, { host: `${host.host}:${host.port}`, fingerprint: host.fingerprint })} onClick={() => setForgetting(host)}>
                  {t(($) => $.servers.forget)}
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {forgetting === null ? null : <ForgetDialog host={forgetting} workspaceId={workspaceId} onClose={() => setForgetting(null)} />}
    </>
  )
}

/** Page that lists the servers whose key the user trusted and lets them forget one. */
export function TrustedServers() {
  const { t } = useTranslation('settings')
  const workspace = useWorkspace()

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold tracking-tight">{t(($) => $.servers.title)}</h2>
        <p className="text-sm text-muted-foreground">{t(($) => $.servers.description)}</p>
      </div>
      {workspace === undefined ? null : <TrustedServersList workspaceId={workspace.id} />}
    </div>
  )
}
