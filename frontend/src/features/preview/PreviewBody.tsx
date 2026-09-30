import { useQuery } from '@tanstack/react-query'
import { CircleAlert, PencilLine } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { PanelState } from '@/components/ui/panel-state'

import { ErrorGuide } from '../errors/ErrorGuide'

import { detectKind, headLength, type PreviewKind } from './detect'
import type { PreviewSource } from './source'
import { viewerOf } from './viewers/registry'

/** Kinds the editor opens. */
const editable: ReadonlySet<PreviewKind> = new Set(['text', 'markdown', 'json', 'yaml', 'xml', 'csv', 'svg'])

/**
 * The body of the viewer for one file: reads its first bytes, tells its kind and shows it with the viewer of that kind, under a line that
 * names the kind and offers the editor for text. The Quick Look window shows it for the file chosen, and the archive viewer for an entry.
 */
export function PreviewBody({ source, onEdit }: { source: PreviewSource; onEdit?: (source: PreviewSource) => void }) {
  const { t } = useTranslation('preview')
  const head = useQuery({
    queryKey: ['preview', source.key, 'head'],
    queryFn: ({ signal }) => source.read(0, headLength, signal),
    staleTime: Infinity,
    retry: false,
  })
  if (source.size === 0) {
    return <p className="m-auto text-sm text-muted-foreground">{t(($) => $.empty)}</p>
  }
  if (head.isPending) {
    return <Loading label={t(($) => $.loading)} className="m-auto" />
  }
  if (head.isError) {
    return (
      <PanelState icon={CircleAlert} tone="danger" role="alert" title={t(($) => $.failed)}>
        <ErrorGuide failure={head.error} />
        <Button variant="outline" onClick={() => void head.refetch()}>
          {t(($) => $.retry)}
        </Button>
      </PanelState>
    )
  }
  const kind = detectKind(source.name, head.data)
  const { View, fallback } = viewerOf(kind)
  return (
    <>
      <div className="flex items-center gap-3 border-b px-4 py-1.5 text-xs text-muted-foreground">
        <span>{t(($) => $.kinds[kind])}</span>
        {fallback && <span>· {t(($) => $.hex.note)}</span>}
        {onEdit !== undefined && editable.has(kind) && (
          <Button variant="ghost" size="xs" className="ms-auto" onClick={() => onEdit(source)}>
            <PencilLine />
            {t(($) => $.edit)}
          </Button>
        )}
      </div>
      <View source={source} kind={kind} />
    </>
  )
}
