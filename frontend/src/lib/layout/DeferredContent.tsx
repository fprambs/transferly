import { Component, Suspense, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'

/** Props of the loading boundary used inside dialogs and feature panels. */
export interface DeferredContentProps {
  /** Contents that may load a component on demand. */
  children: ReactNode
}

/** Content and recovery view of a failed feature module. */
interface DeferredBoundaryProps extends DeferredContentProps {
  /** A translated recovery action without internal error details. */
  fallback: ReactNode
}

/** Whether a feature module failed to load or render. */
interface DeferredBoundaryState {
  /** A failure keeps the surrounding dialog and navigation available. */
  failed: boolean
}

/** Contains feature failures so unavailable code does not replace the whole application shell. */
class DeferredBoundary extends Component<DeferredBoundaryProps, DeferredBoundaryState> {
  /** Starts with the feature content available. */
  state: DeferredBoundaryState = { failed: false }

  /** Replaces a failed feature with its recovery action. */
  static getDerivedStateFromError(): DeferredBoundaryState { return { failed: true } }

  /** Shows the feature content until React reports a failure. */
  render(): ReactNode { return this.state.failed ? this.props.fallback : this.props.children }
}

/** Keeps surrounding dialogs and navigation mounted while feature code loads, and offers recovery if it fails. */
export function DeferredContent({ children }: DeferredContentProps) {
  const { t } = useTranslation('preview')
  const { t: common } = useTranslation('common')
  const failure = (
    <div role="alert" className="flex flex-col items-start gap-3 p-4 text-sm">
      <p>{common(($) => $.deferred.failed)}</p>
      <Button type="button" variant="outline" size="sm" onClick={() => window.location.reload()}>{common(($) => $.deferred.reload)}</Button>
    </div>
  )
  return <DeferredBoundary fallback={failure}><Suspense fallback={<Loading label={t(($) => $.loading)} />}>{children}</Suspense></DeferredBoundary>
}
