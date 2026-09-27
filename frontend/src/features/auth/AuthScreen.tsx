import { useTranslation } from 'react-i18next'

import { useState } from 'react'

import { AuthForm, type AuthMode } from './AuthForm'

/** Props of {@link AuthScreen}. */
export interface AuthScreenProps {
  /** Whether the screen signs in or creates the first administrator. */
  mode: AuthMode
}

/** Centered card of the login and first-run setup screens. */
export function AuthScreen({ mode }: AuthScreenProps) {
  const { t } = useTranslation('auth')
  const { t: common } = useTranslation()
  const { t: security } = useTranslation('security')
  const [second, setSecond] = useState(false)

  return (
    <main className="flex min-h-svh items-center justify-center bg-background p-4">
      <div className="flex w-full max-w-sm flex-col gap-6 rounded-2xl border bg-card p-8 shadow-xl">
        <div className="flex flex-col gap-1.5">
          <p className="font-mono text-xs tracking-widest text-primary uppercase">{common(($) => $.app.name)}</p>
          <h1 className="text-xl font-semibold tracking-tight">{second ? security(($) => $.login.title) : t(($) => $[mode].title)}</h1>
          {!second && <p className="text-sm text-muted-foreground">{t(($) => $[mode].subtitle)}</p>}
        </div>
        <AuthForm mode={mode} onSecondStepChange={setSecond} />
        <p className="text-xs text-muted-foreground">{t(($) => $.secure)}</p>
      </div>
    </main>
  )
}
