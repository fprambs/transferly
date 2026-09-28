import { LoaderCircle } from "lucide-react"
import { cn } from "cn"

/** Props of {@link Loading}. */
interface LoadingProps {
  /** What is loading, read to assistive technology and shown next to the spinner. */
  label: string
  /** Extra classes of the container. */
  className?: string
  /** Whether the container announces itself as a status; turn it off inside a region that already does, so it is not read twice. */
  announce?: boolean
}

/** A spinner with a label, for a screen, panel or action that is still working. The one place to show that state, so it looks the same everywhere. */
function Loading({ label, className, announce = true }: LoadingProps) {
  return (
    <div role={announce ? "status" : undefined} data-slot="loading" className={cn("flex items-center gap-2 text-sm text-muted-foreground", className)}>
      <LoaderCircle aria-hidden="true" className="size-4 shrink-0 animate-spin text-accent-text motion-reduce:animate-none" />
      <span>{label}</span>
    </div>
  )
}

export { Loading }
