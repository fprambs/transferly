import type { FieldErrors, Resolver } from 'react-hook-form'

import type { Connector, QuickConnectInput } from '@/lib/api'

import { neededSecrets, toConfig, toSecrets, validate } from '../connectors/schema'

/** The logon types a quick connection cannot use: they ask when connecting or store nothing, which only a saved site can do. */
export const unattended: Readonly<Record<string, readonly string[]>> = { auth: ['ask', 'anonymous', 'interactive'] }

/** Values of the quick connect form: the connector, its settings and the secrets typed, by field name. */
export interface QuickValues {
  /** The type of the connector. */
  connector: string
  /** The settings of the connector. */
  config: Record<string, string>
  /** The secrets typed, kept in memory only until the connection ends. */
  secrets: Record<string, string>
}

/** The values of an empty quick connect form for a connector. */
export function quickDefaults(connector: string): QuickValues {
  return { connector, config: {}, secrets: {} }
}

/** Builds the request body of a quick connection: the settings that apply and were given, and the secrets typed for fields that apply. */
export function toQuickInput(values: QuickValues, connector: Connector): QuickConnectInput {
  return { connector: values.connector, config: toConfig(connector, values.config), secrets: toSecrets(connector, values.config, values.secrets) }
}

/**
 * A resolver that checks the settings of the chosen connector with its schema, and that the secrets its logon needs are typed, as nothing is
 * stored to fall back on; a connector missing from the catalog is left to the server.
 */
export function quickResolver(connectors: readonly Connector[]): Resolver<QuickValues> {
  return (values) => {
    const connector = connectors.find((candidate) => candidate.type === values.connector)
    const failures = connector === undefined ? {} : validate(connector, values.config)
    const missing = connector === undefined ? [] : neededSecrets(connector, values.config).filter((name) => (values.secrets[name] ?? '') === '')
    if (Object.keys(failures).length === 0 && missing.length === 0) {
      return { values, errors: {} }
    }
    const config = Object.fromEntries(Object.entries(failures).map(([name, reason]) => [name, { type: 'schema', message: reason }]))
    const secrets = Object.fromEntries(missing.map((name) => [name, { type: 'schema', message: 'required' }]))
    return { values: {}, errors: { config, secrets } as FieldErrors<QuickValues> }
  }
}
