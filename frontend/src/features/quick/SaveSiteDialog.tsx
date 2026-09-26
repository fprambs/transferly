import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import type { Collection, QuickConnection, Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { collectionOptions } from '../sites/collection-paths'
import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { quickErrorKeyOf, type QuickErrorKey } from './errors'

/** Validation of the save form; the message is a translation key. */
const saveSchema = z.object({ name: z.string().trim().min(1, 'nameRequired'), collectionId: z.string() })

/** Values of the save form; an empty collection means the top level. */
type SaveValues = z.infer<typeof saveSchema>

/** Props of {@link SaveSiteDialog}. */
export interface SaveSiteDialogProps {
  /** The quick connection to keep. */
  connection: QuickConnection
  /** Workspace the connection belongs to. */
  workspaceId: string
  /** Collections the site can be filed in. */
  collections: readonly Collection[]
  /** Called when the dialog is dismissed without saving. */
  onClose: () => void
  /** Called with the site once it is saved. */
  onSaved: (site: Site) => void
}

/** Form that names the site and picks its collection. */
function SaveForm({ connection, workspaceId, collections, onClose, onSaved }: SaveSiteDialogProps) {
  const { t } = useTranslation('quick')
  const api = useApi()
  const [failure, setFailure] = useState<QuickErrorKey | null>(null)
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<SaveValues>({ resolver: zodResolver(saveSchema), defaultValues: { name: connection.host, collectionId: '' } })

  const submit = handleSubmit(async (values) => {
    try {
      const site = await api.quick.save(workspaceId, connection.id, {
        name: values.name.trim(),
        collectionId: values.collectionId === '' ? undefined : values.collectionId,
      })
      onSaved(site)
    } catch (error) {
      setFailure(quickErrorKeyOf(error))
    }
  })

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.save.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.save.description, { host: connection.host })}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="save-name">{t(($) => $.save.name)}</Label>
        <Input id="save-name" autoComplete="off" aria-invalid={errors.name !== undefined} {...register('name')} />
        {errors.name !== undefined && <p className="text-xs text-destructive">{t(($) => $.errors.nameRequired)}</p>}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="save-collection">{t(($) => $.save.collection)}</Label>
        <NativeSelect
          id="save-collection"
          {...register('collectionId')}
        >
          <option value="">{t(($) => $.save.noCollection)}</option>
          {collectionOptions(collections).map((option) => (
            <option key={option.id} value={option.id}>
              {option.label}
            </option>
          ))}
        </NativeSelect>
      </div>
      {failure === 'vault_locked' ? (
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
          {t(($) => $.cancel)}
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {t(($) => $.save.submit)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Turns a quick connection into a saved site, in a collection of the user's choice. */
export function SaveSiteDialog(props: SaveSiteDialogProps) {
  return (
    <Dialog open onOpenChange={props.onClose}>
      <DialogContent className="sm:max-w-[460px]">
        <SaveForm {...props} />
      </DialogContent>
    </Dialog>
  )
}
