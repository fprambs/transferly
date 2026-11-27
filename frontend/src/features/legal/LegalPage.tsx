import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

/** Link to the public privacy, cookies, and terms notice. */
export function LegalLink() {
  const { t } = useTranslation()
  return <Link className="text-sm text-muted-foreground underline underline-offset-4" to="/legal">{t(($) => $.legal.title)}</Link>
}

/** Public notice about data handling by the software and the self-hosted instance. */
export function LegalPage() {
  const { t } = useTranslation()
  return (
    <main className="mx-auto grid max-w-3xl gap-8 px-6 py-10">
      <header className="grid gap-3">
        <Link className="w-fit text-sm text-muted-foreground underline underline-offset-4" to="/">{t(($) => $.app.name)}</Link>
        <h1 className="text-3xl font-semibold tracking-tight">{t(($) => $.legal.title)}</h1>
        <p className="text-muted-foreground">{t(($) => $.legal.instance)}</p>
      </header>
      <section className="grid gap-3" aria-labelledby="privacy-title">
        <h2 id="privacy-title" className="text-xl font-semibold">{t(($) => $.legal.privacyTitle)}</h2>
        <p>{t(($) => $.legal.privacy)}</p>
        <p>{t(($) => $.legal.providers)}</p>
      </section>
      <section className="grid gap-3" aria-labelledby="cookies-title">
        <h2 id="cookies-title" className="text-xl font-semibold">{t(($) => $.legal.cookiesTitle)}</h2>
        <p>{t(($) => $.legal.cookies)}</p>
        <p>{t(($) => $.legal.storage)}</p>
      </section>
      <section className="grid gap-3" aria-labelledby="terms-title">
        <h2 id="terms-title" className="text-xl font-semibold">{t(($) => $.legal.termsTitle)}</h2>
        <p>{t(($) => $.legal.terms)}</p>
      </section>
    </main>
  )
}
