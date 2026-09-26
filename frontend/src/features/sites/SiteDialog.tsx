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
import { Textarea } from '@/components/ui/textarea'
import type { Collection, Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useVaultStatus } from '../vault/use-vault'
import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { collectionOptions } from './collection-paths'
import { ConnectionTest } from './ConnectionTest'
import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { charsetOptions, defaultPorts, siteDefaults, siteSchema, toSiteInput, type SiteValues } from './site-form'
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
      {error !== undefined && <p className="text-xs text-destructive">{t(($) => $.errors[error as 'nameRequired' | 'hostRequired' | 'portInvalid' | 'usernameRequired'])}</p>}
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
  const secrets = useWatch({ control, name: ['password', 'privateKey', 'passphrase'] })
  const vault = useVaultStatus()
  const needsVault = vault !== undefined && !(vault.initialized && vault.unlocked) && secrets.some(Boolean)

  const submit = handleSubmit(async (values) => {
    try {
      const input = toSiteInput(values)
      await (editing === undefined ? api.sites.create(workspaceId, input) : api.sites.update(workspaceId, editing.id, input))
      await queryClient.invalidateQueries({ queryKey: sitesKeys.sites(workspaceId) })
      onClose()
    } catch (error) {
      setFailure(siteErrorKeyOf(error))
    }
  })

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{editing === undefined ? t(($) => $.site.createTitle) : t(($) => $.site.editTitle)}</DialogTitle>
      </DialogHeader>
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
      {editing !== undefined && <ConnectionTest workspaceId={workspaceId} site={editing} />}
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
