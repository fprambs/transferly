import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

/** Props of {@link RecoveryCodes}. */
export interface RecoveryCodesProps {
  /** The codes, shown once. */
  codes: string[]
  /** Called when the user confirms having stored them. */
  onDone: () => void
}

/** The recovery codes of an account with a button to copy them all, shown only when they are created. */
export function RecoveryCodes({ codes, onDone }: RecoveryCodesProps) {
  const { t } = useTranslation('security')
  const [copied, setCopied] = useState(false)

  return (
    <div className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.recovery.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.recovery.description)}</DialogDescription>
      </DialogHeader>
      <ul className="grid grid-cols-2 gap-2 rounded-lg border bg-muted/40 p-3 font-mono text-sm">
        {codes.map((code) => (
          <li key={code}>{code}</li>
        ))}
      </ul>
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={async () => {
            await navigator.clipboard.writeText(codes.join('\n'))
            setCopied(true)
          }}
        >
          {copied ? t(($) => $.recovery.copied) : t(($) => $.recovery.copy)}
        </Button>
        <Button type="button" onClick={onDone}>
          {t(($) => $.recovery.done)}
        </Button>
      </DialogFooter>
    </div>
  )
}
