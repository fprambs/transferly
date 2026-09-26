import { cn } from "cn"
import type { ComponentProps } from "react"

/** A placeholder block with a shimmer, for content that is still loading; it is hidden from assistive technology. */
function Skeleton({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="skeleton" aria-hidden="true" className={cn("skeleton rounded-md", className)} {...props} />
}

export { Skeleton }
