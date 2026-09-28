import type { ReactElement } from 'react'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

/** Props of {@link Tip}. */
export interface TipProps {
  /** The text of the tooltip. */
  label: string
  /** Optional keyboard shortcut shown after the text. */
  shortcut?: string
  /** Side of the trigger where the tooltip appears. */
  side?: 'top' | 'bottom' | 'left' | 'right'
  /** The element that shows the tooltip on hover and focus; a disabled button works too. */
  children: ReactElement
}

/**
 * A short tooltip for an icon button or any control, in the colors of the theme. The trigger wraps its child, so disabled buttons, which
 * receive no pointer events, still explain themselves.
 */
export function Tip({ label, shortcut, side = 'bottom', children }: TipProps) {
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex" />}>{children}</TooltipTrigger>
      <TooltipContent side={side}>
        {label}
        {shortcut !== undefined && <kbd className="ms-1 rounded border bg-muted px-1 font-mono text-[10px] text-muted-foreground">{shortcut}</kbd>}
      </TooltipContent>
    </Tooltip>
  )
}
