import { SlidersHorizontal } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { buttonVariants } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { cn } from '@/lib/utils'

import { dateOfDay, dayOfDate, formatDay } from './days'
import { emptyFilter, facetCount, kindGroups, modifiedPresets, sizePresets, todayIn, type EntryFilter, type KindGroup } from './entry-filter'

/** A choice of the menu that stays pressed while it is part of the filter. */
function Chip({ pressed, onClick, children }: { pressed: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        'h-7 rounded-full border px-2.5 text-xs transition-colors',
        pressed ? 'border-primary/50 bg-primary/15 font-medium text-foreground' : 'border-border text-muted-foreground hover:bg-accent hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}

/** A titled group of choices. */
function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-1.5">
      <h4 className="text-[11px] font-medium tracking-wider text-muted-foreground uppercase">{title}</h4>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </section>
  )
}

/** Props of {@link FilterMenu}. */
export interface FilterMenuProps {
  /** The filter of the folder. */
  filter: EntryFilter
  /** Called with the changed filter. */
  onChange: (filter: EntryFilter) => void
  /** Time zone of the account, whose today the calendar marks. */
  timeZone: string
  /** Whether there is nothing to filter, such as a folder that could not be listed. */
  disabled?: boolean
}

/**
 * The facets of the filter of a folder (4.14d): the groups of types to keep, how recently the entries changed, with a range of days of
 * its own, and how big the files are. Its button counts the facets in use.
 */
export function FilterMenu({ filter, onChange, timeZone, disabled = false }: FilterMenuProps) {
  const { t, i18n } = useTranslation('explorer')
  const today = todayIn(timeZone)
  const count = facetCount(filter)
  const toggleKind = (kind: KindGroup): void =>
    onChange({ ...filter, kinds: filter.kinds.includes(kind) ? filter.kinds.filter((item) => item !== kind) : [...filter.kinds, kind] })

  return (
    <Popover>
      <PopoverTrigger
        disabled={disabled}
        aria-label={t(($) => $.filter.facets, { count })}
        className={cn(buttonVariants({ variant: 'ghost', size: 'sm' }), 'shrink-0 text-muted-foreground hover:text-foreground data-popup-open:bg-accent', count > 0 && 'bg-accent text-foreground')}
      >
        <SlidersHorizontal aria-hidden="true" />
        <span className="@max-3xl:sr-only">{t(($) => $.filter.facetsButton)}</span>
        {count > 0 && <span className="rounded-full bg-primary px-1.5 text-[10px] leading-4 font-semibold text-primary-foreground">{count}</span>}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 gap-4 p-3.5">
        <Section title={t(($) => $.filter.kind)}>
          {kindGroups.map((kind) => (
            <Chip key={kind} pressed={filter.kinds.includes(kind)} onClick={() => toggleKind(kind)}>
              {t(($) => $.filter.kinds[kind])}
            </Chip>
          ))}
        </Section>
        <Section title={t(($) => $.filter.modified)}>
          {modifiedPresets.map((preset) => (
            <Chip key={preset} pressed={filter.modified === preset} onClick={() => onChange({ ...filter, modified: preset })}>
              {t(($) => $.filter.modifiedPresets[preset])}
            </Chip>
          ))}
          {filter.modified === 'custom' && (
            <div className="flex w-full flex-col items-center gap-2 pt-1">
              <dl className="grid w-full grid-cols-2 gap-2 text-xs">
                {(['from', 'to'] as const).map((edge) => (
                  <div key={edge} className={cn('rounded-md border px-2 py-1', filter[edge] !== '' && 'border-primary/40 bg-primary/5')}>
                    <dt className="text-[10px] tracking-wider text-muted-foreground uppercase">{t(($) => $.filter[edge])}</dt>
                    <dd className="font-medium">{filter[edge] === '' ? '—' : formatDay(filter[edge], i18n.language)}</dd>
                  </div>
                ))}
              </dl>
              <Calendar
                language={i18n.language}
                mode="range"
                resetOnSelect
                captionLayout="dropdown"
                startMonth={new Date(1990, 0)}
                endMonth={today}
                today={today}
                defaultMonth={dateOfDay(filter.from) ?? today}
                selected={{ from: dateOfDay(filter.from), to: dateOfDay(filter.to) }}
                onSelect={(range) => onChange({ ...filter, from: range?.from ? dayOfDate(range.from) : '', to: range?.to ? dayOfDate(range.to) : '' })}
              />
            </div>
          )}
        </Section>
        <Section title={t(($) => $.filter.size)}>
          {sizePresets.map((preset) => (
            <Chip key={preset} pressed={filter.size === preset} onClick={() => onChange({ ...filter, size: preset })}>
              {t(($) => $.filter.sizePresets[preset])}
            </Chip>
          ))}
        </Section>
        <p className="text-xs leading-relaxed text-muted-foreground">{t(($) => $.filter.sizeHint)}</p>
        <button
          type="button"
          disabled={count === 0}
          onClick={() => onChange({ ...emptyFilter, text: filter.text })}
          className="self-end text-xs font-medium text-primary hover:underline disabled:pointer-events-none disabled:opacity-50"
        >
          {t(($) => $.filter.clearFacets)}
        </button>
      </PopoverContent>
    </Popover>
  )
}
