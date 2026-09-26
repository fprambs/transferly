import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useApi } from '@/lib/api/use-api'
import { useUiStore } from '@/state/ui-store'

import { workspacesKey } from '../workspace/use-workspace'

import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { teamErrorKeyOf, type TeamErrorKey } from './errors'
import { TeamFailure } from './TeamMessage'

/** Props of {@link NewTeamDialog}. */
export interface NewTeamDialogProps {
  /** Whether the dialog is open. */
  open: boolean
  /** Called when the dialog is dismissed or the workspace was created. */
  onClose: () => void
}

/** Form of the dialog; it is mounted only while the dialog is open, so it starts empty every time. */
function NewTeamForm({ onClose }: Pick<NewTeamDialogProps, 'onClose'>) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const setWorkspace = useUiStore((state) => state.setWorkspace)
  const [name, setName] = useState('')
  const [failure, setFailure] = useState<TeamErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (name.trim() === '') {
      setFailure('nameRequired')
      return
    }
    setBusy(true)
    try {
      const created = await api.workspaces.create(name.trim())
      await queryClient.invalidateQueries({ queryKey: workspacesKey })
      setWorkspace(created.id)
      onClose()
      await navigate({ to: '/members' })
    } catch (error) {
      setFailure(teamErrorKeyOf(error))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.newTeam.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.newTeam.body)}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="team-name">{t(($) => $.newTeam.name)}</Label>
        <Input id="team-name" value={name} maxLength={80} autoFocus onChange={(event) => setName(event.target.value)} />
      </div>
      {failure === 'vault_locked' ? <VaultLockedNotice need="team" onUnlocked={() => setFailure(null)} /> : <TeamFailure failure={failure} />}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.newTeam.cancel)}
        </Button>
        <Button type="submit" disabled={busy}>
          {t(($) => $.newTeam.create)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Asks for the name of a new team workspace and opens its members page once it exists. */
export function NewTeamDialog({ open, onClose }: NewTeamDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[420px]">{open && <NewTeamForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
