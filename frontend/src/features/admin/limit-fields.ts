import type { InstanceLimits } from '@/lib/api'

/** Bytes in a mebibyte, the unit bandwidth is shown in. */
const mebibyte = 1024 * 1024

/** Bytes in a gibibyte, the unit of the daily volume. */
const gibibyte = 1024 * mebibyte

/** The groups the fields of the limits form are gathered in. */
export type LimitGroup = 'bandwidth' | 'transfers' | 'quotas'

/** One number of the limits form. */
export interface LimitField {
  /** Name of the field in the limits of the server. */
  key: keyof InstanceLimits
  /** The group it is drawn in. */
  group: LimitGroup
  /** How many units of the server one unit of the form is worth: bytes per mebibyte, bytes per gibibyte or one. */
  factor: number
  /** The unit written next to the field, or an empty text for a plain count. */
  unit: 'MiB/s' | 'GiB' | ''
}

/** The fields of the form in the order they are drawn. */
export const limitFields: readonly LimitField[] = [
  { key: 'bandwidthGlobal', group: 'bandwidth', factor: mebibyte, unit: 'MiB/s' },
  { key: 'bandwidthPerUser', group: 'bandwidth', factor: mebibyte, unit: 'MiB/s' },
  { key: 'transfersGlobal', group: 'transfers', factor: 1, unit: '' },
  { key: 'transfersPerWorkspace', group: 'transfers', factor: 1, unit: '' },
  { key: 'transfersPerUser', group: 'transfers', factor: 1, unit: '' },
  { key: 'transfersPerSite', group: 'transfers', factor: 1, unit: '' },
  { key: 'maxSitesPerWorkspace', group: 'quotas', factor: 1, unit: '' },
  { key: 'maxTeamsPerUser', group: 'quotas', factor: 1, unit: '' },
  { key: 'maxMembersPerTeam', group: 'quotas', factor: 1, unit: '' },
  { key: 'dailyBytesPerUser', group: 'quotas', factor: gibibyte, unit: 'GiB' },
]

/** Writes a limit of the server as the text of its field: empty for no limit, otherwise the number in the unit of the field. */
export function limitText(field: LimitField, value: number): string {
  return value === 0 ? '' : String(Number((value / field.factor).toFixed(3)))
}

/** Reads the text of a field as a limit of the server, or `null` when it is not a whole, non-negative amount. An empty text is no limit. */
export function limitValue(field: LimitField, text: string): number | null {
  const trimmed = text.trim()
  if (trimmed === '') {
    return 0
  }
  const amount = Number(trimmed)
  return Number.isFinite(amount) && amount >= 0 ? Math.round(amount * field.factor) : null
}

/** Turns the limits of the server into the texts of the form. */
export function limitTexts(limits: InstanceLimits): Record<keyof InstanceLimits, string> {
  const texts = {} as Record<keyof InstanceLimits, string>
  for (const field of limitFields) {
    texts[field.key] = limitText(field, limits[field.key])
  }
  return texts
}

/** Turns the texts of the form into limits, or `null` when one of them is not a valid amount. */
export function limitsFromTexts(texts: Record<keyof InstanceLimits, string>): InstanceLimits | null {
  const limits = {} as InstanceLimits
  for (const field of limitFields) {
    const value = limitValue(field, texts[field.key])
    if (value === null) {
      return null
    }
    limits[field.key] = value
  }
  return limits
}
