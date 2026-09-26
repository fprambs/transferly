import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { NativeSelect } from '@/components/ui/native-select'
import type { CopyInput } from '@/lib/api'

/** What a copy does with a file that already exists at the destination. */
export type CopyPolicy = NonNullable<CopyInput['policy']>

/** The policies in the order they are offered; the first is the default because it never destroys anything. */
const policies: readonly CopyPolicy[] = ['skip', 'rename', 'newer', 'overwrite', 'fail']

/** Props of {@link CopyPolicyDialog}. */
export interface CopyPolicyDialogProps {
  /** The policies to offer, best default first; all of them when left out. */
  allowed?: readonly CopyPolicy[]
  /** Name of the file or folder being copied. */
  name: string
  /** Called with the policy the user chose. */
  onConfirm: (policy: CopyPolicy) => void
  /** Called when the user gives up. */
  onCancel: () => void
}

/** Asks what to do with the files that already exist at the destination of a copy. */
export function CopyPolicyDialog({ allowed = policies, name, onConfirm, onCancel }: CopyPolicyDialogProps) {
  const { t } = useTranslation('explorer')
  const [policy, setPolicy] = useState<CopyPolicy>(allowed[0])

  return (
    <Dialog open onOpenChange={onCancel}>
      <DialogContent className="sm:max-w-[440px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.policy.title, { name })}</DialogTitle>
          <DialogDescription>{t(($) => $.policy.body)}</DialogDescription>
        </DialogHeader>
        <NativeSelect aria-label={t(($) => $.policy.label)} value={policy} onChange={(event) => setPolicy(event.target.value as CopyPolicy)}>
          {policies.filter((option) => allowed.includes(option)).map((option) => (
            <option key={option} value={option}>
              {t(($) => $.policy[option])}
            </option>
          ))}
        </NativeSelect>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            {t(($) => $.policy.cancel)}
          </Button>
          <Button onClick={() => onConfirm(policy)}>{t(($) => $.policy.confirm)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
