import { useTranslation } from 'react-i18next'

import { LanguageSelect } from '../shell/LanguageSelect'
import { ThemeSelect } from '../shell/ThemeSelect'
import { VaultChip } from '../vault/VaultChip'

import { UserMenu } from './UserMenu'
import { WorkspaceSwitcher } from './WorkspaceSwitcher'

/** Top bar of the shell: brand, workspace, vault state, preferences and the account menu. */
export function TopBar() {
  const { t } = useTranslation('shell')
  const { t: common } = useTranslation()

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
      <div className="flex-1" />
      <ThemeSelect />
      <LanguageSelect />
      <VaultChip />
      <UserMenu />
    </header>
  )
}
