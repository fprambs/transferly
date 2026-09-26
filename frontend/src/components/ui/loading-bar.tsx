import { cn } from "cn"

/** Props of {@link LoadingBar}. */
type LoadingBarProps = {
  /** Whether work is going on; when false the bar is invisible but keeps its height, so nothing jumps. */
  active: boolean
  /** Accessible name of the bar. */
  label: string
  /** Extra classes of the track. */
  className?: string
}

/** A thin indeterminate progress bar for work of unknown length, such as opening a folder on a server. */
function LoadingBar({ active, label, className }: LoadingBarProps) {
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-busy={active}
      aria-hidden={!active}
      data-slot="loading-bar"
      className={cn("relative h-0.5 w-full shrink-0 overflow-hidden bg-transparent", className)}
    >
      {active ? <div className="indeterminate-bar rounded-full bg-primary" /> : null}
    </div>
  )
}

export { LoadingBar }
