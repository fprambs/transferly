import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { Role } from './roles'

/** Colors of each role; they differ in lightness as well as hue so they can be told apart without color. */
const tones: Readonly<Record<Role, string>> = {
  owner: 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
  admin: 'bg-violet-500/15 text-violet-700 dark:text-violet-300',
  editor: 'bg-sky-500/15 text-sky-700 dark:text-sky-300',
  viewer: 'bg-muted text-muted-foreground',
}

/** Props of {@link RoleBadge}. */
export interface RoleBadgeProps {
  /** The role to show. */
  role: Role
}

/** A small pill with the translated name of a role. */
export function RoleBadge({ role }: RoleBadgeProps) {
  const { t } = useTranslation('teams')
  return <span className={cn('inline-flex h-[22px] items-center rounded-full px-2.5 text-xs font-medium', tones[role])}>{t(($) => $.roles[role])}</span>
}
