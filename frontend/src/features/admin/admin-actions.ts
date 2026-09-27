/** Translation keys of the actions of the trail of the instance the interface words in full. */
const known = {
  'admin.user_disabled': 'userDisabled',
  'admin.user_enabled': 'userEnabled',
  'admin.role_changed': 'roleChanged',
  'admin.second_factor_reset': 'secondFactorReset',
  'admin.networks_changed': 'networksChanged',
} as const

/** Translation key of the sentence that describes an action; an action the interface does not know gets the generic one. */
export type AdminActionKey = (typeof known)[keyof typeof known] | 'unknown'

/** Maps an action of the trail to the key of its sentence. */
export function adminActionKey(action: string): AdminActionKey {
  return (known as Readonly<Record<string, AdminActionKey>>)[action] ?? 'unknown'
}

/** The groups the trail can be narrowed to, with the action prefix that selects each. */
export const adminCategories = [
  { key: 'all', prefix: '' },
  { key: 'accounts', prefix: 'admin.user_' },
  { key: 'roles', prefix: 'admin.role_' },
  { key: 'security', prefix: 'admin.second_' },
  { key: 'networks', prefix: 'admin.networks_' },
] as const

/** How many events each page of the trail holds. */
export const adminPageSize = 50
