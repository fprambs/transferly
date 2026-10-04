import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'

import type { QuickConnection } from '@/lib/api'
import { useDialogsStore } from '@/state/dialogs-store'

import { useSitesData, sitesKeys } from '../sites/use-sites-data'
import { useWorkspace } from '../workspace/use-workspace'

import { KeepPrompt } from './KeepPrompt'
import { QuickConnectDialog } from './QuickConnectDialog'
import { SaveSiteDialog } from './SaveSiteDialog'
import { hostOf } from '../connectors/settings'

/** A quick connection that worked, waiting for the user to keep it or not. */
interface Offer {
  /** The open connection. */
  connection: QuickConnection
  /** How long connecting took, in milliseconds. */
  elapsedMs: number
  /** Workspace the connection belongs to. */
  workspaceId: string
}

/** Tells whether a key press is the quick connect shortcut, ⌘⇧K or Ctrl+Shift+K. */
function isQuickShortcut(event: KeyboardEvent): boolean {
  return event.shiftKey && (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k'
}

/** Runs the whole quick connect journey: the shortcut, the dialog, the offer to keep the server and saving it. */
export function QuickConnectFlow() {
  const queryClient = useQueryClient()
  const workspace = useWorkspace()
  const { collections } = useSitesData(workspace?.id)
  const open = useDialogsStore((state) => state.quickConnectOpen)
  const setOpen = useDialogsStore((state) => state.setQuickConnectOpen)
  const [offer, setOffer] = useState<Offer | null>(null)
  const [saving, setSaving] = useState<Offer | null>(null)

  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => {
      if (isQuickShortcut(event)) {
        event.preventDefault()
        setOpen(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [setOpen])

  return (
    <>
      <QuickConnectDialog
        open={open}
        workspaceId={workspace?.id}
        onOpenChange={setOpen}
        onConnected={(connection, elapsedMs, id) => {
          setOffer({ connection, elapsedMs, workspaceId: id })
          void queryClient.invalidateQueries({ queryKey: sitesKeys.quick(id) })
        }}
      />
      {offer !== null && saving === null && (
        <KeepPrompt
          host={hostOf(offer.connection)}
          elapsedMs={offer.elapsedMs}
          onSave={() => setSaving(offer)}
          onDismiss={() => setOffer(null)}
        />
      )}
      {saving !== null && (
        <SaveSiteDialog
          connection={saving.connection}
          workspaceId={saving.workspaceId}
          collections={collections}
          onClose={() => setSaving(null)}
          onSaved={() => {
            setSaving(null)
            setOffer(null)
            void queryClient.invalidateQueries({ queryKey: ['workspaces', saving.workspaceId] })
          }}
        />
      )}
    </>
  )
}
