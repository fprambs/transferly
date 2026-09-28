import { cn } from "cn"

import { Button } from "./button"

/** Props of {@link RetryFailure}. */
interface RetryFailureProps {
  /** Explanation of what went wrong. */
  message: string
  /** Label of the button that tries again. */
  retryLabel: string
  /** Called when the button is pressed. */
  onRetry: () => void
  /** Extra classes of the container. */
  className?: string
}

/** An alert that explains a failed load and offers to try it again. The one place a panel that could not load its data says so, instead of staying on its loading state forever. */
function RetryFailure({ message, retryLabel, onRetry, className }: RetryFailureProps) {
  return (
    <div role="alert" data-slot="retry-failure" className={cn("flex max-w-xl flex-col gap-3 rounded-xl border border-destructive/40 bg-destructive/10 p-4 text-sm", className)}>
      <p className="text-destructive">{message}</p>
      <Button type="button" variant="outline" className="self-start" onClick={onRetry}>
        {retryLabel}
      </Button>
    </div>
  )
}

export { RetryFailure }
