import { RotateCw, Server, ShieldCheck } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { BrandMark } from './BrandMark'

/** Props of {@link AuthFrame}. */
export interface AuthFrameProps {
  /** The card of the page: its form or its message. */
  children: ReactNode
  /** Width of the card, as a max-width class. */
  width?: string
}

/** The three points the brand panel tells, each with its icon. */
const highlights = [
  { key: 'protocols', icon: Server },
  { key: 'vault', icon: ShieldCheck },
  { key: 'transfers', icon: RotateCw },
] as const

/**
 * Frame of the pages seen before signing in: a brand panel in Core Blue with the mark, the tagline and what Transferly does, beside the
 * card of the page. On narrow screens the panel gives way and the card stands alone, with the mark and name on top of it.
 */
export function AuthFrame({ children, width = 'max-w-sm' }: AuthFrameProps) {
  const { t } = useTranslation('auth')
  const { t: common } = useTranslation()

  return (
    <div className="grid min-h-svh bg-background lg:grid-cols-[minmax(0,5fr)_minmax(0,6fr)]">
      <div className="relative hidden flex-col justify-between overflow-hidden bg-(--brand-panel) bg-[radial-gradient(circle_at_15%_10%,var(--brand-panel-glow),transparent_55%)] p-12 text-(--brand-on-panel) duration-500 animate-in fade-in-0 lg:flex">
        <BrandMark surface="brand" className="pointer-events-none absolute -end-24 -bottom-24 size-[28rem] opacity-10" />
        <p className="relative flex items-center gap-3 text-2xl font-bold tracking-tight">
          <BrandMark surface="brand" animated="once" className="size-11" />
          {common(($) => $.app.name)}
        </p>
        <div className="relative flex max-w-md flex-col gap-8">
          <p className="text-3xl leading-tight font-semibold tracking-tight text-balance">{common(($) => $.app.tagline)}</p>
          <ul className="flex flex-col gap-4">
            {highlights.map(({ key, icon: Icon }) => (
              <li key={key} className="flex items-center gap-3 text-[15px]">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-(--brand-on-panel)/10">
                  <Icon aria-hidden="true" className="size-[18px] text-(--brand-ribbon)" />
                </span>
                {t(($) => $.highlights[key])}
              </li>
            ))}
          </ul>
        </div>
        <span />
      </div>
      <main className="flex items-center justify-center p-4 sm:p-8">
        <div className={cn('flex w-full flex-col gap-6 rounded-2xl border bg-card p-8 shadow-xl duration-300 animate-in fade-in-0 slide-in-from-bottom-2', width)}>{children}</div>
      </main>
    </div>
  )
}
