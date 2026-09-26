import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useApi } from '@/lib/api/use-api'

import { vaultErrorKeyOf, type VaultErrorKey } from './errors'
import { vaultKey } from './use-vault'

/** Minimum length of a master password the server accepts. */
const minPasswordLength = 12

/** Which step of the vault the dialog serves. */
export type VaultMode = 'setup' | 'unlock'

/** Validation of the unlock form; messages are translation keys. */
const unlockSchema = z.object({ password: z.string().min(1, 'passwordRequired'), confirm: z.string() })

/** Validation of the setup form: a long enough password, typed twice. */
const setupSchema = z
  .object({ password: z.string().min(minPasswordLength, 'passwordShort'), confirm: z.string() })
  .refine((values) => values.password === values.confirm, { path: ['confirm'], message: 'confirmMismatch' })

/** Props of {@link VaultDialog}. */
export interface VaultDialogProps {
  /** Which step to show, or `null` when the dialog is closed. */
  mode: VaultMode | null
  /** Called when the dialog is dismissed. */
  onClose: () => void
  /** Called after the vault was unlocked, or created and its recovery kit stored, so the caller can carry on with what needed it. */
  onUnlocked?: () => void
}

/** Props of {@link RecoveryKit}. */
interface RecoveryKitProps {
  /** The recovery kit, shown once. */
  kit: string
  /** Called when the user confirms having stored it. */
  onDone: () => void
}

/** Shows the recovery kit created with the vault, with a button to copy it. */
function RecoveryKit({ kit, onDone }: RecoveryKitProps) {
  const { t } = useTranslation('vault')
  const [copied, setCopied] = useState(false)

  return (
    <div className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.kit.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.kit.description)}</DialogDescription>
      </DialogHeader>
      <pre className="rounded-lg border bg-muted/40 p-3 font-mono text-sm break-all whitespace-pre-wrap">{kit}</pre>
      <DialogFooter>
        <Button
          variant="outline"
          onClick={async () => {
            await navigator.clipboard.writeText(kit)
            setCopied(true)
          }}
        >
          {copied ? t(($) => $.kit.copied) : t(($) => $.kit.copy)}
        </Button>
        <Button onClick={onDone}>{t(($) => $.kit.done)}</Button>
      </DialogFooter>
    </div>
  )
}

/** Props of {@link PasswordForm}. */
interface PasswordFormProps {
  /** Whether the vault is being created or unlocked. */
  mode: VaultMode
  /** Called when the dialog is dismissed. */
  onClose: () => void
  /** Called with the recovery kit when a new vault was created. */
  onKit: (kit: string) => void
  /** Called after the vault was unlocked. */
  onUnlocked?: () => void
}

/** Master password form of the vault dialog. */
function PasswordForm({ mode, onClose, onKit, onUnlocked }: PasswordFormProps) {
  const { t } = useTranslation('vault')
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<VaultErrorKey | null>(null)
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm({ resolver: zodResolver(mode === 'setup' ? setupSchema : unlockSchema), defaultValues: { password: '', confirm: '' } })

  const submit = handleSubmit(async ({ password }) => {
    setFailure(null)
    try {
      if (mode === 'setup') {
        const { recoveryKit } = await api.vault.setup(password)
        onKit(recoveryKit)
      } else {
        await api.vault.unlock(password)
        await queryClient.invalidateQueries({ queryKey: vaultKey })
        onClose()
        onUnlocked?.()
        return
      }
      await queryClient.invalidateQueries({ queryKey: vaultKey })
    } catch (error) {
      setFailure(vaultErrorKeyOf(error))
      reset()
    }
  })

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $[mode].title)}</DialogTitle>
        <DialogDescription>{t(($) => $[mode].description)}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="vault-password">{t(($) => $.fields.password)}</Label>
        <Input
          id="vault-password"
          type="password"
          autoComplete={mode === 'setup' ? 'new-password' : 'current-password'}
          aria-invalid={errors.password !== undefined}
          {...register('password')}
        />
        {errors.password?.message !== undefined && (
          <p className="text-xs text-destructive">{t(($) => $.errors[errors.password?.message as 'passwordRequired' | 'passwordShort'])}</p>
        )}
      </div>
      {mode === 'setup' && (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="vault-confirm">{t(($) => $.fields.confirm)}</Label>
          <Input id="vault-confirm" type="password" autoComplete="new-password" aria-invalid={errors.confirm !== undefined} {...register('confirm')} />
          {errors.confirm?.message !== undefined && <p className="text-xs text-destructive">{t(($) => $.errors.confirmMismatch)}</p>}
        </div>
      )}
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.cancel)}
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {t(($) => $[mode].submit)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Dialog to create or unlock the vault; a new vault ends with its recovery kit. */
export function VaultDialog({ mode, onClose, onUnlocked }: VaultDialogProps) {
  const [kit, setKit] = useState<string | null>(null)

  return (
    <Dialog open={mode !== null} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[480px]" showCloseButton={kit === null}>
        {kit !== null ? (
          <RecoveryKit
            kit={kit}
            onDone={() => {
              setKit(null)
              onClose()
              onUnlocked?.()
            }}
          />
        ) : (
          mode !== null && <PasswordForm key={mode} mode={mode} onClose={onClose} onKit={setKit} onUnlocked={onUnlocked} />
        )}
      </DialogContent>
    </Dialog>
  )
}
