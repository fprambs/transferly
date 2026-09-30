import { FileLock, FileWarning } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'

import { SandboxFrame } from '../sandbox/SandboxFrame'
import { HexViewer } from './HexViewer'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** The largest Office file the viewer reads, since it is read whole and laid out in the page. */
export const officeLimit = 50 * 1024 * 1024

/**
 * Shows an Office document inside the isolated page (ADR-0018): Word and OpenDocument texts laid out as pages, Excel and OpenDocument
 * workbooks as sheets, and PowerPoint decks as slides, all read and drawn in the browser, never sent anywhere. A document protected with a
 * password, or one the page cannot read, says so and offers its bytes.
 */
export function OfficeViewer({ source, kind }: ViewerProps) {
  const { t } = useTranslation('preview')
  const [failure, setFailure] = useState<string | null>(null)
  const [asBytes, setAsBytes] = useState(false)
  const labels = useMemo(
    () => ({
      sheets: t(($) => $.office.sheets),
      rowsCut: t(($) => $.office.rowsCut, { shown: '{{shown}}', total: '{{total}}' }),
      columnsCut: t(($) => $.office.columnsCut, { shown: '{{shown}}', total: '{{total}}' }),
      emptySheet: t(($) => $.office.emptySheet),
      slides: t(($) => $.office.slides, { total: '{{total}}' }),
      slide: t(($) => $.office.slide, { number: '{{number}}' }),
    }),
    [t],
  )
  if (asBytes) {
    return <HexViewer source={source} kind="binary" />
  }
  if (failure !== null) {
    const locked = failure === 'password'
    return (
      <PanelState
        icon={locked ? FileLock : FileWarning}
        tone="warn"
        title={locked ? t(($) => $.office.protected) : t(($) => $.office.broken)}
        description={locked ? t(($) => $.office.protectedBody) : t(($) => $.office.brokenBody)}
      >
        <Button variant="outline" onClick={() => setAsBytes(true)}>
          {t(($) => $.tooLarge.asBytes)}
        </Button>
      </PanelState>
    )
  }
  return (
    <WholeFile source={source} limit={officeLimit}>
      {(bytes) => <SandboxFrame kind={kind} name={source.name} bytes={bytes} labels={labels} title={t(($) => $.office.label, { name: source.name })} onFailed={setFailure} />}
    </WholeFile>
  )
}
