import { useTranslation } from 'react-i18next'

import type { Connector } from '@/lib/api'

import type { ConnectorField, FieldReason } from './schema'

/** The texts of a field of a connector, as the connectors translations give them; a connector that brings fields without translations shows
 * their names. */
interface FieldTexts {
  /** The label of the field. */
  label?: string
  /** An explanation drawn under it. */
  hint?: string
  /** The placeholder of a secret that is stored, which typing replaces. */
  keep?: string
  /** The label of the empty choice of an optional choice. */
  empty?: string
  /** The labels of its choices, by value. */
  choices?: Record<string, string>
  /** An explanation of a chosen value, by value. */
  notes?: Record<string, string>
  /** A warning about a chosen value, by value. */
  warnings?: Record<string, string>
}

/** What {@link useConnectorTexts} returns. */
export interface ConnectorTexts {
  /** The label of the choice of a connector. */
  connectorLabel: string
  /** The name of a connector as the site form lists it. */
  connector: (connector: Connector) => string
  /** The texts of a field, by name; empty for a field without translations. */
  field: (name: string) => FieldTexts
  /** The label of a field, its name when it has no translation. */
  label: (name: string) => string
  /** The label of a value of a choice, the value itself when it has no translation. */
  choice: (name: string, value: string) => string
  /** The message that explains why a value breaks the schema of its field. */
  reason: (field: ConnectorField | undefined, reason: string) => string
  /** The placeholder of a secret that is stored. */
  keep: (name: string) => string
  /** The title of a group of fields drawn under one, or undefined for groups without one. */
  group: (group: string) => string | undefined
}

/** Merges the texts a connector gives a field over the texts every connector shares: its own label, hint and placeholders
 * win, and its choices, notes and warnings are added to the shared ones. */
function merge(shared: FieldTexts | undefined, own: FieldTexts | undefined): FieldTexts {
  return {
    ...shared,
    ...own,
    choices: { ...shared?.choices, ...own?.choices },
    notes: { ...shared?.notes, ...own?.notes },
    warnings: { ...shared?.warnings, ...own?.warnings },
  }
}

/** The reasons the interface words; any other reads as the generic one. */
const reasons: readonly FieldReason[] = ['required', 'number', 'range', 'boolean', 'choice', 'length', 'host', 'timezone']

/**
 * Returns the texts of the fields of connectors in the language of the interface; given the type of a connector, the texts it gives
 * its own fields, such as the endpoint of an object store, override the shared ones.
 */
export function useConnectorTexts(type?: string): ConnectorTexts {
  const { t } = useTranslation('connectors')
  const fields = t(($) => $.fields, { returnObjects: true }) as unknown as Record<string, FieldTexts | undefined>
  const overrides = t(($) => $.overrides, { returnObjects: true }) as unknown as Record<string, Record<string, FieldTexts | undefined> | undefined>
  const names = t(($) => $.connectors, { returnObjects: true }) as unknown as Record<string, string | undefined>
  const groups = t(($) => $.groups, { returnObjects: true }) as unknown as Record<string, string | undefined>
  const own = overrides[type ?? ''] ?? {}
  const field = (name: string): FieldTexts => merge(fields[name], own[name])
  return {
    connectorLabel: t(($) => $.connector),
    connector: (connector) => names[connector.type] ?? connector.name,
    field,
    label: (name) => field(name).label ?? name,
    choice: (name, value) => field(name).choices?.[value] ?? value,
    reason: (target, reason) => {
      const known = reasons.find((candidate) => candidate === reason) ?? 'choice'
      return t(($) => $.reasons[known], { min: target?.min, max: known === 'length' ? target?.maxLength : target?.max })
    },
    keep: (name) => field(name).keep ?? t(($) => $.keep),
    group: (group) => groups[group],
  }
}
