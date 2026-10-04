import { cn } from '@/lib/utils'

/** The strokes of each connector icon, from `branding/icons`: the outline in the color of the text and the accent in the brand color. */
const shapes: Readonly<Record<string, { base: string; accent: string }>> = {
  sftp: {
    base: 'M5 3h11a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM3 15.5v-1A2.5 2.5 0 0 1 5.5 12H11M6.5 6.5h.01M6.5 15.5h.01',
    accent: 'M14.5 15h5a1.5 1.5 0 0 1 1.5 1.5v3a1.5 1.5 0 0 1-1.5 1.5h-5a1.5 1.5 0 0 1-1.5-1.5v-3a1.5 1.5 0 0 1 1.5-1.5zM15 15v-1.5a2 2 0 0 1 4 0V15',
  },
  ftps: {
    base: 'M5 3h11a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM3 15.5v-1A2.5 2.5 0 0 1 5.5 12H11M6.5 6.5h.01M6.5 15.5h.01',
    accent: 'M17 13l4 1.5V17c0 2.2-1.7 3.6-4 4.3-2.3-.7-4-2.1-4-4.3v-2.5z',
  },
  cloud: {
    base: 'M7 18a4 4 0 0 1-.6-7.95A6 6 0 0 1 18 9a4.5 4.5 0 0 1-.5 9H14',
    accent: 'M10 21v-7M7.5 16.5 10 14l2.5 2.5',
  },
  gcs: {
    base: 'M7 16a4 4 0 0 1-.6-7.95A6 6 0 0 1 18 7a4.5 4.5 0 0 1-.5 9',
    accent: 'M8 13h8M8 16.5h8M8 20h8',
  },
}

/** The icon of a connector whose descriptor names an icon the interface does not know: a server, with the accent of a plug. */
const fallback = {
  base: 'M5 3h11a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM3 15.5v-1A2.5 2.5 0 0 1 5.5 12H11M6.5 6.5h.01M6.5 15.5h.01',
  accent: 'M15 13v3M19 13v3M14 16h6v1.5a3 3 0 0 1-6 0zM17 20.5V22',
}

/** Props of {@link ConnectorIcon}. */
export interface ConnectorIconProps {
  /** The icon the descriptor of the connector names, such as sftp, ftps or cloud. */
  icon: string
  /** Classes for the size, placement and outline color of the icon. */
  className?: string
}

/** The two-tone icon of a connector from the brand kit, as the catalog and the store show it. It is decorative. */
export function ConnectorIcon({ icon, className }: ConnectorIconProps) {
  const { base, accent } = shapes[icon] ?? fallback
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      data-icon={shapes[icon] === undefined ? 'connector' : icon}
      className={cn('size-5 shrink-0', className)}
    >
      <path d={base} />
      <path d={accent} className="text-primary" />
    </svg>
  )
}
