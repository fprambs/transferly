import { useNavigate } from '@tanstack/react-router'
import { ChevronDown } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useUiStore } from '@/state/ui-store'

import { canManageMembers } from '../teams/roles'
import { NewTeamDialog } from '../teams/NewTeamDialog'
import { useWorkspace, useWorkspaces } from '../workspace/use-workspace'

/** Drop-down of the top bar that shows the workspace in use, switches to another, opens its members and starts a new team. */
export function WorkspaceSwitcher() {
  const { t } = useTranslation('shell')
  const { t: teams } = useTranslation('teams')
  const workspaces = useWorkspaces()
  const current = useWorkspace()
  const setWorkspace = useUiStore((state) => state.setWorkspace)
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)

  if (current === undefined) {
    return null
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          aria-label={t(($) => $.topbar.workspace)}
          className="flex h-[30px] items-center gap-2 rounded-lg border bg-muted/50 px-2.5 text-[13px] font-medium"
        >
          {current.name}
          <ChevronDown className="size-3 text-faint" />
        </DropdownMenuTrigger>
        <DropdownMenuContent className="min-w-56">
          {workspaces.map((workspace) => (
            <DropdownMenuItem key={workspace.id} className="justify-between gap-4" onClick={() => setWorkspace(workspace.id)}>
              {workspace.name}
              {workspace.kind === 'team' && <span className="text-xs text-muted-foreground">{teams(($) => $.roles[workspace.role])}</span>}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          {current.kind === 'team' && <DropdownMenuItem onClick={() => void navigate({ to: '/members' })}>{teams(($) => $.page.label)}</DropdownMenuItem>}
          {current.kind === 'team' && canManageMembers(current.role) && (
            <DropdownMenuItem onClick={() => void navigate({ to: '/audit' })}>{teams(($) => $.audit.label)}</DropdownMenuItem>
          )}
          <DropdownMenuItem onClick={() => setCreating(true)}>{teams(($) => $.page.createTeam)}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <NewTeamDialog open={creating} onClose={() => setCreating(false)} />
    </>
  )
}
