import { useMatch, useNavigate } from '@tanstack/react-router'
import { Command } from 'cmdk'
import {
  Bookmark,
  CircleStop,
  KeyRound,
  ListFilter,
  Lock,
  type LucideIcon,
  RefreshCw,
  ScrollText,
  Search,
  Server,
  Settings,
  ShieldCheck,
  SquareStack,
  Unplug,
  Users,
  Zap,
} from 'lucide-react'
import { useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { formatShortcut, holdsMod } from '@/lib/shortcuts'
import { cn } from '@/lib/utils'
import { useDialogsStore } from '@/state/dialogs-store'
import { useUiStore } from '@/state/ui-store'

import { useSession } from '../auth/use-session'
import { useBookmarks } from '../bookmarks/use-bookmarks'
import { useOpenConnections } from '../sites/use-open-connections'
import { useRecentSites } from '../sites/recent-sites'
import { useSitesData } from '../sites/use-sites-data'
import { useSettingsDialog } from '../settings/sections'
import { useTabs } from '../tabs/tabs-store'
import { useGoToTab } from '../tabs/use-go-to-tab'
import { canManageMembers } from '../teams/roles'
import { panelSwitches, useCommandTarget, useConnectionCommands } from '../toolbar/commands'
import { useWorkspace } from '../workspace/use-workspace'

import { focusFilterEvent } from './focus-filter'

/** Props of {@link Item}. */
interface ItemProps {
  /** Identity of the item among all of the palette. */
  value: string
  /** Words the search matches, besides the value. */
  keywords: string[]
  /** Icon of the item. */
  icon: LucideIcon
  /** Classes of the icon. */
  iconClassName?: string
  /** What the item is or does. */
  label: string
  /** Detail after the label, such as a host or a path. */
  detail?: string
  /** What sits at the end: a shortcut or a state. */
  end?: ReactNode
  /** Whether it cannot be run now. */
  disabled?: boolean
  /** Runs it. */
  onSelect: () => void
}

/** One entry of the palette. */
function Item({ value, keywords, icon: Icon, iconClassName, label, detail, end, disabled, onSelect }: ItemProps) {
  return (
    <Command.Item
      value={value}
      keywords={keywords}
      disabled={disabled}
      onSelect={onSelect}
      className="flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm outline-none select-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-45 data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground"
    >
      <Icon aria-hidden="true" className={cn('size-4 shrink-0 text-muted-foreground', iconClassName)} />
      <span className="truncate">{label}</span>
      {detail !== undefined && <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">{detail}</span>}
      {end !== undefined && <span className="ms-auto flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">{end}</span>}
    </Command.Item>
  )
}

/** A shortcut written for the system of the user. */
function Keys({ spec }: { spec: string }) {
  return <kbd className="rounded border bg-muted px-1.5 font-mono text-[10px]">{formatShortcut(spec)}</kbd>
}

/** Props of {@link CommandPalette}. */
export interface CommandPaletteProps {
  /** Opens a saved site or a quick connection, as the sites panel does. */
  onOpenSite: (siteId: string) => void
}

/** No recent sites; stable, so the store selector does not rerender for nothing. */
const noRecent: string[] = []

/**
 * The command palette (4.14c): the main modifier and K (Ctrl on Windows and Linux, ⌘ on Apple systems) or the button of the top bar open
 * it, and a search reaches the actions on the connection in focus, the tabs, the bookmarks of the site in view, every site, the panels
 * and the pages, with the shortcut of each action written for the system; each opening starts with an empty search. Built on cmdk
 * (docs/guides/cmdk.md).
 */
export function CommandPalette({ onOpenSite }: CommandPaletteProps) {
  const { t } = useTranslation('shell')
  const { t: explorer } = useTranslation('explorer')
  const open = useDialogsStore((state) => state.paletteOpen)
  const setOpen = useDialogsStore((state) => state.setPaletteOpen)
  const setQuickConnectOpen = useDialogsStore((state) => state.setQuickConnectOpen)
  const navigate = useNavigate()
  const settingsDialog = useSettingsDialog()
  const workspace = useWorkspace()
  const session = useSession()
  const ui = useUiStore()
  const { sites, quick } = useSitesData(workspace?.id)
  const connected = useOpenConnections(workspace?.id)
  const tabs = useTabs((state) => state.tabs).filter((tab) => tab.workspaceId === workspace?.id)
  const activeTab = useTabs((state) => state.activeId)
  const goToTab = useGoToTab()
  const target = useCommandTarget()
  const commands = useConnectionCommands(target ?? undefined)
  const match = useMatch({ from: '/shell/sites/$siteId', shouldThrow: false })
  const bookmarkedSite = sites.find((site) => site.id === target?.siteId)
  const bookmarks = useBookmarks(workspace?.id ?? '', bookmarkedSite?.id ?? '', open && bookmarkedSite !== undefined)
  const bookmarkList = bookmarks.data ?? []
  const names = new Map([...sites, ...quick].map((connection) => [connection.id, connection.name]))
  const recentIds = useRecentSites((state) => (workspace === undefined ? noRecent : (state.recent[workspace.id] ?? noRecent)))
  const connections = [...sites, ...quick]
  const recent = recentIds.flatMap((id) => connections.filter((connection) => connection.id === id))
  const others = connections.filter((connection) => !recentIds.includes(connection.id))

  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => {
      if (holdsMod(event) && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setOpen(!useDialogsStore.getState().paletteOpen)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [setOpen])

  const run = (action: () => void) => (): void => {
    setOpen(false)
    action()
  }
  const siteItem = (connection: (typeof connections)[number], value: string) => (
    <Item
      key={value}
      value={value}
      keywords={[connection.name, connection.host, connection.username]}
      icon={'secrets' in connection ? Server : Zap}
      label={connection.name}
      detail={`${connection.username}@${connection.host}`}
      end={connected.has(connection.id) ? <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-ok" />{t(($) => $.sidebar.connected)}</span> : undefined}
      onSelect={run(() => onOpenSite(connection.id))}
    />
  )
  const openBookmark = (path: string): void => {
    const view = match as NonNullable<typeof match>
    const pane = view.params.siteId === target?.siteId ? 'path' : 'splitPath'
    void navigate({ to: '/sites/$siteId', params: { siteId: view.params.siteId }, search: { ...view.search, [pane]: path } })
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent showCloseButton={false} className="top-[15vh] translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-xl">
        <DialogTitle className="sr-only">{t(($) => $.palette.title)}</DialogTitle>
        <Command key={String(open)} label={t(($) => $.palette.title)} loop className="flex flex-col">
          <div className="flex items-center gap-2 border-b px-3">
            <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
            <Command.Input placeholder={t(($) => $.palette.placeholder)} className="h-12 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground" />
            <kbd className="rounded border bg-muted px-1.5 font-mono text-[10px] text-muted-foreground">Esc</kbd>
          </div>
          <Command.List className="max-h-[min(60vh,28rem)] overflow-y-auto p-1.5 **:[[cmdk-group-heading]]:px-2 **:[[cmdk-group-heading]]:pt-2 **:[[cmdk-group-heading]]:pb-1 **:[[cmdk-group-heading]]:text-[11px] **:[[cmdk-group-heading]]:font-medium **:[[cmdk-group-heading]]:tracking-wider **:[[cmdk-group-heading]]:text-muted-foreground **:[[cmdk-group-heading]]:uppercase">
            <Command.Empty className="px-3 py-8 text-center text-sm text-muted-foreground">{t(($) => $.palette.empty)}</Command.Empty>
            <Command.Group heading={t(($) => $.palette.groups.actions)}>
              {target !== null && (
                <>
                  <Item value="action-refresh" keywords={[explorer(($) => $.commands.refresh), 'reload']} icon={RefreshCw} iconClassName="text-accent-text" label={explorer(($) => $.commands.refresh)} detail={commands.name} disabled={commands.idle} onSelect={run(commands.refresh)} />
                  <Item value="action-cancel" keywords={[explorer(($) => $.commands.cancel), 'stop']} icon={CircleStop} iconClassName="text-destructive" label={explorer(($) => $.commands.cancel)} disabled={!commands.busy} onSelect={run(commands.cancel)} />
                  <Item value="action-disconnect" keywords={[explorer(($) => $.commands.disconnect)]} icon={Unplug} iconClassName="text-warn" label={explorer(($) => $.commands.disconnect)} detail={commands.name} disabled={commands.idle} onSelect={run(() => void commands.disconnect())} />
                  <Item value="action-filter" keywords={[explorer(($) => $.filter.label), 'filter']} icon={ListFilter} label={explorer(($) => $.filter.label)} end={<Keys spec="/" />} onSelect={run(() => window.dispatchEvent(new Event(focusFilterEvent)))} />
                </>
              )}
              <Item value="action-quick" keywords={[t(($) => $.sidebar.quickConnect)]} icon={Zap} iconClassName="text-warn" label={t(($) => $.sidebar.quickConnect)} end={<Keys spec="Mod+Shift+K" />} onSelect={run(() => setQuickConnectOpen(true))} />
            </Command.Group>
            {tabs.length > 0 && (
              <Command.Group heading={t(($) => $.palette.groups.tabs)}>
                {tabs.map((tab) => {
                  const name = names.get(tab.siteId) ?? tab.siteId
                  return (
                    <Item
                      key={tab.id}
                      value={`tab-${tab.id}`}
                      keywords={[name, tab.path]}
                      icon={SquareStack}
                      label={name}
                      detail={tab.path}
                      end={tab.id === activeTab ? t(($) => $.palette.current) : undefined}
                      onSelect={run(() => goToTab(tab))}
                    />
                  )
                })}
              </Command.Group>
            )}
            {bookmarkedSite !== undefined && bookmarkList.length > 0 && (
              <Command.Group heading={t(($) => $.palette.groups.bookmarks, { site: bookmarkedSite.name })}>
                {bookmarkList.map((bookmark) => (
                  <Item key={bookmark.id} value={`bookmark-${bookmark.id}`} keywords={[bookmark.name, bookmark.path]} icon={Bookmark} iconClassName="text-warn" label={bookmark.name} detail={bookmark.path} onSelect={run(() => openBookmark(bookmark.path))} />
                ))}
              </Command.Group>
            )}
            {recent.length > 0 && (
              <Command.Group heading={t(($) => $.palette.groups.recent)}>
                {recent.map((connection) => (
                  siteItem(connection, `recent-${connection.id}`)
                ))}
              </Command.Group>
            )}
            {others.length > 0 && (
              <Command.Group heading={t(($) => $.palette.groups.sites)}>
                {others.map((connection) => (
                  siteItem(connection, `site-${connection.id}`)
                ))}
              </Command.Group>
            )}
            <Command.Group heading={t(($) => $.palette.groups.panels)}>
              {panelSwitches.map((panel) => {
                const shown = panel.open(ui)
                return (
                  <Item
                    key={panel.key}
                    value={`panel-${panel.key}`}
                    keywords={[explorer(($) => $.commands.panels[panel.key]), 'panel']}
                    icon={panel.icon}
                    label={shown ? t(($) => $.palette.hide, { panel: t(($) => $.palette.panelNames[panel.key]) }) : t(($) => $.palette.show, { panel: t(($) => $.palette.panelNames[panel.key]) })}
                    end={'shortcut' in panel ? <Keys spec={panel.shortcut} /> : undefined}
                    onSelect={run(() => panel.set(ui)(!shown))}
                  />
                )
              })}
            </Command.Group>
            <Command.Group heading={t(($) => $.palette.groups.pages)}>
              <Item value="page-settings" keywords={[t(($) => $.palette.pages.settings)]} icon={Settings} label={t(($) => $.palette.pages.settings)} onSelect={run(() => settingsDialog.open('general'))} />
              <Item value="page-security" keywords={[t(($) => $.palette.pages.security)]} icon={Lock} label={t(($) => $.palette.pages.security)} onSelect={run(() => settingsDialog.open('security'))} />
              <Item value="page-servers" keywords={[t(($) => $.palette.pages.servers)]} icon={ShieldCheck} label={t(($) => $.palette.pages.servers)} onSelect={run(() => settingsDialog.open('servers'))} />
              {workspace?.kind === 'team' && <Item value="page-members" keywords={[t(($) => $.palette.pages.members)]} icon={Users} label={t(($) => $.palette.pages.members)} onSelect={run(() => settingsDialog.open('members'))} />}
              {workspace?.kind === 'team' && canManageMembers(workspace.role) && <Item value="page-audit" keywords={[t(($) => $.palette.pages.audit)]} icon={ScrollText} label={t(($) => $.palette.pages.audit)} onSelect={run(() => settingsDialog.open('audit'))} />}
              {session?.user.role === 'admin' && <Item value="page-admin" keywords={[t(($) => $.palette.pages.admin)]} icon={KeyRound} label={t(($) => $.palette.pages.admin)} onSelect={run(() => settingsDialog.open('admin'))} />}
            </Command.Group>
          </Command.List>
          <div className="flex items-center gap-4 border-t px-3 py-2 text-[11px] text-muted-foreground">
            <span className="flex items-center gap-1"><Keys spec="↑" /><Keys spec="↓" /> {t(($) => $.palette.hints.move)}</span>
            <span className="flex items-center gap-1"><Keys spec="Enter" /> {t(($) => $.palette.hints.run)}</span>
            <span className="flex items-center gap-1"><Keys spec="Esc" /> {t(($) => $.palette.hints.close)}</span>
          </div>
        </Command>
      </DialogContent>
    </Dialog>
  )
}
