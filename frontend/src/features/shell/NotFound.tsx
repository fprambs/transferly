import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

/** Screen for an address that leads nowhere. */
export function NotFound() {
  const { t } = useTranslation('settings')

  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-3 bg-background p-6 text-center">
      <h1 className="text-xl font-semibold tracking-tight">{t(($) => $.notFound.title)}</h1>
      <p className="max-w-sm text-sm text-muted-foreground">{t(($) => $.notFound.body)}</p>
      <Link to="/" className="text-sm font-medium text-accent-text underline-offset-4 hover:underline">
        {t(($) => $.notFound.home)}
      </Link>
    </main>
  )
}
