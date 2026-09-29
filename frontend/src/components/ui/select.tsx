import { Select as SelectPrimitive } from '@base-ui/react/select'
import { CheckIcon, ChevronDownIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

/** One choice of a {@link Select}. */
export interface SelectOption {
  /** Value the choice stands for. */
  value: string
  /** Text shown for it. */
  label: string
  /** Whether it cannot be chosen. */
  disabled?: boolean
}

/** Props of {@link Select}. */
export interface SelectProps {
  /** Value chosen now. */
  value: string
  /** Called with the value the user chooses. */
  onValueChange: (value: string) => void
  /** The choices, in order. */
  options: readonly SelectOption[]
  /** Identifier of the trigger, which a `<label htmlFor>` names. */
  id?: string
  /** Name of the control when no label names it. */
  'aria-label'?: string
  /** Whether the value is wrong, drawn as the inputs draw it. */
  'aria-invalid'?: boolean
  /** Whether it cannot be changed. */
  disabled?: boolean
  /** Extra classes of the trigger. */
  className?: string
  /** Extra classes of the list of choices. */
  popupClassName?: string
}

/**
 * A drop-down drawn with the theme, in place of the browser's own list, which themes cannot style (4.14k): the trigger looks like the
 * inputs and the list like the menus, with the chosen value checked. Built on Base UI's Select, it keeps the keyboard (arrows, typing to
 * jump, Enter, Escape) and the roles screen readers expect.
 */
export function Select({ value, onValueChange, options, id, 'aria-label': ariaLabel, 'aria-invalid': ariaInvalid, disabled, className, popupClassName }: SelectProps) {
  return (
    <SelectPrimitive.Root
      value={value}
      onValueChange={(next) => onValueChange(next as string)}
      items={options.map((option) => ({ value: option.value, label: option.label }))}
      disabled={disabled}
    >
      <SelectPrimitive.Trigger
        id={id}
        aria-label={ariaLabel}
        aria-invalid={ariaInvalid}
        className={cn(
          'flex h-8 w-full min-w-0 items-center justify-between gap-2 rounded-lg border border-input bg-transparent ps-2.5 pe-2 text-start text-sm text-foreground outline-none transition-colors hover:bg-accent focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive data-popup-open:border-ring dark:bg-input/30',
          className,
        )}
      >
        <SelectPrimitive.Value className="truncate" />
        <SelectPrimitive.Icon className="flex shrink-0 text-muted-foreground">
          <ChevronDownIcon aria-hidden="true" className="size-4" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Positioner sideOffset={4} alignItemWithTrigger={false} className="isolate z-50 outline-none">
          <SelectPrimitive.Popup
            className={cn(
              'max-h-(--available-height) min-w-(--anchor-width) origin-(--transform-origin) overflow-y-auto rounded-lg bg-popover p-1 text-sm text-popover-foreground shadow-md ring-1 ring-foreground/10 outline-none duration-100 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95',
              popupClassName,
            )}
          >
            <SelectPrimitive.List>
              {options.map((option) => (
                <SelectPrimitive.Item
                  key={option.value}
                  value={option.value}
                  disabled={option.disabled}
                  className="relative flex cursor-pointer items-center gap-2 rounded-md py-1.5 ps-2 pe-8 outline-none select-none data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-accent data-highlighted:text-accent-foreground data-selected:font-medium"
                >
                  <SelectPrimitive.ItemText>{option.label}</SelectPrimitive.ItemText>
                  <SelectPrimitive.ItemIndicator className="absolute end-2 flex items-center text-primary">
                    <CheckIcon aria-hidden="true" className="size-4" />
                  </SelectPrimitive.ItemIndicator>
                </SelectPrimitive.Item>
              ))}
            </SelectPrimitive.List>
          </SelectPrimitive.Popup>
        </SelectPrimitive.Positioner>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  )
}
