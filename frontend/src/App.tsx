import { QueryClientProvider } from '@tanstack/react-query'

import { TooltipProvider } from '@/components/ui/tooltip'
import { RouterProvider, type RouterHistory } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

import { createQueryClient } from '@/app/query'
import { createAppRouter } from '@/app/router'
import { bindSessionExpiry } from '@/app/session-expired'
import { useApi } from '@/lib/api/use-api'
import { useTransport } from '@/lib/transport'
import { useThemeSync } from '@/lib/theme/use-theme'

/** Props of {@link App}. */
export interface AppProps {
  /** History the router navigates in; the browser history when omitted, a memory one in tests. */
  history?: RouterHistory
}

/** Application root: keeps the color scheme in sync and renders the routes. */
export default function App({ history }: AppProps) {
  useThemeSync()
  const api = useApi()
  const transport = useTransport()
  const [queryClient] = useState(createQueryClient)
  const [router] = useState(() => createAppRouter({ queryClient, api, transport }, history))

  useEffect(() => bindSessionExpiry(transport, queryClient, router), [transport, queryClient, router])

  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}
