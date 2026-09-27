import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { sessionKey, setupKey } from '@/app/session'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { SessionInfo } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { useTransport } from '@/lib/transport'

import { MfaStep } from '../security/MfaStep'
import { needsSecondStep } from '../security/errors'

import { errorKeyOf, type ErrorKey } from './errors'

/** Which of the two account screens the form serves. */
export type AuthMode = 'login' | 'setup'

/** Minimum length of a password the server accepts. */
const minPasswordLength = 12

/** Builds the validation schema of a mode; messages are translation keys. */
function schemaFor(mode: AuthMode) {
  return z.object({
    email: z.string().min(1, 'emailRequired').pipe(z.email('emailInvalid')),
    password:
      mode === 'setup'
        ? z.string().min(minPasswordLength, 'passwordShort')
        : z.string().min(1, 'passwordRequired'),
  })
}

/** Props of {@link AuthForm}. */
export interface AuthFormProps {
  /** Whether the form signs in or creates the first administrator. */
  mode: AuthMode
  /** Called when the form moves to the second step of signing in and back, so the screen can change its heading. */
  onSecondStepChange?: (active: boolean) => void
}

/** Email and password form of the login and first-run setup screens. */
export function AuthForm({ mode, onSecondStepChange }: AuthFormProps) {
  const { t } = useTranslation('auth')
  const api = useApi()
  const transport = useTransport()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [failure, setFailure] = useState<ErrorKey | null>(null)
  const [challenge, setChallenge] = useState<string | null>(null)
  const schema = schemaFor(mode)
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm({ resolver: zodResolver(schema) })

  const start = async (session: SessionInfo) => {
    transport.setCsrfToken(session.csrfToken)
    queryClient.setQueryData(sessionKey, session)
    queryClient.setQueryData(setupKey, false)
    await navigate({ to: '/' })
  }

  const submit = handleSubmit(async (values) => {
    setFailure(null)
    try {
      const answer = await (mode === 'setup' ? api.auth.setup(values) : api.auth.login(values))
      if (needsSecondStep(answer)) {
        setChallenge(answer.challenge)
        onSecondStepChange?.(true)
        return
      }
      await start(answer)
    } catch (error) {
      setFailure(errorKeyOf(error))
      reset({ email: values.email, password: '' })
    }
  })

  if (challenge !== null) {
    return (
      <MfaStep
        challenge={challenge}
        onVerified={start}
        onRestart={(expired) => {
          setChallenge(null)
          onSecondStepChange?.(false)
          setFailure(expired ? 'invalid_challenge' : null)
        }}
      />
    )
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="email">{t(($) => $.fields.email)}</Label>
        <Input
          id="email"
          type="email"
          autoComplete="username"
          aria-invalid={errors.email !== undefined}
          aria-describedby={errors.email === undefined ? undefined : 'email-error'}
          {...register('email')}
        />
        {errors.email?.message !== undefined && (
          <p id="email-error" className="text-xs text-destructive">
            {t(($) => $.errors[errors.email?.message as 'emailRequired' | 'emailInvalid'])}
          </p>
        )}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="password">{t(($) => $.fields.password)}</Label>
        <Input
          id="password"
          type="password"
          autoComplete={mode === 'setup' ? 'new-password' : 'current-password'}
          aria-invalid={errors.password !== undefined}
          aria-describedby={errors.password === undefined ? 'password-hint' : 'password-error'}
          {...register('password')}
        />
        {errors.password?.message === undefined ? (
          mode === 'setup' && (
            <p id="password-hint" className="text-xs text-muted-foreground">
              {t(($) => $.fields.passwordHint)}
            </p>
          )
        ) : (
          <p id="password-error" className="text-xs text-destructive">
            {t(($) => $.errors[errors.password?.message as 'passwordRequired' | 'passwordShort'])}
          </p>
        )}
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <Button type="submit" size="lg" disabled={isSubmitting}>
        {t(($) => $[mode].submit)}
      </Button>
    </form>
  )
}
