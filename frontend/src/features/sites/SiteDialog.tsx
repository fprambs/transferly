import { useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { FormSelect } from '@/components/ui/form-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { Collection, Connector, Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import { Group, Row } from '../connectors/FormRows'
import { SchemaFields } from '../connectors/SchemaFields'
import { startingConnector, type FieldGroup } from '../connectors/schema'
import { hostOf, usernameOf } from '../connectors/settings'
import { useConnectorTexts } from '../connectors/use-connector-texts'
import { useConnectors } from '../connectors/use-connectors'
import { useVaultStatus } from '../vault/use-vault'
import { VaultDialog } from '../vault/VaultDialog'
import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { collectionOptions } from './collection-paths'
import { ColorPicker } from './ColorPicker'
import { ConnectionTest } from './ConnectionTest'
import { configFailureOf, fieldOf, siteErrorKeyOf, type SiteErrorKey } from './errors'
import { connectorChoices, siteDefaults, siteResolver, tabLabelOf, tabOfField, tabsOf, tabsWithErrors, toSiteInput, type SiteTab, type SiteValues } from './site-form'
import { sitesKeys } from './use-sites-data'

/** What the site dialog does. */
export type SiteTarget = { mode: 'create'; collectionId?: string } | { mode: 'edit'; site: Site }

/** Props of {@link SiteDialog}. */
export interface SiteDialogProps {
  /** What to create or edit; the dialog is closed when there is none. */
  target: SiteTarget | null
  /** Workspace the site belongs to. */
  workspaceId: string
  /** Every collection of the workspace, to file the site in one. */
  collections: readonly Collection[]
  /** Called when the dialog is dismissed or the work is done. */
  onClose: () => void
}

/** Failures of a private key the server refused, which send the form to the tab of that key. */
const keyFailures: readonly SiteErrorKey[] = [
  'private_key_is_public',
  'passphrase_required',
  'wrong_passphrase',
  'private_key_unreadable',
  'private_key_unsupported',
  'jump_private_key_is_public',
  'jump_passphrase_required',
  'jump_wrong_passphrase',
  'jump_private_key_unreadable',
  'jump_private_key_unsupported',
]

/** The groups of fields of a connector drawn on each tab. */
const tabGroups: Record<SiteTab, readonly FieldGroup[]> = {
  general: ['connection', 'access'],
  advanced: ['advanced'],
  transfer: [],
  charset: ['charset'],
  network: ['proxy', 'jump'],
}

/** Props of {@link SiteForm}. */
interface SiteFormProps extends Omit<SiteDialogProps, 'target'> {
  /** What to create or edit. */
  target: SiteTarget
  /** The connectors of the catalog. */
  connectors: readonly Connector[]
}

/** Form with every setting and credential of a site, the settings of its connector drawn from the connector's schema. */
function SiteForm({ target, workspaceId, collections, connectors, onClose }: SiteFormProps) {
  const { t } = useTranslation('sites')
  const texts = useConnectorTexts()
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)
  const editing = target.mode === 'edit' ? target.site : undefined
  const choices = connectorChoices(connectors, editing?.connector)
  const resolver = useMemo(() => siteResolver(connectors), [connectors])
  const {
    register,
    handleSubmit,
    control,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<SiteValues>({
    resolver,
    defaultValues: siteDefaults(editing, target.mode === 'create' ? target.collectionId : undefined, startingConnector(choices)),
  })
  const type = useWatch({ control, name: 'connector' })
  const connector = connectors.find((candidate) => candidate.type === type)
  const secrets = useWatch({ control, name: 'secrets' })
  const vault = useVaultStatus()
  const needsVault = vault !== undefined && !(vault.initialized && vault.unlocked) && Object.values(secrets).some(Boolean)

  const [chosenTab, setTab] = useState<SiteTab>('general')
  const shownTabs = tabsOf(connector)
  const tab = shownTabs.includes(chosenTab) ? chosenTab : 'general'
  const errorTabs = tabsWithErrors(errors, connector)

  const [unlocking, setUnlocking] = useState<SiteValues | null>(null)

  const persist = async (values: SiteValues): Promise<void> => {
    setFailure(null)
    const chosen = connectors.find((candidate) => candidate.type === values.connector) as Connector
    try {
      const input = toSiteInput(values, chosen)
      await (editing === undefined ? api.sites.create(workspaceId, input) : api.sites.update(workspaceId, editing.id, input))
      await queryClient.invalidateQueries({ queryKey: sitesKeys.sites(workspaceId) })
      onClose()
    } catch (error) {
      const key = siteErrorKeyOf(error)
      if (key === 'vault_locked') {
        setUnlocking(values)
        return
      }
      const refused = configFailureOf(error)
      if (refused !== undefined) {
        setError(`config.${refused.field}`, { type: 'server', message: refused.reason })
        setTab(tabOfField(chosen, refused.field))
        return
      }
      const field = fieldOf(error)
      setFailure(key)
      if (keyFailures.includes(key) && field !== undefined) {
        setTab(tabOfField(chosen, field))
      }
    }
  }

  const submit = handleSubmit(
    async (values) => {
      if (needsVault) {
        setUnlocking(values)
        return
      }
      await persist(values)
    },
    (invalid) => {
      const failing = tabsWithErrors(invalid, connector)
      if (!failing.has(tab)) {
        setTab(shownTabs.find((candidate) => failing.has(candidate)) as SiteTab)
      }
    },
  )

  const siteError = (message: string | undefined) => (message === undefined ? undefined : t(($) => $.errors[message as 'nameRequired' | 'speedLimitInvalid' | 'remoteDirInvalid']))
  const schema = (groups: readonly FieldGroup[]) =>
    connector !== undefined && (
      <SchemaFields connector={connector} groups={groups} control={control} register={register} errors={errors} saved={editing?.secrets} idPrefix="site" />
    )

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{editing === undefined ? t(($) => $.site.createTitle) : t(($) => $.site.editTitle)}</DialogTitle>
      </DialogHeader>
      <Tabs value={tab} onValueChange={(value) => setTab(value as SiteTab)}>
        <TabsList variant="line" className="w-full justify-start overflow-x-auto border-b pb-1">
          {shownTabs.map((candidate) => (
            <TabsTrigger key={candidate} value={candidate} className="flex-none px-3">
              {t(($) => $.site.tabs[tabLabelOf(candidate, connector)])}
              {errorTabs.has(candidate) && (
                <>
                  <span aria-hidden="true" className="size-1.5 rounded-full bg-destructive" />
                  <span className="sr-only">{t(($) => $.site.tabs.hasErrors)}</span>
                </>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="general" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
          <Group>
            <Row id="site-name" label={t(($) => $.site.name)} errors={[siteError(errors.name?.message)]}>
              <Input id="site-name" autoComplete="off" aria-invalid={errors.name !== undefined} {...register('name')} />
            </Row>
            <Row id="site-collection" label={t(($) => $.site.collection)}>
              <FormSelect
                id="site-collection"
                control={control}
                name="collectionId"
                options={[{ value: '', label: t(($) => $.site.noCollection) }, ...collectionOptions(collections).map((option) => ({ value: option.id, label: option.label }))]}
              />
            </Row>
            <Row id="site-color" label={t(($) => $.site.color)} hint={t(($) => $.site.colorHint)}>
              <Controller
                control={control}
                name="color"
                render={({ field }) => <ColorPicker id="site-color" label={t(($) => $.site.color)} value={field.value} onChange={field.onChange} />}
              />
            </Row>
            <Row id="site-connector" label={texts.connectorLabel}>
              <FormSelect id="site-connector" control={control} name="connector" options={choices.map((choice) => ({ value: choice.type, label: texts.connector(choice) }))} />
            </Row>
          </Group>
          {schema(tabGroups.general)}
        </TabsContent>
        <TabsContent value="advanced" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
          {schema(tabGroups.advanced)}
          <Group>
            <Row id="site-remote-dir" label={t(($) => $.site.remoteDir)} errors={[siteError(errors.remoteDir?.message)]} hint={t(($) => $.site.remoteDirHint)}>
              <Input id="site-remote-dir" className="font-mono" autoComplete="off" spellCheck={false} placeholder="/" aria-invalid={errors.remoteDir !== undefined} {...register('remoteDir')} />
            </Row>
          </Group>
        </TabsContent>
        <TabsContent value="transfer" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
          <Group>
            <Row id="site-speed-limit" label={t(($) => $.site.speedLimit)} errors={[siteError(errors.speedLimit?.message)]} hint={t(($) => $.site.speedLimitHint)}>
              <Input id="site-speed-limit" className="max-w-40 font-mono" inputMode="decimal" placeholder={t(($) => $.site.speedLimitUnlimited)} aria-invalid={errors.speedLimit !== undefined} {...register('speedLimit')} />
            </Row>
          </Group>
        </TabsContent>
        {shownTabs.includes('charset') && (
          <TabsContent value="charset" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
            {schema(tabGroups.charset)}
          </TabsContent>
        )}
        {shownTabs.includes('network') && (
          <TabsContent value="network" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
            {schema(tabGroups.network)}
          </TabsContent>
        )}
      </Tabs>
      {editing !== undefined && <ConnectionTest workspaceId={workspaceId} site={editing} />}
      <VaultDialog
        mode={unlocking === null ? null : vault?.initialized === false ? 'setup' : 'unlock'}
        target={unlocking === null ? undefined : { name: unlocking.name, host: hostOf(unlocking), username: usernameOf(unlocking) }}
        onClose={() => {
          setUnlocking(null)
          setFailure('vault_locked')
        }}
        onUnlocked={() => void persist(unlocking as SiteValues)}
      />
      {needsVault || failure === 'vault_locked' ? (
        <VaultLockedNotice need="save" onUnlocked={() => setFailure(null)} />
      ) : (
        failure !== null && (
          <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
            <p>{t(($) => $.errors[failure])}</p>
            <ErrorGuide failure={failure} className="mt-2" />
          </div>
        )
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.site.cancel)}
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {editing === undefined ? t(($) => $.site.create) : t(($) => $.site.save)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Dialog to create a site or to edit the settings and credentials of a saved one; it waits for the catalog of connectors. */
export function SiteDialog({ target, ...form }: SiteDialogProps) {
  const { data: connectors } = useConnectors()
  return (
    <Dialog open={target !== null} onOpenChange={form.onClose}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-[680px]">
        {target !== null && connectors !== undefined && <SiteForm target={target} connectors={connectors} {...form} />}
      </DialogContent>
    </Dialog>
  )
}
