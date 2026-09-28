import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import type { Collection, Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useVaultStatus } from '../vault/use-vault'
import { VaultDialog } from '../vault/VaultDialog'
import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { collectionOptions } from './collection-paths'
import { ConnectionTest } from './ConnectionTest'
import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { charsetOptions, defaultJumpPort, defaultPorts, defaultProxyPort, siteDefaults, siteSchema, siteTabs, tabsWithErrors, toSiteInput, type SiteTab, type SiteValues } from './site-form'
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

/** Props of {@link Field}. */
interface FieldProps {
  /** Identifier of the input the label points at. */
  id: string
  /** Visible label. */
  label: string
  /** Translation key of the validation message, if any. */
  error?: string
  /** Extra classes of the wrapper. */
  className: string
  /** The input. */
  children: ReactNode
}

/** Label, input and validation message of one form field. */
function Field({ id, label, error, className, children }: FieldProps) {
  const { t } = useTranslation('sites')

  return (
    <div className={`flex flex-col gap-1.5 ${className}`}>
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error !== undefined && <p className="text-xs text-destructive">{t(($) => $.errors[error as 'nameRequired' | 'hostRequired' | 'portInvalid' | 'usernameRequired' | 'speedLimitInvalid'])}</p>}
    </div>
  )
}

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
  const method = useWatch({ control, name: 'authMethod' })
  const usesKey = protocol === 'sftp' && method === 'key'
  const proxyKind = useWatch({ control, name: 'proxyKind' })
  const jumpHost = useWatch({ control, name: 'jumpHost' })
  const jumpAuthMethod = useWatch({ control, name: 'jumpAuthMethod' })
  const jumpUsesKey = jumpAuthMethod === 'key'
  const secrets = useWatch({ control, name: ['password', 'privateKey', 'passphrase', 'proxyPassword', 'jumpPassword', 'jumpPrivateKey', 'jumpPassphrase'] })
  const vault = useVaultStatus()
  const needsVault = vault !== undefined && !(vault.initialized && vault.unlocked) && secrets.some(Boolean)

  const [tab, setTab] = useState<SiteTab>('general')
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
      } else {
        setFailure(key)
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
        <TabsList variant="line" className="justify-start border-b pb-1">
          {siteTabs.map((candidate) => (
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
        <TabsContent value="general" keepMounted className="flex min-h-80 flex-col gap-4 pt-2">
          <div className="flex gap-3">
            <Field id="site-name" label={t(($) => $.site.name)} error={errors.name?.message} className="flex-1">
              <Input id="site-name" autoComplete="off" aria-invalid={errors.name !== undefined} {...register('name')} />
            </Field>
            <Field id="site-collection" label={t(($) => $.site.collection)} className="flex-1">
              <NativeSelect id="site-collection" {...register('collectionId')}>
                <option value="">{t(($) => $.site.noCollection)}</option>
                {collectionOptions(collections).map((option) => (
                  <option key={option.id} value={option.id}>
                    {option.label}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          </div>
          <div className="flex gap-3">
            <Field id="site-protocol" label={t(($) => $.site.protocol)} className="w-28">
              <NativeSelect id="site-protocol" {...register('protocol')}>
                <option value="sftp">SFTP</option>
                <option value="ftps">FTPS</option>
                <option value="ftp">FTP</option>
              </NativeSelect>
            </Field>
            <Field id="site-host" label={t(($) => $.site.host)} error={errors.host?.message} className="flex-1">
              <Input id="site-host" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.host !== undefined} {...register('host')} />
            </Field>
            <Field id="site-port" label={t(($) => $.site.port)} error={errors.port?.message} className="w-24">
              <Input id="site-port" className="font-mono" inputMode="numeric" placeholder={String(defaultPorts[protocol])} aria-invalid={errors.port !== undefined} {...register('port')} />
            </Field>
          </div>
          <div className="flex gap-3">
            <Field id="site-username" label={t(($) => $.site.username)} error={errors.username?.message} className="flex-1">
              <Input id="site-username" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.username !== undefined} {...register('username')} />
            </Field>
            {protocol === 'sftp' ? (
              <Field id="site-auth" label={t(($) => $.site.authMethod)} className="w-44">
                <NativeSelect id="site-auth" {...register('authMethod')}>
                  <option value="password">{t(($) => $.site.password)}</option>
                  <option value="key">{t(($) => $.site.key)}</option>
                </NativeSelect>
              </Field>
            ) : null}
          </div>
          {protocol === 'ftp' ? (
            <p role="note" className="rounded-md bg-warn/10 px-3 py-2 text-xs">
              {t(($) => $.site.ftpWarning)}
            </p>
          ) : null}
          {protocol === 'ftps' ? <p className="text-xs text-muted-foreground">{t(($) => $.site.ftpsHint)}</p> : null}
          {usesKey ? (
            <>
              <Field id="site-key" label={t(($) => $.site.privateKey)} className="">
                <Textarea id="site-key" className="font-mono" rows={4} spellCheck={false} autoComplete="off" {...register('privateKey')} />
              </Field>
              <Field id="site-passphrase" label={t(($) => $.site.passphrase)} className="">
                <Input id="site-passphrase" type="password" autoComplete="off" {...register('passphrase')} />
              </Field>
            </>
          ) : (
            <Field id="site-password" label={t(($) => $.site.password)} className="">
              <Input id="site-password" type="password" autoComplete="off" {...register('password')} />
            </Field>
          )}
          {editing !== undefined && <p className="text-xs text-muted-foreground">{t(($) => $.site.secretHint)}</p>}
        </TabsContent>
        <TabsContent value="advanced" keepMounted className="flex min-h-80 flex-col gap-4 pt-2">
          {protocol === 'sftp' ? null : (
            <Field id="site-charset" label={t(($) => $.site.charset)} className="">
              <NativeSelect id="site-charset" {...register('charset')}>
                {charsetOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </NativeSelect>
              <p className="text-xs text-muted-foreground">{t(($) => $.site.charsetHint)}</p>
            </Field>
          )}
          <Field id="site-timezone" label={t(($) => $.site.timezone)} className="">
            <Input id="site-timezone" className="font-mono" list="site-timezones" autoComplete="off" {...register('listTimezone')} />
            <datalist id="site-timezones">
              {timeZones.map((zone) => (
                <option key={zone} value={zone} />
              ))}
            </datalist>
          </Field>
          <Field id="site-speed-limit" label={t(($) => $.site.speedLimit)} error={errors.speedLimit?.message} className="">
            <Input id="site-speed-limit" className="font-mono" inputMode="decimal" placeholder={t(($) => $.site.speedLimitUnlimited)} aria-invalid={errors.speedLimit !== undefined} {...register('speedLimit')} />
            <p className="text-xs text-muted-foreground">{t(($) => $.site.speedLimitHint)}</p>
          </Field>
        </TabsContent>
        <TabsContent value="network" keepMounted className="flex min-h-80 flex-col gap-4 pt-2">
          <div className="flex flex-col gap-3 rounded-md border p-3">
            <Field id="site-proxy-kind" label={t(($) => $.site.proxyKind)} className="w-40">
              <NativeSelect id="site-proxy-kind" {...register('proxyKind')}>
                <option value="none">{t(($) => $.site.proxyNone)}</option>
                <option value="socks5">SOCKS5</option>
                <option value="http">{t(($) => $.site.proxyHttp)}</option>
              </NativeSelect>
            </Field>
            {proxyKind !== 'none' && (
              <>
                <div className="flex gap-3">
                  <Field id="site-proxy-host" label={t(($) => $.site.proxyHost)} error={errors.proxyHost?.message} className="flex-1">
                    <Input id="site-proxy-host" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.proxyHost !== undefined} {...register('proxyHost')} />
                  </Field>
                  <Field id="site-proxy-port" label={t(($) => $.site.proxyPort)} error={errors.proxyPort?.message} className="w-24">
                    <Input id="site-proxy-port" className="font-mono" inputMode="numeric" placeholder={String(defaultProxyPort)} aria-invalid={errors.proxyPort !== undefined} {...register('proxyPort')} />
                  </Field>
                </div>
                <div className="flex gap-3">
                  <Field id="site-proxy-username" label={t(($) => $.site.proxyUsername)} className="flex-1">
                    <Input id="site-proxy-username" className="font-mono" autoComplete="off" spellCheck={false} {...register('proxyUsername')} />
                  </Field>
                  <Field id="site-proxy-password" label={t(($) => $.site.proxyPassword)} className="flex-1">
                    <Input id="site-proxy-password" type="password" autoComplete="off" {...register('proxyPassword')} />
                  </Field>
                </div>
              </>
            )}
            <p className="text-xs text-muted-foreground">{t(($) => $.site.proxyHint)}</p>
          </div>
          {protocol === 'sftp' && (
            <div className="flex flex-col gap-3 rounded-md border p-3">
              <p className="text-sm font-medium">{t(($) => $.site.jumpTitle)}</p>
              <div className="flex gap-3">
                <Field id="site-jump-host" label={t(($) => $.site.jumpHost)} className="flex-1">
                  <Input id="site-jump-host" className="font-mono" autoComplete="off" spellCheck={false} placeholder={t(($) => $.site.jumpNone)} {...register('jumpHost')} />
                </Field>
                <Field id="site-jump-port" label={t(($) => $.site.jumpPort)} error={errors.jumpPort?.message} className="w-24">
                  <Input id="site-jump-port" className="font-mono" inputMode="numeric" placeholder={String(defaultJumpPort)} aria-invalid={errors.jumpPort !== undefined} {...register('jumpPort')} />
                </Field>
              </div>
              {jumpHost !== '' && (
                <>
                  <div className="flex gap-3">
                    <Field id="site-jump-username" label={t(($) => $.site.jumpUsername)} error={errors.jumpUsername?.message} className="flex-1">
                      <Input id="site-jump-username" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.jumpUsername !== undefined} {...register('jumpUsername')} />
                    </Field>
                    <Field id="site-jump-auth" label={t(($) => $.site.jumpAuthMethod)} className="w-44">
                      <NativeSelect id="site-jump-auth" {...register('jumpAuthMethod')}>
                        <option value="password">{t(($) => $.site.password)}</option>
                        <option value="key">{t(($) => $.site.key)}</option>
                      </NativeSelect>
                    </Field>
                  </div>
                  {jumpUsesKey ? (
                    <>
                      <Field id="site-jump-key" label={t(($) => $.site.jumpPrivateKey)} className="">
                        <Textarea id="site-jump-key" className="font-mono" rows={4} spellCheck={false} autoComplete="off" {...register('jumpPrivateKey')} />
                      </Field>
                      <Field id="site-jump-passphrase" label={t(($) => $.site.jumpPassphrase)} className="">
                        <Input id="site-jump-passphrase" type="password" autoComplete="off" {...register('jumpPassphrase')} />
                      </Field>
                    </>
                  ) : (
                    <Field id="site-jump-password" label={t(($) => $.site.jumpPassword)} className="">
                      <Input id="site-jump-password" type="password" autoComplete="off" {...register('jumpPassword')} />
                    </Field>
                  )}
                </>
              )}
              <p className="text-xs text-muted-foreground">{t(($) => $.site.jumpHint)}</p>
            </div>
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
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-[560px]">{target !== null && <SiteForm target={target} {...form} />}</DialogContent>
    </Dialog>
  )
}
