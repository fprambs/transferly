import { zodResolver } from '@hookform/resolvers/zod'
import type { FieldErrors, Resolver } from 'react-hook-form'
import { z } from 'zod'

import type { Connector, Site, SiteInput } from '@/lib/api'

import { toConfig, toSecrets, validate, type FieldGroup } from '../connectors/schema'

/** Bytes in a mebibyte, the unit the site's speed limit is shown in. */
const mebibyte = 1024 * 1024

/**
 * The connectors a site may choose: those the instance offers (ADR-0019), every one of the catalog while that is not known, and always the
 * one an edited site already uses, so its form still shows it.
 */
export function connectorChoices(connectors: readonly Connector[], current?: string): Connector[] {
  return connectors.filter((connector) => connector.enabled || connector.type === current)
}

/** Validation of the settings every site has; the configuration of its connector follows the connector's schema. Messages are translation keys. */
export const siteSchema = z.object({
  name: z.string().trim().min(1, 'nameRequired'),
  collectionId: z.string(),
  color: z.string(),
  connector: z.string().min(1),
  remoteDir: z.string().trim().refine((value) => value === '' || value.startsWith('/'), 'remoteDirInvalid'),
  speedLimit: z.string().trim().refine((value) => value === '' || (/^\d+(\.\d+)?$/.test(value) && Number(value) > 0), 'speedLimitInvalid'),
  config: z.custom<Record<string, string>>(),
  secrets: z.custom<Record<string, string>>(),
})

/** Values of the site form: what every site has, the settings of its connector and the secrets typed, both by field name. */
export type SiteValues = z.infer<typeof siteSchema>

/**
 * A resolver that checks the settings every site has with {@link siteSchema} and the settings of the connector with its own schema; a
 * connector missing from the catalog has its settings left to the server.
 */
export function siteResolver(connectors: readonly Connector[]): Resolver<SiteValues> {
  const base = zodResolver(siteSchema)
  return async (values, context, options) => {
    const result = await base(values, context, options)
    const connector = connectors.find((candidate) => candidate.type === values.connector)
    const failures = connector === undefined ? {} : validate(connector, values.config)
    if (Object.keys(failures).length === 0) {
      return result
    }
    const config = Object.fromEntries(Object.entries(failures).map(([name, reason]) => [name, { type: 'schema', message: reason }]))
    return { values: {}, errors: { ...result.errors, config } as FieldErrors<SiteValues> }
  }
}

/** Initial values of the form for a new site of a connector, or for an existing one; secrets are never prefilled. */
export function siteDefaults(site: Site | undefined, collectionId: string | undefined, connector: string): SiteValues {
  return {
    name: site?.name ?? '',
    collectionId: site?.collectionId ?? collectionId ?? '',
    color: site?.color ?? '',
    connector: site?.connector ?? connector,
    remoteDir: site?.remoteDir ?? '',
    speedLimit: site?.speedLimitBps ? String(site.speedLimitBps / mebibyte) : '',
    config: { ...site?.config },
    secrets: {},
  }
}

/** Builds the request body of a site: the settings of its connector that apply and were given, and the secrets typed for fields that apply. */
export function toSiteInput(values: SiteValues, connector: Connector): SiteInput {
  return {
    name: values.name.trim(),
    collectionId: values.collectionId === '' ? undefined : values.collectionId,
    connector: values.connector,
    config: toConfig(connector, values.config),
    secrets: toSecrets(connector, values.config, values.secrets),
    remoteDir: values.remoteDir,
    color: values.color,
    speedLimitBps: values.speedLimit.trim() === '' ? 0 : Math.round(Number(values.speedLimit) * mebibyte),
  }
}

/** The request body that saves a site as it is, to change one of its settings; its secrets stay as they are stored. */
export function inputOf(site: Site): SiteInput {
  return {
    name: site.name,
    collectionId: site.collectionId,
    connector: site.connector,
    config: { ...site.config },
    remoteDir: site.remoteDir ?? '',
    color: site.color ?? '',
    speedLimitBps: site.speedLimitBps,
  }
}

/** The tabs of the site form, as FileZilla splits it: what is needed to connect, advanced settings, the speed limit, the character set of file
 * names, and how the connection is routed. */
export type SiteTab = 'general' | 'advanced' | 'transfer' | 'charset' | 'network'

/** The tabs in the order they are drawn. */
export const siteTabs: readonly SiteTab[] = ['general', 'advanced', 'transfer', 'charset', 'network']

/** The tab each group of fields of a connector is on. */
export const groupTabs: Record<FieldGroup, SiteTab> = {
  connection: 'general',
  access: 'general',
  advanced: 'advanced',
  charset: 'charset',
  proxy: 'network',
  jump: 'network',
}

/** The tabs a connector shows: the general, advanced and transfer tabs always, and the others when it has fields on them. */
export function tabsOf(connector: Connector | undefined): readonly SiteTab[] {
  const used = new Set((connector?.fields ?? []).map((field) => groupTabs[field.group]))
  return siteTabs.filter((tab) => tab === 'general' || tab === 'advanced' || tab === 'transfer' || used.has(tab))
}

/** The tab each setting every site has is on. */
const siteFieldTabs: Record<string, SiteTab> = { name: 'general', collectionId: 'general', color: 'general', connector: 'general', remoteDir: 'advanced', speedLimit: 'transfer' }

/** The tab a field of a connector is on, by its name; an unknown one is on the general tab. */
export function tabOfField(connector: Connector | undefined, name: string): SiteTab {
  const field = connector?.fields.find((candidate) => candidate.name === name)
  return field === undefined ? 'general' : groupTabs[field.group]
}

/** The tabs that hold at least one field with a validation error. */
export function tabsWithErrors(errors: FieldErrors<SiteValues>, connector: Connector | undefined): Set<SiteTab> {
  const tabs = new Set<SiteTab>()
  for (const name of Object.keys(errors)) {
    if (name === 'config' || name === 'secrets') {
      for (const field of Object.keys(errors[name] ?? {})) {
        tabs.add(tabOfField(connector, field))
      }
    } else {
      tabs.add(siteFieldTabs[name] ?? 'general')
    }
  }
  return tabs
}
