import { useQuery } from '@tanstack/react-query'
import { FileLock, FileWarning } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { PanelState } from '@/components/ui/panel-state'

import { SandboxFrame } from '../sandbox/SandboxFrame'
import { HexViewer } from './HexViewer'
import type { ViewerProps } from './types'

/** The bytes handed to the isolated page with the file: enough for the start of most documents, the rest comes by ranges. */
const firstBytes = 1024 * 1024

/**
 * Shows a PDF with pdf.js inside the isolated page (ADR-0018): the page gets the first MiB and asks for the rest by ranges, which are read
 * from the file here, so a large document shows its first page without being fetched whole. A document that asks for a password, or that
 * pdf.js cannot open, says so and offers its bytes.
 */
export function PdfViewer({ source }: ViewerProps) {
  const { t } = useTranslation('preview')
  const [failure, setFailure] = useState<string | null>(null)
  const [asBytes, setAsBytes] = useState(false)
  const first = useQuery({
    queryKey: ['preview', source.key, 'pdf-first'],
    queryFn: ({ signal }) => source.read(0, firstBytes, signal),
    staleTime: Infinity,
    retry: false,
  })
  const labels = useMemo(
    () => ({
      thumbnails: t(($) => $.pdf.thumbnails),
      fitWidth: t(($) => $.pdf.fitWidth),
      zoomIn: t(($) => $.image.zoomIn),
      zoomOut: t(($) => $.image.zoomOut),
      search: t(($) => $.pdf.search),
      page: t(($) => $.pdf.page, { number: '{{number}}' }),
      position: t(($) => $.pdf.position, { page: '{{page}}', total: '{{total}}' }),
      matches: t(($) => $.pdf.matches, { page: '{{page}}', total: '{{total}}' }),
      noMatches: t(($) => $.pdf.noMatches),
    }),
    [t],
  )
  if (asBytes) {
    return <HexViewer source={source} kind="binary" />
  }
  if (failure !== null) {
    const protectedPdf = failure === 'password'
    return (
      <PanelState
        icon={protectedPdf ? FileLock : FileWarning}
        tone="warn"
        title={protectedPdf ? t(($) => $.pdf.protected) : t(($) => $.pdf.broken)}
        description={protectedPdf ? t(($) => $.pdf.protectedBody) : t(($) => $.pdf.brokenBody)}
      >
        <Button variant="outline" onClick={() => setAsBytes(true)}>
          {t(($) => $.tooLarge.asBytes)}
        </Button>
      </PanelState>
    )
  }
  if (first.data === undefined) {
    return <Loading label={t(($) => $.loading)} className="m-auto" />
  }
  return <SandboxFrame kind="pdf" name={source.name} bytes={first.data} source={source} labels={labels} title={t(($) => $.pdf.label, { name: source.name })} onFailed={setFailure} />
}
