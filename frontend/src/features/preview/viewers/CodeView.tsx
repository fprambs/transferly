import { Compartment, EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { basicSetup, EditorView } from 'codemirror'
import { WrapText } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useActiveTheme } from '@/lib/theme/use-theme'
import { editorNonce } from '@/lib/csp'

import { loadLanguageFor } from '../../editor/editor-language'

/** Props of {@link CodeView}. */
export interface CodeViewProps {
  /** Name of the file, which picks the highlighting of its language. */
  name: string
  /** The text to show. */
  text: string
}

/**
 * Shows text read-only with line numbers, the highlighting of its language when its name tells one, search (Ctrl+F or ⌘F) and a switch to
 * wrap long lines. It never edits: the editor is the place for that.
 */
export function CodeView({ name, text }: CodeViewProps) {
  const { t } = useTranslation('preview')
  const { scheme } = useActiveTheme()
  const container = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const theme = useRef(new Compartment())
  const wrapping = useRef(new Compartment())
  const [wrap, setWrap] = useState(false)
  const [initialScheme] = useState(scheme)

  useEffect(() => {
    const controller = new AbortController()
    void loadLanguageFor(name).then((language) => {
      if (controller.signal.aborted) {
        return
      }
      view.current = new EditorView({
        state: EditorState.create({
          doc: text,
          extensions: [
            basicSetup,
            editorNonce(),
            EditorState.readOnly.of(true),
            EditorView.editable.of(false),
            theme.current.of(initialScheme === 'dark' ? oneDark : []),
            wrapping.current.of([]),
            language ?? [],
          ],
        }),
        parent: container.current as HTMLDivElement,
      })
    })
    return () => {
      controller.abort()
      view.current?.destroy()
      view.current = null
    }
  }, [name, text, initialScheme])

  useEffect(() => {
    view.current?.dispatch({ effects: theme.current.reconfigure(scheme === 'dark' ? oneDark : []) })
  }, [scheme])

  const toggleWrap = (): void => {
    view.current?.dispatch({ effects: wrapping.current.reconfigure(wrap ? [] : EditorView.lineWrapping) })
    setWrap(!wrap)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex justify-end border-b px-2 py-1">
        <Button variant={wrap ? 'secondary' : 'ghost'} size="xs" aria-pressed={wrap} onClick={toggleWrap}>
          <WrapText />
          {t(($) => $.text.wrap)}
        </Button>
      </div>
      <div ref={container} aria-label={t(($) => $.text.label, { name })} className="min-h-0 flex-1 overflow-auto text-sm [&_.cm-editor]:h-full" />
    </div>
  )
}
