import { useNavigate } from '@tanstack/react-router'
import { Check, Command, KeyRound, Plus, Server, Users, Zap, type LucideIcon } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Illustration } from '@/components/brand/Illustration'
import { Button } from '@/components/ui/button'
import { formatShortcut } from '@/lib/shortcuts'
import { cn } from '@/lib/utils'
import { useDialogsStore } from '@/state/dialogs-store'

import { useRecentSites } from '../sites/recent-sites'
import { colorFill } from '../sites/site-colors'
import { SiteDialog, type SiteTarget } from '../sites/SiteDialog'
import { startPath } from '../sites/start-path'
import { useSitesData } from '../sites/use-sites-data'
import { canManageSites } from '../teams/roles'
import { NewTeamDialog } from '../teams/NewTeamDialog'
import { useConnectionState } from '../toolbar/connection-store'
import { useVaultStatus } from '../vault/use-vault'
import { VaultDialog } from '../vault/VaultDialog'
import { useWorkspace, useWorkspaces } from '../workspace/use-workspace'

/** No recent sites; stable, so the store selector does not rerender for nothing. */
const none: string[] = []

/** Props of {@link Step}. */
interface StepProps {
  /** Icon of the step. */
  icon: LucideIcon
  /** What the step is. */
  title: string
  /** Whether it is done, which checks it and leaves its button out. */
  done: boolean
  /** The button that does it. */
  children: ReactNode
}

/** One step of the first steps: its icon, or a check once done, its name and the button that does it. */
function Step({ icon: Icon, title, done, children }: StepProps) {
  return (
    <li className="flex items-center gap-3 border-b px-4 py-3 last:border-b-0">
      <span className={cn('flex size-8 shrink-0 items-center justify-center rounded-full', done ? 'bg-ok/15 text-ok-text' : 'bg-primary/12 text-accent-text')}>
        {done ? <Check aria-hidden="true" className="size-4" /> : <Icon aria-hidden="true" className="size-4" />}
      </span>
      <span className={cn('flex-1 text-sm', done && 'text-muted-foreground line-through')}>{title}</span>
      {!done && children}
    </li>
  )
}

/**
 * What the explorer area shows before any site is opened: the sites opened last in the workspace to open again with one click, quick
 * connect and the command palette, and, until they are all done, the first steps: set up the vault, add a site and invite a team.
 */
export function EmptyExplorer() {
  const { t } = useTranslation('explorer')
  const navigate = useNavigate()
  const workspace = useWorkspace()
  const workspaces = useWorkspaces()
  const { sites, quick, collections } = useSitesData(workspace?.id)
  const recentIds = useRecentSites((state) => (workspace === undefined ? none : (state.recent[workspace.id] ?? none)))
  const reconnectSite = useConnectionState((state) => state.reconnectSite)
  const setQuickConnectOpen = useDialogsStore((state) => state.setQuickConnectOpen)
  const setPaletteOpen = useDialogsStore((state) => state.setPaletteOpen)
  const vault = useVaultStatus()
  const [siteTarget, setSiteTarget] = useState<SiteTarget | null>(null)
  const [settingUpVault, setSettingUpVault] = useState(false)
  const [creatingTeam, setCreatingTeam] = useState(false)

  const connections = [...sites, ...quick]
  const recent = recentIds.flatMap((id) => connections.filter((connection) => connection.id === id))
  const steps = {
    vault: vault?.initialized === true,
    site: sites.length > 0,
    team: workspaces.some((candidate) => candidate.kind === 'team'),
  }
  const showSteps = vault !== undefined && workspace !== undefined && !(steps.vault && steps.site && steps.team)
  const open = (siteId: string): void => {
    reconnectSite(siteId)
    void navigate({ to: '/sites/$siteId', params: { siteId }, search: { path: startPath(sites.find((site) => site.id === siteId)) } })
  }

  return (
    <div className="flex flex-1 flex-col items-center overflow-y-auto p-8">
      <div className="flex w-full max-w-2xl flex-col gap-6">
        <div className="flex flex-col items-center gap-3 pt-4 text-center">
          <Illustration scene="start" className="mb-1 h-24 w-36" />
          <h2 className="text-base font-medium">{t(($) => $.empty.title)}</h2>
          <p className="max-w-sm text-sm text-muted-foreground">{t(($) => $.empty.body)}</p>
          <div className="flex flex-wrap justify-center gap-2 pt-1">
            <Button size="sm" onClick={() => setQuickConnectOpen(true)}>
              <Zap />
              {t(($) => $.empty.quickConnect)}
              <kbd aria-hidden="true" className="ms-1 font-mono text-[11px]">{formatShortcut('Mod+Shift+K')}</kbd>
            </Button>
            <Button variant="outline" size="sm" onClick={() => setPaletteOpen(true)}>
              <Command />
              {t(($) => $.empty.palette)}
              <kbd aria-hidden="true" className="ms-1 font-mono text-[11px] text-muted-foreground">{formatShortcut('Mod+K')}</kbd>
            </Button>
          </div>
        </div>
        {recent.length > 0 && (
          <section aria-labelledby="recent-title" className="flex flex-col gap-2">
            <h3 id="recent-title" className="text-[11px] font-medium tracking-widest text-muted-foreground uppercase">
              {t(($) => $.empty.recent)}
            </h3>
            <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {recent.map((connection) => {
                const color = 'color' in connection ? connection.color : undefined
                return (
                  <li key={connection.id}>
                    <button
                      type="button"
                      onClick={() => open(connection.id)}
                      className="flex w-full items-center gap-3 rounded-xl border bg-card px-3.5 py-3 text-start shadow-xs transition-colors hover:border-foreground/25 hover:bg-accent"
                    >
                      <span className="relative flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                        <Server aria-hidden="true" className="size-4" />
                        {color !== undefined && <span aria-hidden="true" className={cn('absolute -end-0.5 -top-0.5 size-2.5 rounded-full ring-2 ring-card', colorFill(color))} />}
                      </span>
                      <span className="flex min-w-0 flex-col">
                        <span className="truncate text-sm font-medium">{connection.name}</span>
                        <span className="truncate font-mono text-xs text-muted-foreground">
                          {connection.protocol.toUpperCase()} · {connection.host}
                        </span>
                      </span>
                    </button>
                  </li>
                )
              })}
            </ul>
          </section>
        )}
        {showSteps && (
          <section aria-labelledby="steps-title" className="flex flex-col gap-2">
            <h3 id="steps-title" className="text-[11px] font-medium tracking-widest text-muted-foreground uppercase">
              {t(($) => $.empty.steps.title)}
            </h3>
            <ol className="overflow-hidden rounded-xl border bg-card shadow-xs">
              <Step icon={KeyRound} title={t(($) => $.empty.steps.vault)} done={steps.vault}>
                <Button variant="outline" size="sm" onClick={() => setSettingUpVault(true)}>
                  {t(($) => $.empty.steps.vaultAction)}
                </Button>
              </Step>
              <Step icon={Plus} title={t(($) => $.empty.steps.site)} done={steps.site}>
                {canManageSites(workspace.role) && (
                  <Button variant="outline" size="sm" onClick={() => setSiteTarget({ mode: 'create' })}>
                    {t(($) => $.empty.steps.siteAction)}
                  </Button>
                )}
              </Step>
              <Step icon={Users} title={t(($) => $.empty.steps.team)} done={steps.team}>
                <Button variant="outline" size="sm" onClick={() => setCreatingTeam(true)}>
                  {t(($) => $.empty.steps.teamAction)}
                </Button>
              </Step>
            </ol>
          </section>
        )}
      </div>
      {workspace !== undefined && <SiteDialog target={siteTarget} workspaceId={workspace.id} collections={collections} onClose={() => setSiteTarget(null)} />}
      <VaultDialog mode={settingUpVault ? 'setup' : null} onClose={() => setSettingUpVault(false)} />
      <NewTeamDialog open={creatingTeam} onClose={() => setCreatingTeam(false)} />
    </div>
  )
}
