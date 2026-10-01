import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { sitesKeys } from './use-sites-data'

/** Props of {@link DeleteSiteDialog}. */
export interface DeleteSiteDialogProps {
  /** The site to delete; the dialog is closed when there is none. */
  site: Site | null
  /** Workspace the site belongs to. */
  workspaceId: string
  /** Called when the dialog is dismissed or the site is gone. */
  onClose: () => void
}

/** Props of {@link DeleteBody}. */
interface DeleteBodyProps extends Omit<DeleteSiteDialogProps, 'site'> {
  /** The site to delete. */
  site: Site
}

/** Confirmation text and buttons of the delete dialog. */
function DeleteBody({ site, workspaceId, onClose }: DeleteBodyProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(($) => $.site.deleteTitle)}</DialogTitle>
        <DialogDescription>{t(($) => $.site.deleteBody, { name: site.name })}</DialogDescription>
      </DialogHeader>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t(($) => $.site.cancel)}
        </Button>
        <Button
          variant="destructive"
          onClick={async () => {
            try {
              await api.sites.remove(workspaceId, site.id)
              await queryClient.invalidateQueries({ queryKey: sitesKeys.sites(workspaceId) })
              onClose()
            } catch (error) {
              setFailure(siteErrorKeyOf(error))
            }
          }}
        >
          {t(($) => $.site.deleteConfirm)}
        </Button>
      </DialogFooter>
    </>
  )
}

/** Asks for confirmation before deleting a site and its stored credentials. */
export function DeleteSiteDialog({ site, ...rest }: DeleteSiteDialogProps) {
  return (
    <Dialog open={site !== null} onOpenChange={rest.onClose}>
      <DialogContent className="sm:max-w-[420px]">{site !== null && <DeleteBody site={site} {...rest} />}</DialogContent>
    </Dialog>
  )
}
