import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { CodeView } from './CodeView'
import { decodeText, textLimit } from './text'
import { ParseError, parseTree, type TreeNode } from './tree'
import { TreeView } from './TreeView'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** The views of a structured file. */
type View = 'tree' | 'source'

/** The tree or the source of a file already read, with the switch between them; a file that does not parse shows its error and source. */
function Structured({ kind, name, text }: { kind: 'json' | 'yaml' | 'xml'; name: string; text: string }) {
  const { t } = useTranslation('preview')
  const [view, setView] = useState<View>('tree')
  let parsed: ReturnType<typeof parseTree> | ParseError
  try {
    parsed = parseTree(kind, name, text)
  } catch (error) {
    parsed = error as ParseError
  }
  const failure = parsed instanceof ParseError ? parsed : null
  const failed = failure !== null
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div role="group" aria-label={t(($) => $.views.label)} className="flex gap-1 border-b px-2 py-1">
        {(['tree', 'source'] as const).map((option) => (
          <Button key={option} variant={view === option && !(failed && option === 'tree') ? 'secondary' : 'ghost'} size="xs" aria-pressed={view === option} disabled={failed && option === 'tree'} onClick={() => setView(option)}>
            {t(($) => $.views[option])}
          </Button>
        ))}
      </div>
      {failure !== null && (
        <p role="alert" className="border-b bg-destructive/10 px-4 py-2 text-xs text-danger-text">
          {t(($) => $.tree.invalid, { error: failure.message })}
        </p>
      )}
      {failure === null && view === 'tree' ? <TreeView root={parsed as TreeNode} label={t(($) => $.tree.label, { name })} /> : <CodeView name={name} text={text} />}
    </div>
  )
}

/** Shows JSON, JSON Lines, YAML and XML as a tree of values or elements, with their source a click away (ADR-0018). */
export function StructuredViewer({ source, kind }: ViewerProps) {
  return <WholeFile source={source} limit={textLimit}>{(bytes) => <Structured kind={kind as 'json' | 'yaml' | 'xml'} name={source.name} text={decodeText(bytes)} />}</WholeFile>
}
