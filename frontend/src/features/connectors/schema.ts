import type { Connector } from '@/lib/api'

/** A field of the schema of a connector (ADR-0022). */
export type ConnectorField = Connector['fields'][number]

/** When a field applies, from the value of a field declared before it. */
export type ConnectorCondition = NonNullable<ConnectorField['when']>

/** The tab of the site form a field is shown on. */
export type FieldGroup = ConnectorField['group']

/** Values of the settings of a connector, by field name, as text. */
export type ConfigValues = Record<string, string>

/** Why a value breaks the schema of its field, as the server names it in invalid_config. */
export type FieldReason = 'required' | 'number' | 'range' | 'boolean' | 'choice' | 'length' | 'host' | 'timezone' | 'connect'

/** Whether a condition holds for values settled so far: the other field has one of the values, or any value when none are listed. */
export function holds(condition: ConnectorCondition, values: ConfigValues): boolean {
  const value = values[condition.field] ?? ''
  return condition.in === undefined || condition.in.length === 0 ? value !== '' : condition.in.includes(value)
}

/** Whether a field applies to values settled so far: it has no condition, or its condition holds. */
export function applies(field: ConnectorField, values: ConfigValues): boolean {
  return field.when === undefined || holds(field.when, values)
}

/** Whether a field is a secret, sealed in the vault and never part of the configuration; an account signed in with OAuth is one. */
export function isSecret(field: ConnectorField): boolean {
  return field.kind === 'secret' || field.kind === 'private_key' || field.kind === 'oauth'
}

/** The passphrase field of a private key, which the server makes every private key declare; empty for any other field. */
export function passphraseOf(field: ConnectorField): string {
  return field.passphrase ?? ''
}

/** The value a field is fixed to for values settled so far, such as the anonymous user, or undefined when it is not fixed. */
export function fixedOf(field: ConnectorField, values: ConfigValues): string | undefined {
  return field.fixed?.find((fixed) => holds(fixed.when, values))?.value
}

/** The default of a field for values settled so far: the first dependent default whose condition holds, or its own default. */
export function defaultOf(field: ConnectorField, values: ConfigValues): string {
  return field.defaults?.find((candidate) => holds(candidate.when, values))?.value ?? field.default ?? ''
}

/**
 * Settles values as the server does, in the order the fields are declared: trimmed, fixed values set, defaults filled in, and the settings
 * that do not apply, the secrets and the empty ones left out. Conditions look at the settled values, defaults included.
 */
export function settle(connector: Connector, values: ConfigValues): ConfigValues {
  const out: ConfigValues = {}
  for (const field of connector.fields) {
    if (isSecret(field) || !applies(field, out)) {
      continue
    }
    const value = fixedOf(field, out) ?? (values[field.name] ?? '').trim()
    const settled = value === '' ? defaultOf(field, out) : value
    if (settled !== '') {
      out[field.name] = settled
    }
  }
  return out
}

/** Highest number of characters of a host name, as DNS allows. */
const maxHostLength = 253

/** One label of a host name. */
const labelPattern = /^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$/

/** An IPv4 address. */
const ipv4Pattern = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/

/** Whether a host is an address or a dotted host name, without scheme, port, path or spaces, as the server checks it. */
export function validHost(host: string): boolean {
  if (host === '' || host.length > maxHostLength) {
    return false
  }
  if (ipv4Pattern.test(host) || (host.includes(':') && /^[0-9A-Fa-f:.]+$/.test(host))) {
    return true
  }
  return host.replace(/\.$/, '').split('.').every((label) => labelPattern.test(label))
}

/** Time zones the browser knows. */
const timeZones = new Set(Intl.supportedValuesOf('timeZone'))

/** Why a settled value breaks the schema of its field, or undefined when it does not. */
function check(field: ConnectorField, value: string): FieldReason | undefined {
  switch (field.kind) {
    case 'number': {
      if (!/^-?\d+$/.test(value)) {
        return 'number'
      }
      const n = Number(value)
      return field.max !== undefined && field.max > 0 && (n < (field.min ?? 0) || n > field.max) ? 'range' : undefined
    }
    case 'boolean':
      return value === 'true' || value === 'false' ? undefined : 'boolean'
    case 'choice':
      return field.choices?.includes(value) === true ? undefined : 'choice'
    case 'host':
      return validHost(value) ? undefined : 'host'
    case 'timezone':
      return timeZones.has(value) || value === 'UTC' ? undefined : 'timezone'
    default:
      return field.maxLength !== undefined && field.maxLength > 0 && [...value].length > field.maxLength ? 'length' : undefined
  }
}

/** Checks values against the schema of a connector as the server does, and returns why each failing field fails, by field name. */
export function validate(connector: Connector, values: ConfigValues): Record<string, FieldReason> {
  const settled = settle(connector, values)
  const failures: Record<string, FieldReason> = {}
  for (const field of connector.fields) {
    if (isSecret(field) || !applies(field, settled)) {
      continue
    }
    const value = settled[field.name]
    if (value === undefined) {
      if (field.required) {
        failures[field.name] = 'required'
      }
      continue
    }
    const reason = check(field, value)
    if (reason !== undefined) {
      failures[field.name] = reason
    }
  }
  return failures
}

/**
 * The secrets a connection that keeps nothing needs for values: those a chosen value asks for, such as the password of the password logon
 * type, and that are not the passphrase of a key, which unencrypted keys go without. A secret any value asks for, such as that of a proxy,
 * stays optional.
 */
export function neededSecrets(connector: Connector, values: ConfigValues): string[] {
  const settled = settle(connector, values)
  const passphrases = new Set(connector.fields.map((field) => field.passphrase))
  const asked = (field: ConnectorField) => field.kind === 'oauth' || (field.when?.in !== undefined && field.when.in.length > 0)
  return connector.fields.filter((field) => isSecret(field) && asked(field) && applies(field, settled) && !passphrases.has(field.name)).map((field) => field.name)
}

/** The accounts a site must have connected before it is saved: the OAuth fields that apply, neither connected now nor stored. */
export function unconnected(connector: Connector, values: ConfigValues, secrets: ConfigValues, saved: readonly string[]): string[] {
  const settled = settle(connector, values)
  return connector.fields
    .filter((field) => field.kind === 'oauth' && applies(field, settled) && (secrets[field.name] ?? '') === '' && !saved.includes(field.name))
    .map((field) => field.name)
}

/** Why a secret a connection needs is missing: an account signed in with OAuth must be connected, other secrets typed. */
export function missingReason(connector: Connector, name: string): FieldReason {
  return connector.fields.find((field) => field.name === name)?.kind === 'oauth' ? 'connect' : 'required'
}

/** The configuration sent for values: the settings that apply and were given, trimmed; fixed values and defaults are left to the server. */
export function toConfig(connector: Connector, values: ConfigValues): ConfigValues {
  const settled = settle(connector, values)
  const out: ConfigValues = {}
  for (const field of connector.fields) {
    const value = (values[field.name] ?? '').trim()
    if (!isSecret(field) && settled[field.name] !== undefined && value !== '' && fixedOf(field, settled) === undefined) {
      out[field.name] = value
    }
  }
  return out
}

/** The secrets sent for typed secrets: those of fields that apply to the configuration and were typed; the others keep what is stored. */
export function toSecrets(connector: Connector, values: ConfigValues, secrets: ConfigValues): ConfigValues {
  const settled = settle(connector, values)
  const out: ConfigValues = {}
  for (const field of connector.fields) {
    const value = secrets[field.name] ?? ''
    if (isSecret(field) && applies(field, settled) && value !== '') {
      out[field.name] = value
    }
  }
  return out
}

/** The groups the fields of a connector are on, in the order the form shows them. */
export const fieldGroups: readonly FieldGroup[] = ['connection', 'access', 'advanced', 'charset', 'proxy', 'jump']

/** Whether a connector has fields on a group. */
export function hasGroup(connector: Connector, group: FieldGroup): boolean {
  return connector.fields.some((field) => field.group === group)
}

/** The connector a new site or quick connection starts on: SFTP when it is among the choices, as it always was, otherwise the first one. */
export function startingConnector(choices: readonly Connector[]): string {
  return (choices.find((connector) => connector.type === 'sftp') ?? choices[0])?.type ?? 'sftp'
}
