import { RefreshCw, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatRelative } from '@/lib/datetime'
import { useNow } from '@/lib/use-now'

import { formatDay } from './days'
import { emptyFilter, type EntryFilter } from './entry-filter'

/** How often the age of the listing is written again. */
const ageRefreshMs = 30_000

/** Props of {@link FilterSummary}. */
export interface FilterSummaryProps {
  /** The filter of the folder, which is active. */
  filter: EntryFilter
  /** How many entries the filter keeps. */
  shown: number
  /** How many entries the folder has. */
  total: number
  /** When the listing was received, in milliseconds since the epoch. */
  listedAt: number
  /** Called with the changed filter. */
  onChange: (filter: EntryFilter) => void
  /** Lists the folder again. */
  onRefresh: () => void
}

/** A part of the filter, removed with its button. */
function FacetChip({ label, removeLabel, onRemove }: { label: string; removeLabel: string; onRemove: () => void }) {
  return (
    <span className="flex h-6 shrink-0 items-center gap-1 rounded-full border border-primary/40 bg-primary/10 ps-2.5 pe-1 text-xs text-foreground">
      {label}
      <button type="button" aria-label={removeLabel} onClick={onRemove} className="flex size-4 items-center justify-center rounded-full hover:bg-primary/20">
        <X aria-hidden="true" className="size-3" />
      </button>
    </span>
  )
}

/**
 * The line under the toolbar while a filter is active (4.14d): how many entries it keeps of how many, each part of the filter as a chip to
 * remove, how old the listing it reads is with a way to list the folder again, and a way to clear the filter.
 */
export function FilterSummary({ filter, shown, total, listedAt, onChange, onRefresh }: FilterSummaryProps) {
  const { t, i18n } = useTranslation('explorer')
  const now = useNow(ageRefreshMs)
  const text = filter.text.trim()
  const range =
    filter.modified === 'custom'
      ? t(($) => $.filter.range, { from: filter.from === '' ? '…' : formatDay(filter.from, i18n.language), to: filter.to === '' ? '…' : formatDay(filter.to, i18n.language) })
      : t(($) => $.filter.modifiedPresets[filter.modified])

  return (
    <div role="status" className="flex min-h-9 shrink-0 flex-wrap items-center gap-2 border-b bg-primary/5 px-3 py-1.5 text-xs">
      <span className="font-medium">{t(($) => $.filter.shown, { shown, total })}</span>
      {text !== '' && <FacetChip label={`“${text}”`} removeLabel={t(($) => $.filter.remove, { facet: t(($) => $.filter.text) })} onRemove={() => onChange({ ...filter, text: '' })} />}
      {filter.kinds.length > 0 && (
        <FacetChip
          label={filter.kinds.map((kind) => t(($) => $.filter.kinds[kind])).join(', ')}
          removeLabel={t(($) => $.filter.remove, { facet: t(($) => $.filter.kind) })}
          onRemove={() => onChange({ ...filter, kinds: [] })}
        />
      )}
      {filter.modified !== 'any' && (
        <FacetChip label={range} removeLabel={t(($) => $.filter.remove, { facet: t(($) => $.filter.modified) })} onRemove={() => onChange({ ...filter, modified: 'any', from: '', to: '' })} />
      )}
      {filter.size !== 'any' && (
        <FacetChip
          label={t(($) => $.filter.sizePresets[filter.size])}
          removeLabel={t(($) => $.filter.remove, { facet: t(($) => $.filter.size) })}
          onRemove={() => onChange({ ...filter, size: 'any' })}
        />
      )}
      <span className="ms-auto flex shrink-0 items-center gap-3 text-muted-foreground">
        <span>{t(($) => $.filter.listed, { age: formatRelative(new Date(Math.min(listedAt, now.getTime())), now, i18n.language) })}</span>
        <button type="button" onClick={onRefresh} className="flex items-center gap-1 font-medium text-primary hover:underline">
          <RefreshCw aria-hidden="true" className="size-3" />
          {t(($) => $.filter.refresh)}
        </button>
        <button type="button" onClick={() => onChange(emptyFilter)} className="font-medium text-foreground hover:underline">
          {t(($) => $.filter.clear)}
        </button>
      </span>
    </div>
  )
}
