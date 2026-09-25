import { cn } from "cn"
import { ChevronDownIcon } from "lucide-react"
import type { ComponentProps } from "react"

/** Props of {@link NativeSelect}. */
type NativeSelectProps = ComponentProps<"select"> & {
  /** Extra classes of the element that wraps the select and its chevron. */
  containerClassName?: string
}

/**
 * A native drop-down drawn with the theme: same height, border, hover and focus ring as the inputs, and a chevron.
 *
 * It stays a real `<select>`, so it keeps its keyboard, form and screen reader behavior.
 */
function NativeSelect({ className, containerClassName, children, ...props }: NativeSelectProps) {
  return (
    <div data-slot="native-select-wrapper" className={cn("relative inline-flex w-full items-center", containerClassName)}>
      <select
        data-slot="native-select"
        className={cn(
          "h-8 w-full cursor-pointer appearance-none rounded-lg border border-input bg-transparent ps-2.5 pe-8 text-sm text-foreground outline-none transition-colors hover:bg-accent focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive dark:bg-input/30 [&_optgroup]:bg-popover [&_optgroup]:text-muted-foreground [&_option]:bg-popover [&_option]:text-popover-foreground",
          className
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDownIcon aria-hidden="true" className="pointer-events-none absolute end-2.5 size-4 text-muted-foreground" />
    </div>
  )
}

export { NativeSelect }
