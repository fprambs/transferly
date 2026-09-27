import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { InstanceLimits, InstanceLimitsView } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { AdminFailure } from './AdminMessage'
import { adminErrorKeyOf, type AdminErrorKey } from './errors'
import { limitFields, limitTexts, limitsFromTexts, type LimitGroup } from './limit-fields'
import { adminKeys, useLimits } from './use-admin'

/** The groups in the order they are drawn. */
const groups: readonly LimitGroup[] = ['bandwidth', 'transfers', 'quotas']

/** Props of {@link LimitsForm}. */
interface LimitsFormProps {
  /** The limits in force and the ones the server started with. */
  view: InstanceLimitsView
}

/** The form of the caps and quotas, filled with the limits in force. */
function LimitsForm({ view }: LimitsFormProps) {
  const { t } = useTranslation('admin')
  const api = useApi()
  const queryClient = useQueryClient()
  const [texts, setTexts] = useState(limitTexts(view.current))
  const [failure, setFailure] = useState<AdminErrorKey | null>(null)
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setSaved(false)
    const limits = limitsFromTexts(texts)
    if (limits === null) {
      setFailure('invalid_limits')
      return
    }
    setBusy(true)
    setFailure(null)
    try {
      const now = await api.admin.setLimits(limits)
      queryClient.setQueryData(adminKeys.limits, now)
      setTexts(limitTexts(now.current))
      setSaved(true)
    } catch (error) {
      setFailure(adminErrorKeyOf(error))
    }
    setBusy(false)
  }

  return (
    <form onSubmit={submit} noValidate className="flex max-w-3xl flex-col gap-5">
      {groups.map((group) => (
        <fieldset key={group} className="rounded-lg border bg-card px-4 py-3.5">
          <legend className="px-1 text-sm font-semibold">{t(($) => $.limits.groups[group].title)}</legend>
          <p className="mb-3 text-xs text-muted-foreground">{t(($) => $.limits.groups[group].note)}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            {limitFields
              .filter((field) => field.group === group)
              .map((field) => {
                const id = `limit-${field.key}`
                const started = view.defaults[field.key as keyof InstanceLimits]
                return (
                  <div key={field.key} className="flex flex-col gap-1.5">
                    <Label htmlFor={id}>{t(($) => $.limits.fields[field.key])}</Label>
                    <div className="flex items-center gap-2">
                      <Input
                        id={id}
                        inputMode="decimal"
                        autoComplete="off"
                        placeholder={t(($) => $.limits.unlimited)}
                        value={texts[field.key]}
                        onChange={(event) => setTexts({ ...texts, [field.key]: event.target.value })}
                        className="max-w-40"
                      />
                      {field.unit !== '' && <span className="text-sm text-muted-foreground">{field.unit}</span>}
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {started === 0 ? t(($) => $.limits.startedUnlimited) : t(($) => $.limits.started, { value: Number((started / field.factor).toFixed(3)), unit: field.unit })}
                    </p>
                  </div>
                )
              })}
          </div>
        </fieldset>
      ))}
      <AdminFailure failure={failure} />
      {saved && (
        <p role="status" className="rounded-md bg-emerald-500/10 px-3 py-2 text-sm text-emerald-800 dark:text-emerald-300">
          {t(($) => $.limits.saved)}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={busy}>
          {t(($) => $.limits.save)}
        </Button>
        <Button type="button" variant="outline" onClick={() => setTexts(limitTexts(view.defaults))}>
          {t(($) => $.limits.restore)}
        </Button>
      </div>
    </form>
  )
}

/** The caps on bandwidth and concurrent transfers and the quotas of the instance, in one form. */
export function LimitsTab() {
  const { t } = useTranslation('admin')
  const view = useLimits()

  if (view === undefined) {
    return <p className="text-sm text-muted-foreground">{t(($) => $.limits.loading)}</p>
  }
  return (
    <div className="flex flex-col gap-4">
      <p className="max-w-3xl text-sm leading-relaxed text-muted-foreground">{t(($) => $.limits.intro)}</p>
      <LimitsForm view={view} />
    </div>
  )
}
