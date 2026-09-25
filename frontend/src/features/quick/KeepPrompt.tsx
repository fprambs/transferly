import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

/** Props of {@link KeepPrompt}. */
export interface KeepPromptProps {
  /** Host of the connection that worked. */
  host: string
  /** How long connecting took, in milliseconds. */
  elapsedMs: number
  /** Called to start saving the server as a site. */
  onSave: () => void
  /** Called to dismiss the prompt. */
  onDismiss: () => void
}

/** Offers to keep a quick connection that worked. */
export function KeepPrompt({ host, elapsedMs, onSave, onDismiss }: KeepPromptProps) {
  const { t } = useTranslation('quick')

  return (
    <div
      role="status"
      className="fixed bottom-7 start-1/2 z-50 flex w-[560px] max-w-[calc(100vw-2rem)] -translate-x-1/2 items-center gap-3.5 rounded-xl border bg-muted p-3 shadow-2xl rtl:translate-x-1/2"
    >
      <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-ok/20 text-ok">
        <Check className="size-3.5" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="font-semibold">{t(($) => $.keep.title)}</span>
        <span className="truncate text-xs text-muted-foreground">
          {t(($) => $.keep.detail, { host })} · {elapsedMs} ms
        </span>
      </div>
      <Button size="sm" onClick={onSave}>
        {t(($) => $.keep.save)}
      </Button>
      <Button size="sm" variant="ghost" onClick={onDismiss}>
        {t(($) => $.keep.dismiss)}
      </Button>
    </div>
  )
}
