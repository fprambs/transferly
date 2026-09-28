import { useQueryClient } from '@tanstack/react-query'
import { Clock } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import type { Site, TimeZoneProbe as Probe } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { siteDefaults, toSiteInput } from '../sites/site-form'
import { sitesKeys } from '../sites/use-sites-data'

import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'

/** Props of {@link TimeZoneProbe}. */
export interface TimeZoneProbeProps {
  /** Workspace of the connection. */
  workspaceId: string
  /** Saved site or quick connection whose listings are read. */
  siteId: string
  /** Folder in which the probe file is written. */
  path: string
  /** The saved site, or `undefined` for a quick connection, whose time zone cannot be kept. */
  site: Site | undefined
  /** Called after the time zone of the site was changed, so the listing is read again. */
  onApplied: () => void
}

/** What the probe is doing or found. */
type State = { kind: 'working' } | { kind: 'found'; probe: Probe } | { kind: 'applied'; zone: string } | { kind: 'failed'; key: ExplorerErrorKey } | null

/** Formats an offset in minutes as `+03:00`. */
function formatOffset(minutes: number): string {
  const sign = minutes < 0 ? '-' : '+'
  const abs = Math.abs(minutes)
  return `${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`
}

/** Button that finds the time zone of the listings of an FTP server, and says what it found and what to do about it. */
export function TimeZoneProbe({ workspaceId, siteId, path, site, onApplied }: TimeZoneProbeProps) {
  const { t } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const [state, setState] = useState<State>(null)

  const probe = async (): Promise<void> => {
    setState({ kind: 'working' })
    try {
      setState({ kind: 'found', probe: await api.files.probeTimeZone(workspaceId, siteId, path) })
    } catch (error) {
      setState({ kind: 'failed', key: explorerErrorKeyOf(error) })
    }
  }

  const apply = async (zone: string): Promise<void> => {
    const current = site as Site
    try {
      await api.sites.update(workspaceId, current.id, { ...toSiteInput(siteDefaults(current, undefined)), listTimezone: zone })
      await queryClient.invalidateQueries({ queryKey: sitesKeys.sites(workspaceId) })
      setState({ kind: 'applied', zone })
      onApplied()
    } catch (error) {
      setState({ kind: 'failed', key: explorerErrorKeyOf(error) })
    }
  }

  return (
    <>
      <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.timezone.button)} disabled={state?.kind === 'working'} onClick={() => void probe()}>
        <Clock />
      </Button>
      {state === null ? null : (
        <div role="status" className="absolute end-3 top-12 z-30 flex max-w-sm flex-col gap-2 rounded-lg border bg-card p-3 text-sm shadow-lg">
          {state.kind === 'working' ? <Loading label={t(($) => $.timezone.working)} announce={false} className="text-foreground" /> : null}
          {state.kind === 'failed' ? <p className="text-destructive">{t(($) => $.timezone.failed)} {t(($) => $.errors[state.key])}</p> : null}
          {state.kind === 'applied' ? <p>{t(($) => $.timezone.applied, { zone: state.zone })}</p> : null}
          {state.kind === 'found' && state.probe.exact ? <p>{t(($) => $.timezone.exact)}</p> : null}
          {state.kind === 'found' && !state.probe.exact ? (
            <>
              <p>{t(($) => $.timezone.offset, { offset: formatOffset(state.probe.offsetMinutes) })}</p>
              {state.probe.suggestedZone === undefined ? <p className="text-muted-foreground">{t(($) => $.timezone.manual)}</p> : null}
              {state.probe.suggestedZone !== undefined && site === undefined ? <p className="text-muted-foreground">{t(($) => $.timezone.quick)}</p> : null}
              {state.probe.suggestedZone !== undefined && site !== undefined ? (
                <Button size="sm" className="self-start" onClick={() => void apply(state.probe.suggestedZone as string)}>
                  {t(($) => $.timezone.suggest, { zone: state.probe.suggestedZone })}
                </Button>
              ) : null}
            </>
          ) : null}
          {state.kind === 'working' ? null : (
            <Button variant="outline" size="sm" className="self-start" onClick={() => setState(null)}>
              {t(($) => $.timezone.dismiss)}
            </Button>
          )}
        </div>
      )}
    </>
  )
}
