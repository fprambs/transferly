import { useQuery } from '@tanstack/react-query'
import { CircleAlert, FileWarning } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { PanelState } from '@/components/ui/panel-state'

import { formatSize } from '../../explorer/entries'
import type { PreviewSource } from '../source'

import { HexViewer } from './HexViewer'

/** Props of {@link WholeFile}. */
export interface WholeFileProps {
  /** The file to read. */
  source: PreviewSource
  /** The largest file the viewer reads whole, in bytes. */
  limit: number
  /** Draws the file once it is read. */
  children: (bytes: Uint8Array) => ReactNode
}

/**
 * Reads a whole file for a viewer that needs all of it, such as text or a table (ADR-0018). A file over the limit of the viewer is not
 * read: it says so and offers the hexadecimal view, which reads any size by pages. A failed read can be tried again.
 */
export function WholeFile({ source, limit, children }: WholeFileProps) {
  const { t } = useTranslation('preview')
  const [asBytes, setAsBytes] = useState(false)
  const tooLarge = source.size > limit
  const file = useQuery({
    queryKey: ['preview', source.key, 'all'],
    queryFn: ({ signal }) => source.read(0, source.size, signal),
    enabled: !tooLarge && !asBytes,
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
  if (asBytes) {
    return <HexViewer source={source} kind="binary" />
  }
  if (tooLarge) {
    return (
      <PanelState icon={FileWarning} tone="warn" title={t(($) => $.tooLarge.title)} description={t(($) => $.tooLarge.body, { limit: formatSize(limit) })}>
        <Button variant="outline" onClick={() => setAsBytes(true)}>
          {t(($) => $.tooLarge.asBytes)}
        </Button>
      </PanelState>
    )
  }
  if (file.isPending) {
    return <Loading label={t(($) => $.loading)} className="m-auto" />
  }
  if (file.isError) {
    return (
      <PanelState icon={CircleAlert} tone="danger" role="alert" title={t(($) => $.failed)}>
        <Button variant="outline" onClick={() => void file.refetch()}>
          {t(($) => $.retry)}
        </Button>
      </PanelState>
    )
  }
  return children(file.data)
}
