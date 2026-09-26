import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { themes, type ThemeDefinition } from '@/lib/theme'
import { cn } from '@/lib/utils'
import { useUiStore } from '@/state/ui-store'

/** Props of {@link ThemeCard}. */
interface ThemeCardProps {
  /** The theme the card shows. */
  theme: ThemeDefinition
  /** Whether it is the chosen theme. */
  selected: boolean
  /** Whether choosing is off, because the interface follows the system. */
  disabled: boolean
  /** Translated name of the theme's color scheme. */
  scheme: string
  /** Called when the theme is chosen. */
  onChoose: (themeId: string) => void
}

/** A theme drawn with its own colors, whatever theme is active, so the choice can be judged before it is made. */
function ThemeCard({ theme, selected, disabled, scheme, onChoose }: ThemeCardProps) {
  const { tokens } = theme

  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={disabled}
      data-theme-option={theme.id}
      onClick={() => onChoose(theme.id)}
      className={cn(
        'flex cursor-pointer flex-col gap-3 rounded-xl border p-3 text-start outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
        selected && 'border-primary bg-primary/10',
      )}
    >
      <div className="flex h-16 gap-1 overflow-hidden rounded-lg border p-1" style={{ background: tokens.bg, borderColor: tokens.line }} aria-hidden="true">
        <div className="flex w-1/3 flex-col gap-1 rounded-md p-1.5" style={{ background: tokens.surface }}>
          <span className="h-1.5 w-full rounded-full" style={{ background: tokens.accent }} />
          <span className="h-1.5 w-2/3 rounded-full" style={{ background: tokens.line }} />
          <span className="h-1.5 w-3/4 rounded-full" style={{ background: tokens.line }} />
        </div>
        <div className="flex flex-1 flex-col justify-center gap-1 px-1.5">
          <span className="h-1.5 w-3/4 rounded-full" style={{ background: tokens.text }} />
          <span className="h-1.5 w-1/2 rounded-full" style={{ background: tokens.muted }} />
          <span className="flex gap-1">
            <span className="size-2 rounded-full" style={{ background: tokens.ok }} />
            <span className="size-2 rounded-full" style={{ background: tokens.warn }} />
            <span className="size-2 rounded-full" style={{ background: tokens.danger }} />
            <span className="size-2 rounded-full" style={{ background: tokens.info }} />
          </span>
        </div>
      </div>
      <div className="flex items-center justify-between gap-2">
        <span className="flex flex-col">
          <span className="text-sm font-medium">{theme.name}</span>
          <span className="text-xs text-muted-foreground">{scheme}</span>
        </span>
        {selected ? <Check aria-hidden="true" className="size-4 text-accent-text" /> : null}
      </div>
    </button>
  )
}

/** Props of {@link ThemeList}. */
export interface ThemeListProps {
  /** Called when the user chooses a theme. */
  onChoose: (themeId: string) => void
}

/** The list of themes as cards; the interface changes as soon as one is chosen. */
export function ThemeList({ onChoose }: ThemeListProps) {
  const { t } = useTranslation('settings')
  const themeId = useUiStore((state) => state.themeId)
  const followSystem = useUiStore((state) => state.followSystem)

  return (
    <div role="radiogroup" aria-label={t(($) => $.appearance.themes)} className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-3">
      {themes.map((theme) => (
        <ThemeCard
          key={theme.id}
          theme={theme}
          selected={theme.id === themeId}
          disabled={followSystem}
          scheme={t(($) => $.schemes[theme.scheme])}
          onChoose={onChoose}
        />
      ))}
    </div>
  )
}
