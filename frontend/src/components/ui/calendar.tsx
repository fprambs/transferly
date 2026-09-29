import { DayPicker, type ChevronProps, type DayPickerProps } from '@daypicker/react'
import { enUS, es } from '@daypicker/react/locale'
import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp } from 'lucide-react'

import { cn } from '@/lib/utils'

/** The arrows of the calendar, drawn with the icons of the rest of the interface. */
function Chevron({ orientation, className }: ChevronProps) {
  const Icon = { up: ChevronUp, down: ChevronDown, left: ChevronLeft, right: ChevronRight }[orientation ?? 'left']
  return <Icon aria-hidden="true" className={cn('size-4', className)} />
}

/** Locales of the calendar by interface language; other languages use English. */
const locales = { es, en: enUS } as const

/**
 * A calendar in the look of the interface, built on DayPicker (docs/guides/daypicker.md): its colors come from the theme, the selected
 * days and the days of a range are marked with the brand color, and its texts follow the interface language.
 */
export function Calendar({ language, className, classNames, ...props }: DayPickerProps & { language: string }) {
  return (
    <DayPicker
      locale={language in locales ? locales[language as keyof typeof locales] : enUS}
      showOutsideDays
      components={{ Chevron }}
      className={cn('w-fit text-sm', className)}
      classNames={{
        months: 'relative flex flex-col',
        month: 'flex flex-col gap-2',
        month_caption: 'flex h-8 items-center justify-center px-8',
        caption_label: 'font-medium',
        dropdowns: 'flex items-center gap-1.5',
        dropdown_root: 'relative flex h-7 items-center rounded-md border bg-background px-2 text-xs hover:border-foreground/25 has-focus-visible:ring-2 has-focus-visible:ring-ring',
        dropdown: 'absolute inset-0 cursor-pointer opacity-0',
        nav: 'absolute inset-x-0 top-0 flex h-8 items-center justify-between',
        button_previous: 'flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-40',
        button_next: 'flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-40',
        month_grid: 'border-collapse',
        weekdays: 'flex',
        weekday: 'flex size-8 items-center justify-center text-[11px] font-medium text-muted-foreground uppercase',
        week: 'mt-0.5 flex',
        day: 'relative flex size-8 items-center justify-center p-0 text-xs',
        day_button:
          'flex size-8 items-center justify-center rounded-md outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring aria-selected:hover:bg-transparent',
        today: 'font-semibold text-accent-text',
        outside: 'text-muted-foreground/50',
        disabled: 'pointer-events-none text-muted-foreground/40',
        selected: '[&>button]:bg-primary [&>button]:text-primary-foreground [&>button]:hover:bg-primary',
        range_start: 'rounded-s-md bg-primary/15',
        range_middle: 'bg-primary/15 [&>button]:bg-transparent [&>button]:text-foreground [&>button]:hover:bg-primary/20',
        range_end: 'rounded-e-md bg-primary/15',
        hidden: 'invisible',
        ...classNames,
      }}
      {...props}
    />
  )
}
