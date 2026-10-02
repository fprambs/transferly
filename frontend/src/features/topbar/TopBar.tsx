import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatShortcut } from '@/lib/shortcuts'
import { BrandMark } from '@/components/brand/BrandMark'
import { useDialogsStore } from '@/state/dialogs-store'

import { VaultChip } from '../vault/VaultChip'

import { UserMenu } from './UserMenu'
import { WorkspaceSwitcher } from './WorkspaceSwitcher'

/**
 * Top bar of the shell: brand, workspace, the button of the command palette (4.14c), vault state and the account menu. In a narrow window
 * the name of the product and the text of the palette button are left to screen readers, so the button is a search icon.
 */
export function TopBar() {
  const { t } = useTranslation('shell')
  const { t: common } = useTranslation()
  const setPaletteOpen = useDialogsStore((state) => state.setPaletteOpen)

  return (
    <header aria-label={t(($) => $.topbar.label)} className="flex h-12 flex-none items-center gap-2 border-b bg-card px-3 md:gap-4 md:px-4">
      <div className="flex items-center gap-2.5">
        <BrandMark className="size-6" />
        <h1 className="text-[15px] font-semibold tracking-tight max-md:sr-only">{common(($) => $.app.name)}</h1>
      </div>
      <WorkspaceSwitcher />
      <div className="flex flex-1 justify-end md:justify-center">
        <button
          type="button"
          onClick={() => setPaletteOpen(true)}
          className="flex h-8 items-center gap-2 rounded-lg border bg-muted/40 px-2 text-sm text-muted-foreground transition-colors hover:border-foreground/25 hover:bg-accent hover:text-foreground md:w-full md:max-w-md md:px-3"
        >
          <Search aria-hidden="true" className="size-4 shrink-0" />
          <span className="flex-1 truncate text-start max-md:sr-only">{t(($) => $.palette.button)}</span>
          <kbd className="rounded border bg-background px-1.5 font-mono text-[10px] max-md:hidden">{formatShortcut('Mod+K')}</kbd>
        </button>
      </div>
      <VaultChip />
      <UserMenu />
    </header>
  )
}
