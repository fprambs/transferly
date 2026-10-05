import { ListFilter, X } from 'lucide-react'
import type { RefObject } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

/** Props of {@link FilterBox}. */
export interface FilterBoxProps {
  /** Text of the filter. */
  value: string
  /** Called with the new text. */
  onChange: (value: string) => void
  /** Called when the user leaves the box for the list: Escape on an empty box or the down arrow. */
  onLeave: () => void
  /** The field, so the explorer can focus it with its shortcuts. */
  inputRef: RefObject<HTMLInputElement | null>
  /** Whether there is nothing to filter, such as a folder that could not be listed. */
  disabled?: boolean
}

/**
 * The box that filters the folder in view as the user types (4.14d). Escape clears the text, and on an empty box goes back to the list, as
 * does the down arrow; the hint shows the shortcut while the box is empty.
 */
export function FilterBox({ value, onChange, onLeave, inputRef, disabled = false }: FilterBoxProps) {
  const { t } = useTranslation('explorer')

  return (
    <div
      className={cn(
        'group/filter flex h-7 w-44 shrink-0 items-center gap-1.5 rounded-md border bg-background px-2 transition-[width,border-color] focus-within:w-60 focus-within:border-ring hover:border-foreground/25 @max-3xl:w-32 @max-3xl:focus-within:w-48 @max-2xl:w-auto @max-2xl:min-w-24 @max-2xl:flex-1 @max-2xl:shrink @max-2xl:focus-within:w-auto',
        value !== '' && 'border-primary/50',
        disabled && 'pointer-events-none opacity-50',
      )}
    >
      <ListFilter aria-hidden="true" className={cn('size-3.5 shrink-0', value !== '' ? 'text-primary' : 'text-muted-foreground')} />
      <input
        ref={inputRef}
        type="search"
        disabled={disabled}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault()
            if (value === '') {
              onLeave()
            } else {
              onChange('')
            }
          } else if (event.key === 'ArrowDown') {
            event.preventDefault()
            onLeave()
          }
        }}
        aria-label={t(($) => $.filter.label)}
        placeholder={t(($) => $.filter.placeholder)}
        title={t(($) => $.filter.help)}
        spellCheck={false}
        autoComplete="off"
        className="min-w-0 flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground [&::-webkit-search-cancel-button]:hidden"
      />
      {value === '' ? (
        <kbd aria-hidden="true" className="rounded border px-1 font-mono text-[10px] text-muted-foreground group-focus-within/filter:hidden">
          /
        </kbd>
      ) : (
        <button
          type="button"
          aria-label={t(($) => $.filter.clearText)}
          onClick={() => {
            const field = inputRef.current as HTMLInputElement
            onChange('')
            field.focus()
          }}
          className="flex size-4 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <X aria-hidden="true" className="size-3" />
        </button>
      )}
    </div>
  )
}
