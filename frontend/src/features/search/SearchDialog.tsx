import { File, Folder } from 'lucide-react'
import { type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useApi } from '@/lib/api/use-api'

import { formatSize, parentPath } from '../explorer/entries'

import { useSearch } from './use-search'
import type { SearchMatch } from './walk'

/** Props of {@link SearchDialog}. */
export interface SearchDialogProps {
  /** Workspace of the connection. */
  workspaceId: string
  /** Saved site or quick connection to search. */
  siteId: string
  /** Absolute path of the folder the search starts from. */
  path: string
  /** Called with a match the user chose, to open the folder that holds it (or itself, for a folder). */
  onOpen: (path: string) => void
  /** Called when the dialog is dismissed. */
  onClose: () => void
}

/** One matching file or folder, with its path and a button to open it. */
function MatchRow({ match, onOpen }: { match: SearchMatch; onOpen: () => void }) {
  const isFolder = match.entry.type === 'directory'

  return (
    <li className="border-b px-4 py-2 text-[13px] last:border-b-0">
      <button type="button" onClick={onOpen} className="flex w-full items-center gap-2.5 text-start hover:text-primary">
        {isFolder ? <Folder aria-hidden="true" className="size-4 shrink-0 text-accent-text" /> : <File aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />}
        <span className="min-w-0 flex-1 truncate font-mono">{match.entry.path}</span>
        {!isFolder && <span className="shrink-0 text-xs text-muted-foreground">{formatSize(match.entry.size)}</span>}
      </button>
    </li>
  )
}

/** Recursively searches a folder of a site for files and folders whose name matches a query, and lets a match be opened. */
export function SearchDialog({ workspaceId, siteId, path, onOpen, onClose }: SearchDialogProps) {
  const { t } = useTranslation('explorer')
  const api = useApi()
  const search = useSearch(api, workspaceId, siteId)
  const running = search.phase === 'running'

  const submit = (event: FormEvent): void => {
    event.preventDefault()
    if (search.query.trim() !== '') {
      search.start(path)
    }
  }

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.search.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.search.subtitle, { path })}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} noValidate className="flex gap-2">
          <Input aria-label={t(($) => $.search.query)} placeholder={t(($) => $.search.placeholder)} value={search.query} onChange={(event) => search.setQuery(event.target.value)} autoFocus className="font-mono" />
          {running ? (
            <Button type="button" variant="outline" onClick={search.stop}>
              {t(($) => $.search.stop)}
            </Button>
          ) : (
            <Button type="submit" disabled={search.query.trim() === ''}>
              {t(($) => $.search.start)}
            </Button>
          )}
        </form>
        <p className="text-xs text-muted-foreground">{t(($) => $.search.hint)}</p>
        {running && <p role="status" className="text-sm text-muted-foreground">{t(($) => $.search.scanning, { count: search.scanned })}</p>}
        {search.phase === 'done' && (
          <p role="status" className="text-sm text-muted-foreground">
            {search.summary?.canceled ? t(($) => $.search.canceled, { count: search.matches.length }) : t(($) => $.search.found, { count: search.matches.length })}
            {search.summary?.truncated && ` ${t(($) => $.search.truncated)}`}
          </p>
        )}
        {search.matches.length > 0 && (
          <div className="max-h-[360px] overflow-y-auto overflow-x-hidden rounded-lg border bg-card">
            <ul aria-label={t(($) => $.search.results)}>
              {search.matches.map((match) => (
                <MatchRow
                  key={match.entry.path}
                  match={match}
                  onOpen={() => {
                    onOpen(match.entry.type === 'directory' ? match.entry.path : parentPath(match.entry.path))
                    onClose()
                  }}
                />
              ))}
            </ul>
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.cancel)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
