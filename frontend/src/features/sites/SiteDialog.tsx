import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { FormSelect } from '@/components/ui/form-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { Collection, Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useVaultStatus } from '../vault/use-vault'
import { VaultDialog } from '../vault/VaultDialog'
import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { collectionOptions } from './collection-paths'
import { ConnectionTest } from './ConnectionTest'
import { KeyFileField } from './KeyFileField'
import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import {
  accessModesOf,
  anonymousUser,
  charsetOptions,
  defaultJumpPort,
  defaultPortOf,
  defaultProxyPort,
  effectiveMode,
  ftpEncryptions,
  siteDefaults,
  siteSchema,
  siteTabs,
  tabsOf,
  tabsWithErrors,
  toSiteInput,
  type SiteTab,
  type SiteValues,
} from './site-form'
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

/** Props of {@link Row}. */
interface RowProps {
  /** Identifier of the input the label points at. */
  id: string
  /** Visible label, drawn in the left column. */
  label: string
  /** Translation keys of the validation messages, if any. */
  errors?: ReadonlyArray<string | undefined>
  /** Explanation drawn under the input. */
  hint?: ReactNode
  /** The input, or a group of inputs. */
  children: ReactNode
}

/** One row of the form as FileZilla lays it out: the label on the left, the input and its messages on the right. */
function Row({ id, label, errors = [], hint, children }: RowProps) {
  const { t } = useTranslation('sites')

  return (
    <div className="grid grid-cols-[8.5rem_minmax(0,1fr)] items-start gap-x-4">
      <Label htmlFor={id} className="min-h-8 font-normal text-muted-foreground">
        {label}
      </Label>
      <div className="flex min-w-0 flex-col gap-1.5">
        {children}
        {hint !== undefined && <div className="text-xs leading-relaxed text-muted-foreground">{hint}</div>}
        {errors
          .filter((error) => error !== undefined)
          .map((error) => (
            <p key={error} className="text-xs text-destructive">
              {t(($) => $.errors[error as 'nameRequired' | 'hostRequired' | 'portInvalid' | 'usernameRequired' | 'speedLimitInvalid' | 'remoteDirInvalid'])}
            </p>
          ))}
      </div>
    </div>
  )
}

/** A titled group of rows inside a tab, set apart from the next one by a line. */
function Group({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3 border-b pb-4 last:border-b-0 last:pb-0">
      {title !== undefined && <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>}
      {children}
    </section>
  )
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

/** Time zones the browser knows, offered as suggestions. */
const timeZones = Intl.supportedValuesOf('timeZone')

/** Props of {@link SiteForm}. */
interface SiteFormProps extends Omit<SiteDialogProps, 'target'> {
  /** What to create or edit. */
  target: SiteTarget
}

/** Form with every setting and credential of a site. */
function SiteForm({ target, workspaceId, collections, onClose }: SiteFormProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)
  const editing = target.mode === 'edit' ? target.site : undefined
  const {
    register,
    handleSubmit,
    control,
    formState: { errors, isSubmitting },
  } = useForm<SiteValues>({
    resolver: zodResolver(siteSchema),
    defaultValues: siteDefaults(editing, target.mode === 'create' ? target.collectionId : undefined),
  })
  const protocol = useWatch({ control, name: 'protocol' })
  const ftpEncryption = useWatch({ control, name: 'ftpEncryption' })
  const accessMode = useWatch({ control, name: 'accessMode' })
  const mode = effectiveMode({ protocol, accessMode })
  const hasPassword = editing?.secrets.includes('password') === true
  const hasKey = editing?.secrets.includes('private_key') === true
  const hasJumpKey = editing?.secrets.includes('jump_private_key') === true
  const proxyKind = useWatch({ control, name: 'proxyKind' })
  const jumpHost = useWatch({ control, name: 'jumpHost' })
  const jumpAuthMethod = useWatch({ control, name: 'jumpAuthMethod' })
  const jumpUsesKey = jumpAuthMethod === 'key'
  const secrets = useWatch({ control, name: ['password', 'privateKey', 'passphrase', 'proxyPassword', 'jumpPassword', 'jumpPrivateKey', 'jumpPassphrase'] })
  const vault = useVaultStatus()
  const needsVault = vault !== undefined && !(vault.initialized && vault.unlocked) && secrets.some(Boolean)

  const [tab, setTab] = useState<SiteTab>('general')
  const shownTabs = tabsOf(protocol)
  const errorTabs = tabsWithErrors(errors)

  const [unlocking, setUnlocking] = useState<SiteValues | null>(null)

  const persist = async (values: SiteValues): Promise<void> => {
    setFailure(null)
    try {
      const input = toSiteInput(values)
      await (editing === undefined ? api.sites.create(workspaceId, input) : api.sites.update(workspaceId, editing.id, input))
      await queryClient.invalidateQueries({ queryKey: sitesKeys.sites(workspaceId) })
      onClose()
    } catch (error) {
      const key = siteErrorKeyOf(error)
      if (key === 'vault_locked') {
        setUnlocking(values)
        return
      }
      setFailure(key)
      if (keyFailures.includes(key)) {
        setTab(key.startsWith('jump_') ? 'network' : 'general')
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
      const failing = tabsWithErrors(invalid)
      if (!failing.has(tab)) {
        setTab(siteTabs.find((candidate) => failing.has(candidate)) as SiteTab)
      }
    },
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
              {t(($) => $.site.tabs[candidate])}
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
            <Row id="site-name" label={t(($) => $.site.name)} errors={[errors.name?.message]}>
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
          </Group>
          <Group>
            <Row id="site-protocol" label={t(($) => $.site.protocol)}>
              <FormSelect
                id="site-protocol"
                control={control}
                name="protocol"
                options={[
                  { value: 'sftp', label: t(($) => $.site.protocols.sftp) },
                  { value: 'ftp', label: t(($) => $.site.protocols.ftp) },
                ]}
              />
            </Row>
            <Row id="site-host" label={t(($) => $.site.host)} errors={[errors.host?.message, errors.port?.message]}>
              <div className="flex items-center gap-3">
                <Input id="site-host" className="min-w-0 flex-1 font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.host !== undefined} {...register('host')} />
                <Label htmlFor="site-port" className="font-normal text-muted-foreground">
                  {t(($) => $.site.port)}
                </Label>
                <Input id="site-port" className="w-20 font-mono" inputMode="numeric" placeholder={String(defaultPortOf(protocol, ftpEncryption))} aria-invalid={errors.port !== undefined} {...register('port')} />
              </div>
            </Row>
            {protocol === 'ftp' && (
              <Row
                id="site-encryption"
                label={t(($) => $.site.encryption)}
                hint={
                  ftpEncryption === 'plain' || ftpEncryption === 'optional' ? (
                    <p role="note" className="rounded-md bg-warn/10 px-3 py-2 text-foreground">
                      {t(($) => $.site.encryptionHints[ftpEncryption])}
                    </p>
                  ) : (
                    t(($) => $.site.encryptionHints[ftpEncryption])
                  )
                }
              >
                <FormSelect
                  id="site-encryption"
                  control={control}
                  name="ftpEncryption"
                  options={ftpEncryptions.map((candidate) => ({ value: candidate, label: t(($) => $.site.encryptions[candidate]) }))}
                />
              </Row>
            )}
          </Group>
          <Group>
            <Row id="site-access" label={t(($) => $.site.accessMode)} hint={mode === 'anonymous' ? t(($) => $.site.anonymousHint) : mode === 'ask' ? t(($) => $.site.askHint) : undefined}>
              <FormSelect
                id="site-access"
                control={control}
                name="accessMode"
                value={mode}
                options={accessModesOf(protocol).map((candidate) => ({ value: candidate, label: t(($) => $.site.accessModes[candidate]) }))}
              />
            </Row>
            {mode === 'anonymous' ? (
              <Row id="site-username" label={t(($) => $.site.username)}>
                <Input key="anonymous" id="site-username" className="font-mono" value={anonymousUser} readOnly disabled />
              </Row>
            ) : (
              <Row id="site-username" label={t(($) => $.site.username)} errors={[errors.username?.message]}>
                <Input key="username" id="site-username" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.username !== undefined} {...register('username')} />
              </Row>
            )}
            {mode === 'normal' && (
              <Row id="site-password" label={t(($) => $.site.password)}>
                <Input id="site-password" type="password" autoComplete="off" placeholder={hasPassword ? t(($) => $.site.keepPassword) : undefined} {...register('password')} />
              </Row>
            )}
            {mode === 'key' && (
              <>
                <Row id="site-key" label={t(($) => $.site.privateKey)}>
                  <KeyFileField control={control} name="privateKey" passphraseName="passphrase" id="site-key" passphraseLabel={t(($) => $.site.passphrase)} saved={hasKey} />
                </Row>
              </>
            )}
          </Group>
        </TabsContent>
        <TabsContent value="advanced" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
          <Group>
            <Row id="site-timezone" label={t(($) => $.site.timezone)} hint={t(($) => $.site.timezoneHint)}>
              <Input id="site-timezone" className="font-mono" list="site-timezones" autoComplete="off" {...register('listTimezone')} />
              <datalist id="site-timezones">
                {timeZones.map((zone) => (
                  <option key={zone} value={zone} />
                ))}
              </datalist>
            </Row>
            <Row id="site-remote-dir" label={t(($) => $.site.remoteDir)} errors={[errors.remoteDir?.message]} hint={t(($) => $.site.remoteDirHint)}>
              <Input id="site-remote-dir" className="font-mono" autoComplete="off" spellCheck={false} placeholder="/" aria-invalid={errors.remoteDir !== undefined} {...register('remoteDir')} />
            </Row>
          </Group>
        </TabsContent>
        <TabsContent value="transfer" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
          <Group>
            <Row id="site-speed-limit" label={t(($) => $.site.speedLimit)} errors={[errors.speedLimit?.message]} hint={t(($) => $.site.speedLimitHint)}>
              <Input id="site-speed-limit" className="max-w-40 font-mono" inputMode="decimal" placeholder={t(($) => $.site.speedLimitUnlimited)} aria-invalid={errors.speedLimit !== undefined} {...register('speedLimit')} />
            </Row>
          </Group>
        </TabsContent>
        {protocol !== 'sftp' && (
          <TabsContent value="charset" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
            <Group>
              <Row id="site-charset" label={t(($) => $.site.charset)} hint={t(($) => $.site.charsetHint)}>
                <FormSelect id="site-charset" control={control} name="charset" options={charsetOptions} />
              </Row>
            </Group>
          </TabsContent>
        )}
        <TabsContent value="network" keepMounted className="flex min-h-96 flex-col gap-4 pt-3">
          <Group title={t(($) => $.site.proxyTitle)}>
            <Row id="site-proxy-kind" label={t(($) => $.site.proxyKind)} hint={t(($) => $.site.proxyHint)}>
              <FormSelect
                id="site-proxy-kind"
                control={control}
                name="proxyKind"
                options={[
                  { value: 'none', label: t(($) => $.site.proxyNone) },
                  { value: 'socks5', label: 'SOCKS5' },
                  { value: 'http', label: t(($) => $.site.proxyHttp) },
                ]}
              />
            </Row>
            {proxyKind !== 'none' && (
              <>
                <Row id="site-proxy-host" label={t(($) => $.site.proxyHost)} errors={[errors.proxyHost?.message, errors.proxyPort?.message]}>
                  <div className="flex items-center gap-3">
                    <Input id="site-proxy-host" className="min-w-0 flex-1 font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.proxyHost !== undefined} {...register('proxyHost')} />
                    <Label htmlFor="site-proxy-port" className="font-normal text-muted-foreground">
                      {t(($) => $.site.proxyPort)}
                    </Label>
                    <Input id="site-proxy-port" className="w-20 font-mono" inputMode="numeric" placeholder={String(defaultProxyPort)} aria-invalid={errors.proxyPort !== undefined} {...register('proxyPort')} />
                  </div>
                </Row>
                <Row id="site-proxy-username" label={t(($) => $.site.proxyUsername)}>
                  <Input id="site-proxy-username" className="font-mono" autoComplete="off" spellCheck={false} {...register('proxyUsername')} />
                </Row>
                <Row id="site-proxy-password" label={t(($) => $.site.proxyPassword)}>
                  <Input id="site-proxy-password" type="password" autoComplete="off" {...register('proxyPassword')} />
                </Row>
              </>
            )}
          </Group>
          {protocol === 'sftp' && (
            <Group title={t(($) => $.site.jumpTitle)}>
              <Row id="site-jump-host" label={t(($) => $.site.jumpHost)} errors={[errors.jumpPort?.message]} hint={t(($) => $.site.jumpHint)}>
                <div className="flex items-center gap-3">
                  <Input id="site-jump-host" className="min-w-0 flex-1 font-mono" autoComplete="off" spellCheck={false} placeholder={t(($) => $.site.jumpNone)} {...register('jumpHost')} />
                  <Label htmlFor="site-jump-port" className="font-normal text-muted-foreground">
                    {t(($) => $.site.jumpPort)}
                  </Label>
                  <Input id="site-jump-port" className="w-20 font-mono" inputMode="numeric" placeholder={String(defaultJumpPort)} aria-invalid={errors.jumpPort !== undefined} {...register('jumpPort')} />
                </div>
              </Row>
              {jumpHost !== '' && (
                <>
                  <Row id="site-jump-auth" label={t(($) => $.site.jumpAuthMethod)}>
                    <FormSelect
                      id="site-jump-auth"
                      control={control}
                      name="jumpAuthMethod"
                      options={[
                        { value: 'password', label: t(($) => $.site.accessModes.normal) },
                        { value: 'key', label: t(($) => $.site.accessModes.key) },
                      ]}
                    />
                  </Row>
                  <Row id="site-jump-username" label={t(($) => $.site.jumpUsername)} errors={[errors.jumpUsername?.message]}>
                    <Input id="site-jump-username" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.jumpUsername !== undefined} {...register('jumpUsername')} />
                  </Row>
                  {jumpUsesKey ? (
                    <>
                      <Row id="site-jump-key" label={t(($) => $.site.jumpPrivateKey)}>
                        <KeyFileField
                          control={control}
                          name="jumpPrivateKey"
                          passphraseName="jumpPassphrase"
                          id="site-jump-key"
                          passphraseLabel={t(($) => $.site.jumpPassphrase)}
                          saved={hasJumpKey}
                        />
                      </Row>
                    </>
                  ) : (
                    <Row id="site-jump-password" label={t(($) => $.site.jumpPassword)}>
                      <Input id="site-jump-password" type="password" autoComplete="off" {...register('jumpPassword')} />
                    </Row>
                  )}
                </>
              )}
            </Group>
          )}
        </TabsContent>
      </Tabs>
      {editing !== undefined && <ConnectionTest workspaceId={workspaceId} site={editing} />}
      <VaultDialog
        mode={unlocking === null ? null : vault?.initialized === false ? 'setup' : 'unlock'}
        target={unlocking === null ? undefined : { name: unlocking.name, host: unlocking.host, username: unlocking.username }}
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
          <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {t(($) => $.errors[failure])}
          </p>
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

/** Dialog to create a site or to edit the settings and credentials of a saved one. */
export function SiteDialog({ target, ...form }: SiteDialogProps) {
  return (
    <Dialog open={target !== null} onOpenChange={form.onClose}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-[680px]">{target !== null && <SiteForm target={target} {...form} />}</DialogContent>
    </Dialog>
  )
}
