import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { SiteColor } from '@/lib/api'
import { cn } from '@/lib/utils'

import { colorFill, siteColors } from './site-colors'

/** Props of {@link ColorPicker}. */
export interface ColorPickerProps {
  /** Identifier of the group, the target of the label of its row. */
  id: string
  /** Name of the group, read to assistive technology. */
  label: string
  /** The chosen color; empty for none. */
  value: string
  /** Called with the color chosen, or empty for none. */
  onChange: (color: string) => void
}

/**
 * Swatches to choose the label color of a site, or none, as a radio group: the arrow keys move between them as in any group of radio
 * buttons, and each swatch says its color to assistive technology.
 */
export function ColorPicker({ id, label, value, onChange }: ColorPickerProps) {
  const { t } = useTranslation('sites')
  const options: ReadonlyArray<SiteColor | ''> = ['', ...siteColors]

  return (
    <div role="radiogroup" id={id} aria-label={label} className="flex flex-wrap gap-2">
      {options.map((color) => {
        const checked = value === color
        return (
          <label
            key={color || 'none'}
            title={t(($) => $.site.colors[color || 'none'])}
            className={cn(
              'relative flex size-7 cursor-pointer items-center justify-center rounded-full ring-offset-2 ring-offset-background has-focus-visible:ring-2 has-focus-visible:ring-ring',
              colorFill(color || undefined) ?? 'border border-dashed border-muted-foreground/60',
              checked && 'ring-2 ring-foreground',
            )}
          >
            <input
              type="radio"
              name={id}
              value={color}
              checked={checked}
              onChange={() => onChange(color)}
              aria-label={t(($) => $.site.colors[color || 'none'])}
              className="sr-only"
            />
            {checked && (
              <span className="flex size-4 items-center justify-center rounded-full bg-background">
                <Check aria-hidden="true" className="size-3 text-foreground" />
              </span>
            )}
          </label>
        )
      })}
    </div>
  )
}
