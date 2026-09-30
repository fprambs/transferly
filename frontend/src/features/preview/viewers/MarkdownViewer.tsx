import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { SandboxFrame } from '../sandbox/SandboxFrame'
import { CodeView } from './CodeView'
import { decodeText, textLimit } from './text'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** The views of a Markdown file. */
type View = 'rendered' | 'source'

/** A Markdown file already read: rendered in the isolated page, or its source; a rendering that fails falls back to the source. */
function Markdown({ name, bytes }: { name: string; bytes: Uint8Array }) {
  const { t } = useTranslation('preview')
  const [view, setView] = useState<View>('rendered')
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div role="group" aria-label={t(($) => $.views.label)} className="flex gap-1 border-b px-2 py-1">
        {(['rendered', 'source'] as const).map((option) => (
          <Button key={option} variant={view === option ? 'secondary' : 'ghost'} size="xs" aria-pressed={view === option} onClick={() => setView(option)}>
            {t(($) => $.views[option])}
          </Button>
        ))}
      </div>
      {view === 'rendered' ? (
        <SandboxFrame kind="markdown" name={name} bytes={bytes} title={t(($) => $.markdown.label, { name })} onFailed={() => setView('source')} />
      ) : (
        <CodeView name={name} text={decodeText(bytes)} />
      )}
    </div>
  )
}

/** Shows a Markdown file rendered and sanitized inside the isolated page, with its source a click away (ADR-0018). */
export function MarkdownViewer({ source }: ViewerProps) {
  return <WholeFile source={source} limit={textLimit}>{(bytes) => <Markdown name={source.name} bytes={bytes} />}</WholeFile>
}
