import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

/** Props of {@link PageHeader}. */
export interface PageHeaderProps {
  /** Icon of the page, drawn in a tile in the brand color. */
  icon: LucideIcon
  /** Title of the page, its only first-level heading. */
  title: string
  /** What the page is for, in one sentence. */
  description: string
  /** Buttons on the right of the title, such as the main action of the page. */
  actions?: ReactNode
  /** What goes below the title, such as the tabs of the page. */
  children?: ReactNode
}

/** Heading of a full page: an icon tile, the title and its description, with room for the tabs below. */
export function PageHeader({ icon: Icon, title, description, actions, children }: PageHeaderProps) {
  return (
    <header className="flex flex-col gap-5">
      <div className="flex flex-wrap items-start gap-3.5">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary/12 text-accent-text">
          <Icon aria-hidden="true" className="size-5" />
        </span>
        <div className="flex min-w-0 flex-col gap-0.5">
          <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
        {actions !== undefined && <div className="ms-auto flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
      {children}
    </header>
  )
}

/** Props of {@link Page}. */
export interface PageProps {
  /** The header and the cards of the page. */
  children: ReactNode
}

/** The frame of a full page: centered, with a comfortable width, and room between its header and cards. */
export function Page({ children }: PageProps) {
  return <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-7">{children}</div>
}

/** Props of {@link EmptyCard}. */
export interface EmptyCardProps {
  /** Icon of the state, drawn in a tinted circle. */
  icon: LucideIcon
  /** What the state is, as the heading of the card. */
  title: string
  /** What it means or what to do. */
  body?: string
  /** Actions, such as a button to get out of the state. */
  children?: ReactNode
}

/** A card for a page that has nothing to show yet, or cannot show it: an icon, a heading, its explanation and what to do. */
export function EmptyCard({ icon: Icon, title, body, children }: EmptyCardProps) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border bg-card px-6 py-12 text-center shadow-xs">
      <span className="flex size-12 items-center justify-center rounded-full bg-primary/12 text-accent-text">
        <Icon aria-hidden="true" className="size-5" />
      </span>
      <h2 className="text-base font-semibold">{title}</h2>
      {body !== undefined && <p className="max-w-md text-sm text-muted-foreground">{body}</p>}
      {children}
    </div>
  )
}

/** Props of {@link Section}. */
export interface SectionProps {
  /** Heading of the card. */
  title: string
  /** What the card holds or how it works, under its title. */
  description?: ReactNode
  /** Controls on the right of the heading: a filter, a count, a button. */
  actions?: ReactNode
  /** Bar at the bottom of the card, for its buttons and messages. */
  footer?: ReactNode
  /** Identifier of the heading, for regions and forms that it names. */
  id?: string
  /** Whether the body touches the edges, as a table does, instead of keeping the padding of the card. */
  flush?: boolean
  /** The content of the card. */
  children: ReactNode
  /** Extra classes of the card. */
  className?: string
}

/** A card of a page: a heading with its description and actions, the content, and an optional bar of actions at the bottom. */
export function Section({ title, description, actions, footer, id, flush = false, children, className }: SectionProps) {
  return (
    <section aria-labelledby={id} className={cn('flex min-w-0 flex-col overflow-hidden rounded-xl border bg-card shadow-xs', className)}>
      <div className="flex flex-wrap items-start justify-between gap-3 border-b px-5 py-4">
        <div className="flex min-w-0 flex-col gap-1">
          <h2 id={id} className="text-sm font-semibold">
            {title}
          </h2>
          {description !== undefined && <div className="max-w-2xl text-xs leading-relaxed text-muted-foreground">{description}</div>}
        </div>
        {actions !== undefined && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
      <div className={flush ? undefined : 'px-5 py-4'}>{children}</div>
      {footer !== undefined && <div className="flex flex-wrap items-center gap-3 border-t bg-muted/30 px-5 py-3">{footer}</div>}
    </section>
  )
}

/** Props of {@link StatTile}. */
export interface StatTileProps {
  /** What is counted. */
  label: string
  /** The count. */
  value: number
  /** Icon of the count. */
  icon: LucideIcon
  /** Color of the icon tile, from the state colors of the theme. */
  tone?: Tone
}

/** The colors a tile or a badge can wear, all from the tokens of the theme. */
export type Tone = 'accent' | 'ok' | 'warn' | 'danger' | 'info' | 'muted'

/** Classes of the soft background and the text of each tone. */
const tones: Record<Tone, string> = {
  accent: 'bg-primary/12 text-accent-text',
  ok: 'bg-ok/12 text-ok',
  warn: 'bg-warn/12 text-warn',
  danger: 'bg-destructive/12 text-destructive',
  info: 'bg-info/12 text-info',
  muted: 'bg-muted text-muted-foreground',
}

/** A count at the top of a page, with its icon in a tinted tile. */
export function StatTile({ label, value, icon: Icon, tone = 'accent' }: StatTileProps) {
  return (
    <div className="flex items-center gap-3 rounded-xl border bg-card px-4 py-3.5 shadow-xs">
      <span className={cn('flex size-9 shrink-0 items-center justify-center rounded-lg', tones[tone])}>
        <Icon aria-hidden="true" className="size-[18px]" />
      </span>
      <div className="flex min-w-0 flex-col">
        <span className="text-xl leading-tight font-semibold tabular-nums">{value}</span>
        <span className="truncate text-xs text-muted-foreground">{label}</span>
      </div>
    </div>
  )
}

/** Props of {@link Badge}. */
export interface BadgeProps {
  /** Color of the badge. */
  tone: Tone
  /** Whether a dot leads the text, for states. */
  dot?: boolean
  /** The text. */
  children: ReactNode
}

/** A short label in a soft pill of one of the tones, for roles, states and origins. */
export function Badge({ tone, dot = false, children }: BadgeProps) {
  return (
    <span className={cn('inline-flex h-[22px] items-center gap-1.5 rounded-full px-2.5 text-xs font-medium whitespace-nowrap', tones[tone])}>
      {dot && <span aria-hidden="true" className="size-1.5 rounded-full bg-current" />}
      {children}
    </span>
  )
}
