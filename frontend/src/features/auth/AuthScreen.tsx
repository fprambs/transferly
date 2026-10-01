import { useTranslation } from 'react-i18next'

import { useState } from 'react'

import { AuthFrame } from '@/components/brand/AuthFrame'
import { BrandMark } from '@/components/brand/BrandMark'

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
    <AuthFrame>
      <div className="flex flex-col gap-1.5">
        <p className="flex items-center gap-2 text-base font-bold tracking-tight lg:hidden">
          <BrandMark className="size-7" />
          {common(($) => $.app.name)}
        </p>
        <h1 className="text-xl font-semibold tracking-tight">{second ? security(($) => $.login.title) : t(($) => $[mode].title)}</h1>
        {!second && <p className="text-sm text-muted-foreground">{t(($) => $[mode].subtitle)}</p>}
      </div>
      <AuthForm mode={mode} onSecondStepChange={setSecond} />
      <p className="text-xs text-muted-foreground">{t(($) => $.secure)}</p>
    </AuthFrame>
  )
}
