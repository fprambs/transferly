import { cn } from '@/lib/utils'

import { ConnectorIcon } from './ConnectorIcon'
import type { Transport } from './transport'

/** Color of the badge of each transport that is not always encrypted: a warning when encryption may be skipped, danger when off. */
const badges: Partial<Record<Transport, string>> = {
  optional: 'bg-warn',
  plain: 'bg-destructive',
}

/** Props of {@link SiteIcon}. */
export interface SiteIconProps {
  /** The icon the descriptor of the site's connector names. */
  icon: string
  /** How the site reaches its server. */
  transport: Transport
  /** Classes for the size and placement of the icon. */
  className?: string
}

/**
 * The icon of a saved site: the icon of its connector, with a badge when its connection may travel unencrypted, amber when FTP only
 * uses TLS if the server offers it and red when encryption is off. It is decorative; the site's name and host carry the meaning.
 */
export function SiteIcon({ icon, transport, className }: SiteIconProps) {
  const badge = badges[transport]
  return (
    <span data-transport={transport} className={cn('relative inline-flex size-4 shrink-0', className)}>
      <ConnectorIcon icon={icon} className="size-full" />
      {badge !== undefined && <span data-badge="" className={cn('absolute -end-0.5 -bottom-0.5 size-[7px] rounded-full ring-2 ring-card', badge)} />}
    </span>
  )
}
