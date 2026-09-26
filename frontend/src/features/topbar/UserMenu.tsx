import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { sessionKey } from '@/app/session'
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useApi } from '@/lib/api/use-api'
import { useTransport } from '@/lib/transport'

import { useSession } from '../auth/use-session'

/** Builds the initials shown in the avatar from an email address. */
function initialsOf(email: string): string {
  return email.slice(0, 2).toUpperCase()
}

/** Avatar of the top bar with the account menu: who is signed in and how to sign out. */
export function UserMenu() {
  const { t } = useTranslation('shell')
  const { t: settings } = useTranslation('settings')
  const api = useApi()
  const transport = useTransport()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const session = useSession()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t(($) => $.topbar.account)}
        className="flex size-7 items-center justify-center rounded-full border bg-muted text-[11px] font-semibold text-muted-foreground"
      >
        {initialsOf(session?.user.email ?? '')}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="truncate">{session?.user.email}</DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => void navigate({ to: '/settings' })}>{settings(($) => $.menu.settings)}</DropdownMenuItem>
        <DropdownMenuItem onClick={() => void navigate({ to: '/servers' })}>{settings(($) => $.menu.servers)}</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={async () => {
            await api.auth.logout()
            transport.setCsrfToken(undefined)
            queryClient.clear()
            queryClient.setQueryData(sessionKey, null)
            await navigate({ to: '/login' })
          }}
        >
          {t(($) => $.topbar.signOut)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
