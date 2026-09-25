import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import type { Collection } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { collectionOptions } from './collection-paths'
import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { sitesKeys } from './use-sites-data'

/** What the collection dialog does. */
export type CollectionTarget = { mode: 'create'; parentId?: string } | { mode: 'edit'; collection: Collection }

/** Validation of the collection form; the message is a translation key. */
const collectionSchema = z.object({ name: z.string().trim().min(1, 'nameRequired'), parentId: z.string() })

/** Values of the collection form; an empty parent means the top level. */
type CollectionValues = z.infer<typeof collectionSchema>

/** Props of {@link CollectionDialog}. */
export interface CollectionDialogProps {
  /** What to create or edit; the dialog is closed when there is none. */
  target: CollectionTarget | null
  /** Workspace the collection belongs to. */
  workspaceId: string
  /** Every collection of the workspace. */
  collections: readonly Collection[]
  /** Called when the dialog is dismissed or the work is done. */
  onClose: () => void
}

/** Props of {@link CollectionForm}. */
interface CollectionFormProps extends Omit<CollectionDialogProps, 'target'> {
  /** What to create or edit. */
  target: CollectionTarget
}

/** Form to name a collection and choose where it lives. */
function CollectionForm({ target, workspaceId, collections, onClose }: CollectionFormProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)
  const editing = target.mode === 'edit' ? target.collection : undefined
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<CollectionValues>({
    resolver: zodResolver(collectionSchema),
    defaultValues: { name: editing?.name ?? '', parentId: editing?.parentId ?? (target.mode === 'create' ? (target.parentId ?? '') : '') },
  })

  const submit = handleSubmit(async ({ name, parentId }) => {
    try {
      if (editing === undefined) {
        await api.collections.create(workspaceId, { name: name.trim(), parentId: parentId === '' ? undefined : parentId })
      } else {
        await api.collections.update(workspaceId, editing.id, parentId === '' ? { name: name.trim(), moveToTop: true } : { name: name.trim(), parentId })
      }
      await queryClient.invalidateQueries({ queryKey: sitesKeys.collections(workspaceId) })
      onClose()
    } catch (error) {
      setFailure(siteErrorKeyOf(error))
    }
  })

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{editing === undefined ? t(($) => $.collection.createTitle) : t(($) => $.collection.editTitle)}</DialogTitle>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="collection-name">{t(($) => $.collection.name)}</Label>
        <Input id="collection-name" autoComplete="off" aria-invalid={errors.name !== undefined} {...register('name')} />
        {errors.name !== undefined && <p className="text-xs text-destructive">{t(($) => $.errors.nameRequired)}</p>}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="collection-parent">{t(($) => $.collection.parent)}</Label>
        <NativeSelect
          id="collection-parent"
          {...register('parentId')}
        >
          <option value="">{t(($) => $.collection.top)}</option>
          {collectionOptions(collections, editing?.id).map((option) => (
            <option key={option.id} value={option.id}>
              {option.label}
            </option>
          ))}
        </NativeSelect>
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.collection.cancel)}
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {editing === undefined ? t(($) => $.collection.create) : t(($) => $.collection.save)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Dialog to create a collection or to rename and move an existing one. */
export function CollectionDialog({ target, ...form }: CollectionDialogProps) {
  return (
    <Dialog open={target !== null} onOpenChange={form.onClose}>
      <DialogContent className="sm:max-w-[440px]">{target !== null && <CollectionForm target={target} {...form} />}</DialogContent>
    </Dialog>
  )
}
