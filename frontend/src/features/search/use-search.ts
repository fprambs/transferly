import { useRef, useState } from 'react'

import type { Api } from '@/lib/api/use-api'

import { matcherFor } from './match'
import { searchTree, type SearchMatch, type SearchSummary } from './walk'

/** How a search stands. */
export type SearchPhase = 'idle' | 'running' | 'done'

/** A recursive search of one folder of a site, and everything the interface needs to run and show it. */
export interface Search {
  /** The query typed so far. */
  query: string
  /** Sets the query. */
  setQuery: (query: string) => void
  /** How the search stands. */
  phase: SearchPhase
  /** The matches found so far, in the order they were found. */
  matches: SearchMatch[]
  /** How many entries were looked at so far; updates as the search runs, not only once it ends. */
  scanned: number
  /** Why the last search ended, once it has. */
  summary: SearchSummary | null
  /** Starts a search for the current query below `path`; a search already running is stopped first. */
  start: (path: string) => void
  /** Stops a running search; the matches found so far are kept. */
  stop: () => void
}

/** How many entries pass between two progress updates, so a huge tree does not re-render the interface for every one of them. */
const progressEvery = 25

/** Runs recursive searches of the folders of one site, one at a time. */
export function useSearch(api: Api, workspaceId: string, siteId: string): Search {
  const [query, setQuery] = useState('')
  const [phase, setPhase] = useState<SearchPhase>('idle')
  const [matches, setMatches] = useState<SearchMatch[]>([])
  const [scanned, setScanned] = useState(0)
  const [summary, setSummary] = useState<SearchSummary | null>(null)
  const controller = useRef<AbortController | null>(null)

  const stop = (): void => controller.current?.abort()

  const start = (path: string): void => {
    stop()
    const signal = new AbortController()
    controller.current = signal
    setPhase('running')
    setMatches([])
    setScanned(0)
    setSummary(null)
    const matcher = matcherFor(query)
    const onProgress = (count: number): void => {
      if (count % progressEvery === 0) {
        setScanned(count)
      }
    }
    void searchTree((folder) => api.files.list(workspaceId, siteId, folder, signal.signal), path, matcher, (match) => setMatches((previous) => [...previous, match]), signal.signal, onProgress).then((result) => {
      setScanned(result.scanned)
      setSummary(result)
      setPhase('done')
    })
  }

  return { query, setQuery, phase, matches, scanned, summary, start, stop }
}
