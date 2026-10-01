import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTable, Td, Th, Tr } from '@/components/page/DataTable'
import { Badge, Page, PageHeader, Section } from '@/components/page/Page'
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
          <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
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
    return <Loading label={t(($) => $.servers.loading)} className="px-5 py-4" />
  }
  if (hosts.isError) {
    return (
      <p role="alert" className="px-5 py-4 text-sm text-destructive">
        {t(($) => $.servers.loadFailed)}
      </p>
    )
  }
  if (hosts.data.length === 0) {
    return <p className="px-5 py-8 text-center text-sm text-muted-foreground">{t(($) => $.servers.empty)}</p>
  }
  return (
    <>
      <DataTable aria-label={t(($) => $.servers.listTitle)}>
        <thead>
          <tr>
            <Th>{t(($) => $.servers.host)}</Th>
            <Th>{t(($) => $.servers.keyType)}</Th>
            <Th>{t(($) => $.servers.fingerprint)}</Th>
            <Th>{t(($) => $.servers.firstSeen)}</Th>
            <Th>
              <span className="sr-only">{t(($) => $.servers.forget)}</span>
            </Th>
          </tr>
        </thead>
        <tbody>
          {hosts.data.map((host) => (
            <Tr key={`${host.host}:${host.port}:${host.keyType}:${host.fingerprint}`}>
              <Td className="font-mono whitespace-nowrap">
                {host.host}:{host.port}
              </Td>
              <Td>
                <Badge tone="info">{host.keyType}</Badge>
              </Td>
              <Td className="font-mono text-xs break-all text-muted-foreground">{host.fingerprint}</Td>
              <Td className="whitespace-nowrap text-muted-foreground">
                {formatInstant(host.firstSeen, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })}
              </Td>
              <Td className="text-end">
                <Button variant="ghost" size="sm" aria-label={t(($) => $.servers.forgetLabel, { host: `${host.host}:${host.port}`, fingerprint: host.fingerprint })} onClick={() => setForgetting(host)}>
                  {t(($) => $.servers.forget)}
                </Button>
              </Td>
            </Tr>
          ))}
        </tbody>
      </DataTable>
      {forgetting === null ? null : <ForgetDialog host={forgetting} workspaceId={workspaceId} onClose={() => setForgetting(null)} />}
    </>
  )
}

/** Page that lists the servers whose key the user trusted and lets them forget one. */
export function TrustedServers() {
  const { t } = useTranslation('settings')
  const workspace = useWorkspace()

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <Page>
        <PageHeader icon={ShieldCheck} title={t(($) => $.servers.title)} description={t(($) => $.servers.description)} />
        <Section id="trusted-keys-title" title={t(($) => $.servers.listTitle)} flush>
          {workspace === undefined ? null : <TrustedServersList workspaceId={workspace.id} />}
        </Section>
      </Page>
    </div>
  )
}
