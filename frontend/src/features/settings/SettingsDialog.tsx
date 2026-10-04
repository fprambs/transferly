import { KeyRound, Lock, ScrollText, Settings, ShieldCheck, Users, type LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { AdminPage } from '../admin/AdminPage'
import { useSession } from '../auth/use-session'
import { SecurityPage } from '../security/SecurityPage'
import { TrustedServers } from '../servers/TrustedServers'
import { AuditPage } from '../teams/AuditPage'
import { MembersPage } from '../teams/MembersPage'
import { canManageMembers } from '../teams/roles'
import { useWorkspace } from '../workspace/use-workspace'

import { useSettingsDialog, type SettingsSection } from './sections'
import { SettingsPage } from './SettingsPage'
import { useVersion } from './use-version'

/** The groups of the side bar, each with its sections and their icons. */
const groups: ReadonlyArray<{ key: 'account' | 'workspace' | 'instance'; items: ReadonlyArray<{ section: SettingsSection; icon: LucideIcon }> }> = [
  {
    key: 'account',
    items: [
      { section: 'general', icon: Settings },
      { section: 'security', icon: Lock },
    ],
  },
  {
    key: 'workspace',
    items: [
      { section: 'members', icon: Users },
      { section: 'audit', icon: ScrollText },
      { section: 'servers', icon: ShieldCheck },
    ],
  },
  { key: 'instance', items: [{ section: 'admin', icon: KeyRound }] },
]

/** The content of each section. */
const contents: Record<SettingsSection, () => ReactNode> = {
  general: () => <SettingsPage />,
  security: () => <SecurityPage />,
  servers: () => <TrustedServers />,
  members: () => <MembersPage />,
  audit: () => <AuditPage />,
  admin: () => <AdminPage />,
}

/**
 * Settings, security, the people and record of the workspace and the administration of the instance, in one dialog over the work in
 * course: a side bar of sections grouped by account, workspace and instance, and the section on the right. Closing it, with its button,
 * Escape or the back button, returns to the sites and connections exactly as they were.
 */
export function SettingsDialog() {
  const { t } = useTranslation('settings')
  const { section, open, close } = useSettingsDialog()
  const session = useSession()
  const workspace = useWorkspace()

  const version = useVersion()
  const shows = (item: SettingsSection): boolean => {
    if (item === 'admin') {
      return session?.user.role === 'admin'
    }
    if (item === 'audit') {
      return workspace?.kind === 'team' && canManageMembers(workspace.role)
    }
    return true
  }

  return (
    <Dialog open={section !== undefined} onOpenChange={(next) => !next && close()}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[min(880px,calc(100svh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl md:flex-row"
      >
        <nav aria-label={t(($) => $.dialog.title)} className="flex shrink-0 flex-col gap-4 border-b bg-muted/30 p-3 md:w-60 md:border-e md:border-b-0 md:p-4">
          <DialogTitle className="px-2 text-base font-semibold">{t(($) => $.dialog.title)}</DialogTitle>
          <div className="flex gap-4 overflow-x-auto md:flex-col md:overflow-visible">
            {groups.map((group) => {
              const items = group.items.filter((item) => shows(item.section))
              return items.length === 0 ? null : (
                <div key={group.key} className="flex shrink-0 gap-1 md:flex-col">
                  <p className="hidden px-2 pb-1 text-[11px] font-medium tracking-wider text-muted-foreground uppercase md:block">{t(($) => $.dialog.groups[group.key])}</p>
                  {items.map(({ section: item, icon: Icon }) => (
                    <button
                      key={item}
                      type="button"
                      aria-current={item === section ? 'page' : undefined}
                      onClick={() => open(item)}
                      className={cn(
                        'flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-start text-sm whitespace-nowrap',
                        item === section ? 'bg-primary/12 font-medium text-accent-text' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
                      )}
                    >
                      <Icon aria-hidden="true" className="size-4 shrink-0" />
                      {t(($) => $.dialog.sections[item])}
                    </button>
                  ))}
                </div>
              )
            })}
          </div>
          {version !== undefined && <p className="mt-auto hidden px-2 text-[11px] text-muted-foreground md:block">{t(($) => $.dialog.version, { version })}</p>}
        </nav>
        <div className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto bg-background">
          <button
            type="button"
            aria-label={t(($) => $.dialog.close)}
            onClick={close}
            className="absolute end-3 top-3 z-10 flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <XIcon />
          </button>
          {section === undefined ? null : contents[section]()}
        </div>
      </DialogContent>
    </Dialog>
  )
}

/** The cross of the close button. */
function XIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true" className="size-4">
      <path d="M18 6 6 18M6 6l12 12" />
    </svg>
  )
}
