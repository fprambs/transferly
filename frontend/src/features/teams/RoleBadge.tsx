import { useTranslation } from 'react-i18next'

import { Badge, type Tone } from '@/components/page/Page'

import type { Role } from './roles'

/** Colors of each role; they differ in lightness as well as hue so they can be told apart without color. */
const tones: Readonly<Record<Role, Tone>> = {
  owner: 'accent',
  admin: 'info',
  editor: 'ok',
  viewer: 'muted',
}

/** Props of {@link RoleBadge}. */
export interface RoleBadgeProps {
  /** The role to show. */
  role: Role
}

/** A small pill with the translated name of a role. */
export function RoleBadge({ role }: RoleBadgeProps) {
  const { t } = useTranslation('teams')
  return <Badge tone={tones[role]}>{t(($) => $.roles[role])}</Badge>
}
