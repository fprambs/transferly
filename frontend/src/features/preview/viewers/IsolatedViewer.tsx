import { FileLock, FileWarning } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'

import { SandboxFrame } from '../sandbox/SandboxFrame'
import { HexViewer } from './HexViewer'
import type { PreviewKind } from '../detect'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** The groups of kinds the isolated page draws whole, each with its own words. */
type Group = 'office' | 'sqlite'

/** The group of each kind this viewer shows, and the largest file of it the viewer reads, since it is read whole into the page. */
const groups: Partial<Record<PreviewKind, { group: Group; limit: number }>> = {
  document: { group: 'office', limit: 50 * 1024 * 1024 },
  spreadsheet: { group: 'office', limit: 50 * 1024 * 1024 },
  presentation: { group: 'office', limit: 50 * 1024 * 1024 },
  sqlite: { group: 'sqlite', limit: 64 * 1024 * 1024 },
}

/**
 * Shows a file the isolated page reads whole and draws itself (ADR-0018): Word and OpenDocument texts laid out as pages, Excel and
 * OpenDocument workbooks as sheets, PowerPoint decks as slides, and SQLite databases as tables, all read and drawn in the browser, never
 * sent anywhere. A document protected with a password, or a file the page cannot read, says so and offers its bytes.
 */
export function IsolatedViewer({ source, kind }: ViewerProps) {
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
      tables: t(($) => $.sqlite.tables),
      view: t(($) => $.sqlite.view),
      rows: t(($) => $.sqlite.rows, { from: '{{from}}', to: '{{to}}', total: '{{total}}' }),
      previous: t(($) => $.sqlite.previous),
      next: t(($) => $.sqlite.next),
      structure: t(($) => $.sqlite.structure),
      query: t(($) => $.sqlite.query),
      queryPlaceholder: t(($) => $.sqlite.queryPlaceholder),
      run: t(($) => $.sqlite.run),
      result: t(($) => $.sqlite.result),
      resultRows: t(($) => $.sqlite.resultRows, { total: '{{count}}' }),
      resultCut: t(($) => $.sqlite.resultCut, { total: '{{count}}' }),
      blob: t(($) => $.sqlite.blob, { size: '{{size}}' }),
      empty: t(($) => $.sqlite.empty),
    }),
    [t],
  )
  if (asBytes) {
    return <HexViewer source={source} kind="binary" />
  }
  const { group, limit } = groups[kind] as { group: Group; limit: number }
  if (failure !== null) {
    const locked = failure === 'password'
    return (
      <PanelState
        icon={locked ? FileLock : FileWarning}
        tone="warn"
        title={locked ? t(($) => $.office.protected) : t(($) => $[group].broken)}
        description={locked ? t(($) => $.office.protectedBody) : t(($) => $[group].brokenBody)}
      >
        <Button variant="outline" onClick={() => setAsBytes(true)}>
          {t(($) => $.tooLarge.asBytes)}
        </Button>
      </PanelState>
    )
  }
  return (
    <WholeFile source={source} limit={limit}>
      {(bytes) => <SandboxFrame kind={kind} name={source.name} bytes={bytes} labels={labels} title={t(($) => $[group].label, { name: source.name })} onFailed={setFailure} />}
    </WholeFile>
  )
}
