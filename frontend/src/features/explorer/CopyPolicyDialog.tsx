import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select } from '@/components/ui/select'
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
  /** Whether to offer to verify the copy by reading it back; uploads and downloads to the local folder cannot. */
  verifiable?: boolean
  /** Called with the policy the user chose and whether the copy is to be verified. */
  onConfirm: (policy: CopyPolicy, verify: boolean) => void
  /** Called when the user gives up. */
  onCancel: () => void
}

/** Asks what to do with the files that already exist at the destination of a copy. */
export function CopyPolicyDialog({ allowed = policies, verifiable = false, name, onConfirm, onCancel }: CopyPolicyDialogProps) {
  const { t } = useTranslation('explorer')
  const [policy, setPolicy] = useState<CopyPolicy>(allowed[0])
  const [verify, setVerify] = useState(false)

  return (
    <Dialog open onOpenChange={onCancel}>
      <DialogContent className="sm:max-w-[440px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.policy.title, { name })}</DialogTitle>
          <DialogDescription>{t(($) => $.policy.body)}</DialogDescription>
        </DialogHeader>
        <Select
          aria-label={t(($) => $.policy.label)}
          value={policy}
          options={policies.filter((option) => allowed.includes(option)).map((option) => ({ value: option, label: t(($) => $.policy[option]) }))}
          onValueChange={(value) => setPolicy(value as CopyPolicy)}
        />
        {verifiable ? (
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1" checked={verify} onChange={(event) => setVerify(event.target.checked)} />
            <span>
              {t(($) => $.policy.verify)}
              <span className="block text-xs text-muted-foreground">{t(($) => $.policy.verifyHint)}</span>
            </span>
          </label>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            {t(($) => $.policy.cancel)}
          </Button>
          <Button onClick={() => onConfirm(policy, verify)}>{t(($) => $.policy.confirm)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
