import { KeyRound } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'

import { CredentialDialog, type CredentialTarget } from './CredentialDialog'

/** Props of {@link CredentialNotice}. */
export interface CredentialNoticeProps {
  /** Workspace of the site. */
  workspaceId: string
  /** The site that asks for its password. */
  target: CredentialTarget
  /** Called once the password is held for the session, so the caller can connect again. */
  onGiven: () => void
  /** Whether the server refused the password given before, so it is asked for again with that said. */
  refused?: boolean
}

/**
 * A card centered in the explorer for a site that asks for its password when connecting (ADR-0010). The password dialog opens at once, as
 * FileZilla asks for it; the card stays behind in case it is dismissed, with a button that opens it again.
 */
export function CredentialNotice({ workspaceId, target, onGiven, refused = false }: CredentialNoticeProps) {
  const { t } = useTranslation('sites')
  const [open, setOpen] = useState(true)

  return (
    <>
      <PanelState icon={KeyRound} title={t(($) => $.credential.cardTitle)} description={t(($) => $.credential.cardBody)}>
        <div className="flex max-w-full flex-col items-center gap-0.5 rounded-lg border bg-muted/40 px-4 py-2">
          <span className="max-w-full truncate text-sm font-medium">{target.name}</span>
          <span className="max-w-full truncate font-mono text-xs text-muted-foreground">
            {target.username}@{target.host}
          </span>
        </div>
        <Button type="button" size="lg" className="min-w-52" onClick={() => setOpen(true)}>
          <KeyRound />
          {t(($) => $.credential.cardAction)}
        </Button>
      </PanelState>
      <CredentialDialog
        workspaceId={workspaceId}
        target={open ? target : null}
        refused={refused}
        onClose={() => setOpen(false)}
        onGiven={() => {
          setOpen(false)
          onGiven()
        }}
      />
    </>
  )
}
