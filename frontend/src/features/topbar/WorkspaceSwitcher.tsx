import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useUiStore } from '@/state/ui-store'

import { useWorkspace, useWorkspaces } from '../workspace/use-workspace'

/** Drop-down of the top bar that shows the workspace in use and switches to another. */
export function WorkspaceSwitcher() {
  const { t } = useTranslation('shell')
  const workspaces = useWorkspaces()
  const current = useWorkspace()
  const setWorkspace = useUiStore((state) => state.setWorkspace)

  if (current === undefined) {
    return null
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t(($) => $.topbar.workspace)}
        className="flex h-[30px] items-center gap-2 rounded-lg border bg-muted/50 px-2.5 text-[13px] font-medium"
      >
        {current.name}
        <ChevronDown className="size-3 text-faint" />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="min-w-48">
        {workspaces.map((workspace) => (
          <DropdownMenuItem key={workspace.id} onClick={() => setWorkspace(workspace.id)}>
            {workspace.name}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
