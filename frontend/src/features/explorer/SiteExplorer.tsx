import { getRouteApi } from '@tanstack/react-router'

import { useWorkspace } from '../workspace/use-workspace'

import { Explorer } from './Explorer'

/** Access to the address of the site route: its site and folder. */
const siteRoute = getRouteApi('/shell/sites/$siteId')

/** Explorer of the site in the address, at the folder in its `path` search parameter. */
export function SiteExplorer() {
  const { siteId } = siteRoute.useParams()
  const { path } = siteRoute.useSearch()
  const navigate = siteRoute.useNavigate()
  const workspace = useWorkspace()

  if (workspace === undefined) {
    return null
  }
  return (
    <Explorer
      workspaceId={workspace.id}
      siteId={siteId}
      path={path}
      onNavigate={(next) => void navigate({ search: { path: next } })}
    />
  )
}
