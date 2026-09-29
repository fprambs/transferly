import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import type { Member, Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { useUiStore } from '@/state/ui-store'

import { workspacesKey } from '../workspace/use-workspace'

import { teamErrorKeyOf, type TeamErrorKey } from './errors'
import { TeamFailure } from './TeamMessage'
import { teamKeys } from './use-teams'

/** Props shared by the dialogs that act on a team workspace. */
interface TeamDialogProps {
  /** The team workspace. */
  workspace: Workspace
  /** Called when the dialog is dismissed or the action is done. */
  onClose: () => void
}

/** Runs an action of a dialog, keeping its failure and busy state; the dialog closes when it succeeds. */
function useAction(onClose: () => void) {
  const [failure, setFailure] = useState<TeamErrorKey | null>(null)
  const [busy, setBusy] = useState(false)
  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    try {
      await action()
      onClose()
    } catch (error) {
      setFailure(teamErrorKeyOf(error))
      setBusy(false)
    }
  }
  return { failure, busy, run }
}

/** Props of {@link RemoveMemberDialog}. */
export interface RemoveMemberDialogProps {
  /** The team workspace. */
  workspace: Workspace
  /** The member to remove; the dialog is closed when there is none. */
  member: Member | null
  /** Called when the dialog is dismissed or the member is gone. */
  onClose: () => void
}

/** Text and buttons of the removal dialog. */
function RemoveBody({ workspace, member, onClose }: TeamDialogProps & { member: Member }) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const queryClient = useQueryClient()
  const { failure, busy, run } = useAction(onClose)

  return (
    <div className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.remove.title, { email: member.email })}</DialogTitle>
        <DialogDescription>{t(($) => $.remove.body, { email: member.email, name: workspace.name })}</DialogDescription>
      </DialogHeader>
      <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2.5 text-sm leading-relaxed">
        <p className="mb-1 font-semibold">{t(($) => $.remove.rotation)}</p>
        {t(($) => $.remove.rotationBody)}
      </div>
      <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm leading-relaxed">{t(($) => $.remove.change)}</p>
      <TeamFailure failure={failure} />
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t(($) => $.remove.cancel)}
        </Button>
        <Button
          variant="destructive"
          disabled={busy}
          onClick={() =>
            run(async () => {
              await api.workspaces.removeMember(workspace.id, member.userId)
              await queryClient.invalidateQueries({ queryKey: teamKeys.members(workspace.id) })
            })
          }
        >
          {t(($) => $.remove.confirm)}
        </Button>
      </DialogFooter>
    </div>
  )
}

/** Asks for confirmation before removing a member, and explains that the key of the workspace is rotated. */
export function RemoveMemberDialog({ member, ...rest }: RemoveMemberDialogProps) {
  return (
    <Dialog open={member !== null} onOpenChange={rest.onClose}>
      <DialogContent className="sm:max-w-[460px]">{member !== null && <RemoveBody member={member} {...rest} />}</DialogContent>
    </Dialog>
  )
}

/** Props of {@link TransferDialog}. */
export interface TransferDialogProps {
  /** The team workspace. */
  workspace: Workspace
  /** The members that could become the owner; the dialog is closed when it is `null`. */
  candidates: Member[] | null
  /** The member the choice starts on. */
  initial?: string
  /** Called when the dialog is dismissed or the ownership moved. */
  onClose: () => void
}

/** Form of the transfer dialog. */
function TransferBody({ workspace, candidates, initial, onClose }: TransferDialogProps & { candidates: Member[] }) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const queryClient = useQueryClient()
  const { failure, busy, run } = useAction(onClose)
  const [userId, setUserId] = useState(initial ?? candidates[0]?.userId ?? '')
  const [confirmation, setConfirmation] = useState('')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    void run(async () => {
      await api.workspaces.transfer(workspace.id, userId)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: workspacesKey }),
        queryClient.invalidateQueries({ queryKey: teamKeys.members(workspace.id) }),
      ])
    })
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.transfer.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.transfer.body)}</DialogDescription>
      </DialogHeader>
      {candidates.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.transfer.noCandidates)}</p>
      ) : (
        <>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="transfer-owner">{t(($) => $.transfer.newOwner)}</Label>
            <Select
              id="transfer-owner"
              value={userId}
              options={candidates.map((member) => ({ value: member.userId, label: `${member.email} · ${t(($) => $.roles[member.role])}` }))}
              onValueChange={setUserId}
            />
            <p className="text-xs text-muted-foreground">{t(($) => $.transfer.onlySealed)}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="transfer-confirm">{t(($) => $.transfer.confirmLabel)}</Label>
            <Input id="transfer-confirm" value={confirmation} placeholder={workspace.name} autoComplete="off" onChange={(event) => setConfirmation(event.target.value)} />
          </div>
        </>
      )}
      <TeamFailure failure={failure} />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.transfer.cancel)}
        </Button>
        <Button type="submit" variant="destructive" disabled={busy || candidates.length === 0 || confirmation !== workspace.name}>
          {t(($) => $.transfer.confirm)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Hands a team workspace to another member, after the owner types its name. */
export function TransferDialog({ candidates, ...rest }: TransferDialogProps) {
  return (
    <Dialog open={candidates !== null} onOpenChange={rest.onClose}>
      <DialogContent className="sm:max-w-[460px]">{candidates !== null && <TransferBody candidates={candidates} {...rest} />}</DialogContent>
    </Dialog>
  )
}

/** Props of {@link LeaveDialog}. */
export interface LeaveDialogProps {
  /** The team workspace to leave; the dialog is closed when there is none. */
  workspace: Workspace | null
  /** Called when the dialog is dismissed or the user has left. */
  onClose: () => void
}

/** Text and buttons of the leave dialog. */
function LeaveBody({ workspace, onClose }: TeamDialogProps) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const clearWorkspace = useUiStore((state) => state.clearWorkspace)
  const { failure, busy, run } = useAction(onClose)

  return (
    <div className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.leave.title, { name: workspace.name })}</DialogTitle>
        <DialogDescription>{t(($) => $.leave.body)}</DialogDescription>
      </DialogHeader>
      <TeamFailure failure={failure} />
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t(($) => $.leave.cancel)}
        </Button>
        <Button
          variant="destructive"
          disabled={busy}
          onClick={() =>
            run(async () => {
              await api.workspaces.leave(workspace.id)
              clearWorkspace()
              await queryClient.invalidateQueries({ queryKey: workspacesKey })
              await navigate({ to: '/' })
            })
          }
        >
          {t(($) => $.leave.confirm)}
        </Button>
      </DialogFooter>
    </div>
  )
}

/** Asks for confirmation before the user leaves a team workspace, then goes back to the personal one. */
export function LeaveDialog({ workspace, onClose }: LeaveDialogProps) {
  return (
    <Dialog open={workspace !== null} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[420px]">{workspace !== null && <LeaveBody workspace={workspace} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
