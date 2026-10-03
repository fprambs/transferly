/** Translation keys of the actions of the trail of the instance the interface words in full. */
const known = {
  'admin.user_disabled': 'userDisabled',
  'admin.user_enabled': 'userEnabled',
  'admin.role_changed': 'roleChanged',
  'admin.second_factor_reset': 'secondFactorReset',
  'admin.networks_changed': 'networksChanged',
  'admin.limits_changed': 'limitsChanged',
} as const

/** Translation key of the sentence that describes an action; an action the interface does not know gets the generic one. */
export type AdminActionKey = (typeof known)[keyof typeof known] | 'connectorEnabled' | 'connectorDisabled' | 'unknown'

/** Maps an action of the trail, with its metadata, to the key of its sentence. A connector change says which way it went. */
export function adminActionKey(action: string, meta: Readonly<Record<string, string>> = {}): AdminActionKey {
  if (action === 'admin.connector_changed') {
    return meta.enabled === 'true' ? 'connectorEnabled' : 'connectorDisabled'
  }
  return (known as Readonly<Record<string, AdminActionKey>>)[action] ?? 'unknown'
}

/** The groups the trail can be narrowed to, with the action prefix that selects each. */
export const adminCategories = [
  { key: 'all', prefix: '' },
  { key: 'accounts', prefix: 'admin.user_' },
  { key: 'roles', prefix: 'admin.role_' },
  { key: 'security', prefix: 'admin.second_' },
  { key: 'networks', prefix: 'admin.networks_' },
  { key: 'limits', prefix: 'admin.limits_' },
  { key: 'connectors', prefix: 'admin.connector_' },
] as const

/** How many events each page of the trail holds. */
export const adminPageSize = 50
