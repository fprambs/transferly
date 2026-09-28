import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

/** Colors of the icon of a panel state: the accent, a warning, a failure or a plain fact. */
const tones = {
  accent: 'bg-accent-text/10 text-accent-text ring-accent-text/5',
  warn: 'bg-warn/15 text-warn ring-warn/5',
  danger: 'bg-destructive/15 text-destructive ring-destructive/5',
  muted: 'bg-muted text-muted-foreground ring-muted/40',
} as const

/** Props of {@link PanelState}. */
export interface PanelStateProps {
  /** Icon drawn in the circle above the title. */
  icon: LucideIcon
  /** How the state reads. */
  tone?: keyof typeof tones
  /** What happened, in a few words. */
  title: string
  /** What it means or what to do. */
  description?: ReactNode
  /** Announced as a status (default) or, for failures, as an alert; a state that is only a fact, such as an empty folder, is not announced. */
  role?: 'status' | 'alert' | 'none'
  /** Details and actions, below the text. */
  children?: ReactNode
  /** Extra classes of the card. */
  className?: string
}

/**
 * The state of a panel that has nothing else to show (disconnected, a failure, a key to trust, an empty folder): a card centered in the
 * panel both ways, as the vault card is. Every panel of Sentinel shows its states this way.
 */
export function PanelState({ icon: Icon, tone = 'accent', title, description, role = 'status', children, className }: PanelStateProps) {
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto p-6">
      <div
        role={role === 'none' ? undefined : role}
        className={cn('flex w-full max-w-md flex-col items-center gap-5 rounded-2xl border bg-card px-8 py-9 text-center shadow-sm duration-300 animate-in fade-in-0 zoom-in-95', className)}
      >
        <div className={cn('flex size-16 items-center justify-center rounded-full ring-8', tones[tone])}>
          <Icon aria-hidden="true" className="size-7" />
        </div>
        <div className="flex flex-col gap-2">
          <p className="text-lg font-semibold tracking-tight">{title}</p>
          {description !== undefined && <div className="text-sm leading-relaxed text-muted-foreground">{description}</div>}
        </div>
        {children}
      </div>
    </div>
  )
}
