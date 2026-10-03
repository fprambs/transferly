import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

/** Screen for a failure no route handled, instead of the router's own: a short message and a way to reload. */
export function RouteFault() {
  const { t } = useTranslation('shell')

  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-3 bg-background p-6 text-center">
      <h1 className="text-xl font-semibold tracking-tight">{t(($) => $.routeError.title)}</h1>
      <p className="max-w-sm text-sm text-muted-foreground">{t(($) => $.routeError.body)}</p>
      <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
        {t(($) => $.routeError.reload)}
      </Button>
    </main>
  )
}
