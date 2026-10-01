import { useQueryClient } from '@tanstack/react-query'
import { Info } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTable, Td, Th, Tr } from '@/components/page/DataTable'
import { Section } from '@/components/page/Page'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Loading } from '@/components/ui/loading'
import { RetryFailure } from '@/components/ui/retry-failure'
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
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {groups.map((group) => (
        <Section key={group} id={`limits-${group}`} title={t(($) => $.limits.groups[group].title)} description={t(($) => $.limits.groups[group].note)} flush>
          <DataTable aria-labelledby={`limits-${group}`}>
            <thead>
              <tr>
                <Th className="w-1/2">{t(($) => $.limits.table.limit)}</Th>
                <Th>{t(($) => $.limits.table.value)}</Th>
                <Th>{t(($) => $.limits.table.started)}</Th>
              </tr>
            </thead>
            <tbody>
              {limitFields
                .filter((field) => field.group === group)
                .map((field) => {
                  const id = `limit-${field.key}`
                  const started = view.defaults[field.key as keyof InstanceLimits]
                  return (
                    <Tr key={field.key}>
                      <Td>
                        <Label htmlFor={id} className="font-normal">
                          {t(($) => $.limits.fields[field.key])}
                        </Label>
                      </Td>
                      <Td>
                        <div className="flex items-center gap-2">
                          <Input
                            id={id}
                            inputMode="decimal"
                            autoComplete="off"
                            placeholder={t(($) => $.limits.unlimited)}
                            value={texts[field.key]}
                            onChange={(event) => setTexts({ ...texts, [field.key]: event.target.value })}
                            className="h-8 w-36 tabular-nums"
                          />
                          {field.unit !== '' && <span className="text-xs text-muted-foreground">{field.unit}</span>}
                        </div>
                      </Td>
                      <Td className="whitespace-nowrap text-muted-foreground">
                        {started === 0 ? t(($) => $.limits.startedUnlimited) : t(($) => $.limits.started, { value: Number((started / field.factor).toFixed(3)), unit: field.unit })}
                      </Td>
                    </Tr>
                  )
                })}
            </tbody>
          </DataTable>
        </Section>
      ))}
      <AdminFailure failure={failure} />
      <div className="sticky bottom-0 flex flex-wrap items-center gap-3 rounded-xl border bg-card/95 px-5 py-3 shadow-sm backdrop-blur-sm">
        <Button type="submit" disabled={busy}>
          {t(($) => $.limits.save)}
        </Button>
        <Button type="button" variant="outline" onClick={() => setTexts(limitTexts(view.defaults))}>
          {t(($) => $.limits.restore)}
        </Button>
        {saved && (
          <p role="status" className="text-sm text-ok">
            {t(($) => $.limits.saved)}
          </p>
        )}
      </div>
    </form>
  )
}

/** The caps on bandwidth and concurrent transfers and the quotas of the instance, in one form. */
export function LimitsTab() {
  const { t } = useTranslation('admin')
  const view = useLimits()

  if (view.loading) {
    return <Loading label={t(($) => $.limits.loading)} />
  }
  if (view.data === undefined) {
    return <RetryFailure message={t(($) => $.errors[adminErrorKeyOf(view.error)])} retryLabel={t(($) => $.retry)} onRetry={view.refetch} />
  }
  return (
    <div className="flex flex-col gap-4">
      <p className="flex items-start gap-2.5 rounded-xl border border-info/30 bg-info/8 px-5 py-3.5 text-sm leading-relaxed">
        <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-info" />
        {t(($) => $.limits.intro)}
      </p>
      <LimitsForm view={view.data} />
    </div>
  )
}
