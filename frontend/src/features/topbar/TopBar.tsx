import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatShortcut } from '@/lib/shortcuts'
import { useDialogsStore } from '@/state/dialogs-store'

import { VaultChip } from '../vault/VaultChip'

import { UserMenu } from './UserMenu'
import { WorkspaceSwitcher } from './WorkspaceSwitcher'

/** Top bar of the shell: brand, workspace, the button of the command palette (4.14c), vault state and the account menu. */
export function TopBar() {
  const { t } = useTranslation('shell')
  const { t: common } = useTranslation()
  const setPaletteOpen = useDialogsStore((state) => state.setPaletteOpen)

  return (
    <header aria-label={t(($) => $.topbar.label)} className="flex h-12 flex-none items-center gap-4 border-b bg-card px-4">
      <div className="flex items-center gap-2.5">
        <svg width="22" height="22" viewBox="0 0 22 22" fill="none" aria-hidden="true">
          <rect x="1.5" y="1.5" width="19" height="19" rx="5.5" stroke="var(--accent)" strokeWidth="1.6" />
          <circle cx="11" cy="11" r="3.2" fill="var(--accent)" />
        </svg>
        <h1 className="text-[15px] font-semibold tracking-tight">{common(($) => $.app.name)}</h1>
      </div>
      <WorkspaceSwitcher />
      <div className="flex flex-1 justify-center">
        <button
          type="button"
          onClick={() => setPaletteOpen(true)}
          className="flex h-8 w-full max-w-md items-center gap-2 rounded-lg border bg-muted/40 px-3 text-sm text-muted-foreground transition-colors hover:border-foreground/25 hover:bg-accent hover:text-foreground"
        >
          <Search aria-hidden="true" className="size-4 shrink-0" />
          <span className="flex-1 truncate text-start">{t(($) => $.palette.button)}</span>
          <kbd className="rounded border bg-background px-1.5 font-mono text-[10px]">{formatShortcut('Mod+K')}</kbd>
        </button>
      </div>
      <VaultChip />
      <UserMenu />
    </header>
  )
}
