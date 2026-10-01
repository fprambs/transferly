import type { Site } from '@/lib/api'
import { cn } from '@/lib/utils'

/** How a site reaches its server, as far as the user can tell from its icon. */
export type Transport = 'sftp' | 'ftps' | 'optional' | 'plain'

/** Tells the transport of a site: SFTP, FTP over TLS, FTP that uses TLS only when the server offers it, or FTP without encryption. */
export function transportOf(site: Pick<Site, 'protocol' | 'ftpEncryption'>): Transport {
  if (site.protocol === 'sftp') {
    return 'sftp'
  }
  if (site.protocol === 'ftps' || site.ftpEncryption === 'explicit' || site.ftpEncryption === 'implicit') {
    return 'ftps'
  }
  return site.ftpEncryption === 'plain' ? 'plain' : 'optional'
}

/** Color of the accent of each transport: the brand color when encrypted, a warning when encryption may be skipped, danger when off. */
const accents: Record<Transport, string> = {
  sftp: 'text-primary',
  ftps: 'text-primary',
  optional: 'text-warn',
  plain: 'text-destructive',
}

/** Props of {@link ProtocolIcon}. */
export interface ProtocolIconProps {
  /** The transport to draw. */
  transport: Transport
  /** Classes for the size and placement of the icon. */
  className?: string
}

/**
 * Icon of a site's protocol from the brand kit (`branding/icons`): a server whose accent tells the encryption, a lock for SFTP, a
 * shield for FTP over TLS and a crossed circle for FTP without it. It is decorative; the site's name and host carry the meaning.
 */
export function ProtocolIcon({ transport, className }: ProtocolIconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      data-transport={transport}
      className={cn('size-4 shrink-0 text-muted-foreground', className)}
    >
      <rect x="3" y="3" width="15" height="7" rx="2" />
      {transport === 'plain' ? <rect x="3" y="12" width="9" height="7" rx="2" /> : <path d="M3 15.5v-1A2.5 2.5 0 0 1 5.5 12H11" />}
      <path d="M6.5 6.5h.01M6.5 15.5h.01" strokeWidth="2.25" />
      <g stroke="currentColor" className={accents[transport]}>
        {transport === 'sftp' ? (
          <>
            <rect x="13" y="15" width="8" height="6" rx="1.5" />
            <path d="M15 15v-1.5a2 2 0 0 1 4 0V15" />
          </>
        ) : transport === 'plain' ? (
          <>
            <circle cx="17.5" cy="17" r="3.5" />
            <path d="M15 19.5l5-5" />
          </>
        ) : (
          <path d="M17 13l4 1.5V17c0 2.2-1.7 3.6-4 4.3-2.3-.7-4-2.1-4-4.3v-2.5z" />
        )}
      </g>
    </svg>
  )
}
