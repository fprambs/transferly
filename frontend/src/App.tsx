import { useTranslation } from 'react-i18next'

import { supportedLanguages } from '@/i18n'

/** Application shell: shows the translated product name and lets the user switch language. */
export default function App() {
  const { t, i18n } = useTranslation()

  return (
    <main className="mx-auto flex min-h-svh max-w-3xl flex-col justify-center gap-6 p-8">
      <header>
        <h1 className="text-3xl font-semibold">{t(($) => $.app.name)}</h1>
        <p className="text-muted-foreground">{t(($) => $.app.tagline)}</p>
      </header>
      <div className="flex items-center gap-2">
        <label htmlFor="language">{t(($) => $.language.label)}</label>
        <select
          id="language"
          className="rounded-md border px-2 py-1"
          value={i18n.resolvedLanguage}
          onChange={(event) => void i18n.changeLanguage(event.target.value)}
        >
          {supportedLanguages.map((code) => (
            <option key={code} value={code}>
              {t(($) => $.language.names[code])}
            </option>
          ))}
        </select>
      </div>
    </main>
  )
}
