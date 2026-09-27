import { Compartment, EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { basicSetup, EditorView } from 'codemirror'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { RemoteEntry } from '@/lib/api'
import type { Api } from '@/lib/api/use-api'
import { useActiveTheme } from '@/lib/theme/use-theme'
import { cn } from '@/lib/utils'

import { stampChanged, type FileStamp } from './conflict'
import { loadLanguageFor } from './editor-language'
import { editorErrorKeyOf, type EditorErrorKey } from './errors'

/** Largest file the remote editor opens; bigger files are downloaded, changed locally and uploaded instead. */
export const maxEditableBytes = 2 * 1024 * 1024

/** Props of {@link RemoteEditor}. */
export interface RemoteEditorProps {
  /** Client used to load and save the file. */
  api: Api
  /** Workspace of the connection. */
  workspaceId: string
  /** Site the file belongs to. */
  siteId: string
  /** File being edited, as the explorer last listed it. */
  entry: RemoteEntry
  /** Called when the dialog is dismissed, whether or not anything was saved. */
  onClose: () => void
  /** Called after a save completes, so the caller can refresh its listing. */
  onSaved: () => void
}

/** What the dialog shows in place of the editor while there is no document to edit yet. */
type Phase = { kind: 'loading' } | { kind: 'tooLarge' } | { kind: 'error'; key: EditorErrorKey } | { kind: 'ready' }

/** Reads the whole body of a stream as text. */
async function readText(stream: ReadableStream<Uint8Array>): Promise<string> {
  return new Response(stream).text()
}

/**
 * Edits a remote text file in place, with CodeMirror. The file is downloaded once when the dialog opens; saving
 * checks the file's size and modification time against what was last read, and offers to overwrite it anyway or
 * reload the other version instead of silently losing either side's changes. Saving reuses the atomic upload of
 * 4.3: the server never shows a half-written file.
 */
export function RemoteEditor({ api, workspaceId, siteId, entry, onClose, onSaved }: RemoteEditorProps) {
  const { t } = useTranslation('editor')
  const scheme = useActiveTheme().scheme
  const [initialScheme] = useState(scheme)
  const container = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const themeCompartment = useRef(new Compartment())
  const [phase, setPhase] = useState<Phase>(entry.size > maxEditableBytes ? { kind: 'tooLarge' } : { kind: 'loading' })
  const [baseline, setBaseline] = useState<FileStamp>({ size: entry.size, modifiedAt: entry.modifiedAt })
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<EditorErrorKey | null>(null)
  const [conflict, setConflict] = useState<RemoteEntry | null>(null)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  const tooLarge = phase.kind === 'tooLarge'

  useEffect(() => {
    if (tooLarge) {
      return
    }
    let cancelled = false
    void (async () => {
      try {
        const [stream, fresh] = await Promise.all([api.files.download(workspaceId, siteId, entry.path), api.files.stat(workspaceId, siteId, entry.path)])
        const [text, language] = await Promise.all([readText(stream), loadLanguageFor(entry.path)])
        if (cancelled || container.current === null) {
          return
        }
        view.current = new EditorView({
          state: EditorState.create({
            doc: text,
            extensions: [
              basicSetup,
              themeCompartment.current.of(initialScheme === 'dark' ? oneDark : []),
              language ?? [],
              EditorView.updateListener.of((update) => (update.docChanged ? setDirty(true) : undefined)),
            ],
          }),
          parent: container.current,
        })
        setBaseline({ size: fresh.size, modifiedAt: fresh.modifiedAt })
        setPhase({ kind: 'ready' })
      } catch (error) {
        if (!cancelled) {
          setPhase({ kind: 'error', key: editorErrorKeyOf(error) })
        }
      }
    })()
    return () => {
      cancelled = true
      view.current?.destroy()
      view.current = null
    }
  }, [api, workspaceId, siteId, entry.path, tooLarge, initialScheme])

  useEffect(() => {
    view.current?.dispatch({ effects: themeCompartment.current.reconfigure(scheme === 'dark' ? oneDark : []) })
  }, [scheme])

  const reload = async (): Promise<void> => {
    const [stream, fresh] = await Promise.all([api.files.download(workspaceId, siteId, entry.path), api.files.stat(workspaceId, siteId, entry.path)])
    const text = await readText(stream)
    const editor = view.current as EditorView
    editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: text } })
    setBaseline({ size: fresh.size, modifiedAt: fresh.modifiedAt })
    setDirty(false)
    setConflict(null)
  }

  const save = async (force: boolean): Promise<void> => {
    setSaving(true)
    setSaveError(null)
    try {
      const editor = view.current as EditorView
      const fresh = force ? null : await api.files.stat(workspaceId, siteId, entry.path)
      if (fresh !== null && stampChanged(baseline, fresh)) {
        setConflict(fresh)
        return
      }
      const body = new TextEncoder().encode(editor.state.doc.toString())
      const saved = await api.files.upload(workspaceId, siteId, entry.path, body, { mode: 'overwrite' })
      setBaseline({ size: saved.size, modifiedAt: saved.modifiedAt })
      setDirty(false)
      setConflict(null)
      onSaved()
    } catch (error) {
      setSaveError(editorErrorKeyOf(error))
    } finally {
      setSaving(false)
    }
  }

  const requestClose = (): void => {
    if (dirty) {
      setConfirmDiscard(true)
      return
    }
    onClose()
  }

  return (
    <Dialog open onOpenChange={requestClose}>
      <DialogContent className="flex h-[85svh] max-w-4xl flex-col sm:max-w-4xl" showCloseButton={false}>
        <DialogHeader className="flex-row items-center justify-between gap-3 space-y-0">
          <DialogTitle className="truncate font-mono text-sm font-normal">{entry.path}</DialogTitle>
        </DialogHeader>
        {tooLarge && (
          <p role="alert" className="text-sm text-destructive">
            {t(($) => $.tooLarge)}
          </p>
        )}
        {phase.kind === 'error' && (
          <p role="alert" className="text-sm text-destructive">
            {t(($) => $.errors[phase.key])}
          </p>
        )}
        {phase.kind === 'loading' && (
          <p role="status" className="text-sm text-muted-foreground">
            {t(($) => $.loading)}
          </p>
        )}
        <div ref={container} className={cn('min-h-0 flex-1 overflow-auto rounded-md border text-sm [&_.cm-editor]:h-full', phase.kind !== 'ready' && 'hidden')} />
        {conflict !== null && (
          <div role="alert" className="flex flex-col gap-2 rounded-md border border-warn/40 bg-warn/10 p-3 text-sm">
            <p className="font-medium">{t(($) => $.conflict.title)}</p>
            <p className="text-muted-foreground">{t(($) => $.conflict.body)}</p>
            <div className="flex gap-2">
              <Button type="button" size="sm" variant="outline" onClick={() => void reload()}>
                {t(($) => $.conflict.reload)}
              </Button>
              <Button type="button" size="sm" variant="destructive" onClick={() => void save(true)}>
                {t(($) => $.conflict.overwrite)}
              </Button>
            </div>
          </div>
        )}
        {saveError !== null && (
          <p role="alert" className="text-sm text-destructive">
            {t(($) => $.errors[saveError])}
          </p>
        )}
        {confirmDiscard && (
          <div role="alert" className="flex items-center justify-between gap-3 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm">
            <span>{t(($) => $.discardBody)}</span>
            <div className="flex gap-2">
              <Button type="button" size="sm" variant="outline" onClick={() => setConfirmDiscard(false)}>
                {t(($) => $.discardKeep)}
              </Button>
              <Button type="button" size="sm" variant="destructive" onClick={onClose}>
                {t(($) => $.discardConfirm)}
              </Button>
            </div>
          </div>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={requestClose}>
            {t(($) => $.close)}
          </Button>
          <Button type="button" disabled={phase.kind !== 'ready' || saving || conflict !== null} onClick={() => void save(false)}>
            {saving ? t(($) => $.saving) : t(($) => $.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
