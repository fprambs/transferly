import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'

/** Validation of a file or folder name; messages are translation keys. */
const nameSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, 'nameRequired')
    .refine((value) => !value.includes('/') && value !== '.' && value !== '..', 'nameInvalid'),
})

/** What the name dialog does, which decides its texts. */
export type NameDialogMode = 'newFolder' | 'rename'

/** Props of {@link EntryNameDialog}. */
export interface EntryNameDialogProps {
  /** Whether a folder is being created or an entry renamed. */
  mode: NameDialogMode
  /** Name the field starts with. */
  initialName: string
  /** Performs the work with the chosen name; a failure is shown in the dialog. */
  onSubmit: (name: string) => Promise<void>
  /** Called when the dialog is dismissed or the work is done. */
  onClose: () => void
}

/** Form that asks for the name of a new folder or the new name of an entry. */
function NameForm({ mode, initialName, onSubmit, onClose }: EntryNameDialogProps) {
  const { t } = useTranslation('explorer')
  const [failure, setFailure] = useState<ExplorerErrorKey | null>(null)
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm({ resolver: zodResolver(nameSchema), defaultValues: { name: initialName } })

  const submit = handleSubmit(async ({ name }) => {
    try {
      await onSubmit(name)
      onClose()
    } catch (error) {
      setFailure(explorerErrorKeyOf(error))
    }
  })

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $[mode].title)}</DialogTitle>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="entry-name">{t(($) => $[mode].name)}</Label>
        <Input id="entry-name" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.name !== undefined} {...register('name')} />
        {errors.name?.message !== undefined && (
          <p className="text-xs text-destructive">{t(($) => $.errors[errors.name?.message as 'nameRequired' | 'nameInvalid'])}</p>
        )}
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.cancel)}
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {t(($) => $[mode].submit)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Dialog that asks for the name of a new folder or the new name of an entry. */
export function EntryNameDialog(props: EntryNameDialogProps) {
  return (
    <Dialog open onOpenChange={props.onClose}>
      <DialogContent className="sm:max-w-[440px]">
        <NameForm {...props} />
      </DialogContent>
    </Dialog>
  )
}
