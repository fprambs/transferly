import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { bookmarkErrorKeyOf, type BookmarkErrorKey } from './errors'

/** Validation of a bookmark; messages are translation keys. */
const bookmarkSchema = z.object({
  name: z.string().trim().min(1, 'nameRequired').max(100, 'nameTooLong'),
  path: z.string().trim().startsWith('/', 'pathAbsolute'),
})

/** What a bookmark form holds. */
type BookmarkForm = z.infer<typeof bookmarkSchema>

/** Keys of the messages of a field that does not validate. */
type FieldError = 'nameRequired' | 'nameTooLong' | 'pathAbsolute'

/** Props of {@link BookmarkDialog}. */
export interface BookmarkDialogProps {
  /** Whether a folder is being bookmarked or a bookmark changed, which decides the texts. */
  mode: 'add' | 'edit'
  /** Name of the site, to say where the folder is. */
  siteName: string
  /** What the fields start with. */
  initial: BookmarkForm
  /** Saves the bookmark; a failure is shown in the dialog. */
  onSubmit: (bookmark: BookmarkForm) => Promise<void>
  /** Called when the dialog is dismissed or the bookmark saved. */
  onClose: () => void
}

/** Form that asks for the name and folder of a bookmark (4.14b). */
function BookmarkFormBody({ mode, siteName, initial, onSubmit, onClose }: BookmarkDialogProps) {
  const { t } = useTranslation('explorer')
  const [failure, setFailure] = useState<BookmarkErrorKey | null>(null)
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm({ resolver: zodResolver(bookmarkSchema), defaultValues: initial })

  const submit = handleSubmit(async (bookmark) => {
    try {
      await onSubmit(bookmark)
      onClose()
    } catch (error) {
      setFailure(bookmarkErrorKeyOf(error))
    }
  })

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.bookmarks[mode].title)}</DialogTitle>
        <DialogDescription>{t(($) => $.bookmarks.dialogHint, { site: siteName })}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="bookmark-name">{t(($) => $.bookmarks.name)}</Label>
        <Input id="bookmark-name" autoComplete="off" aria-invalid={errors.name !== undefined} {...register('name')} />
        {errors.name?.message !== undefined && <p className="text-xs text-destructive">{t(($) => $.bookmarks.errors[errors.name?.message as FieldError])}</p>}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="bookmark-path">{t(($) => $.bookmarks.path)}</Label>
        <Input id="bookmark-path" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.path !== undefined} {...register('path')} />
        {errors.path?.message !== undefined && <p className="text-xs text-destructive">{t(($) => $.bookmarks.errors[errors.path?.message as FieldError])}</p>}
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.bookmarks.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.cancel)}
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {t(($) => $.bookmarks[mode].submit)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Dialog to bookmark a folder or change a bookmark (4.14b). */
export function BookmarkDialog(props: BookmarkDialogProps) {
  return (
    <Dialog open onOpenChange={props.onClose}>
      <DialogContent className="sm:max-w-md">
        <BookmarkFormBody {...props} />
      </DialogContent>
    </Dialog>
  )
}
