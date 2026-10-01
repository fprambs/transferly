import { cn } from '@/lib/utils'

/** The icons of the brand kit the interface draws. */
export type BrandIconName = 'transfer' | 'logs' | 'sites'

/** The strokes of each icon, from `branding/icons`: the outline in the color of the text and the accent in the brand color. */
const shapes: Record<BrandIconName, { base: string; accent: string }> = {
  transfer: { base: 'M4 8h14M14 4l4 4-4 4', accent: 'M20 16H6M10 12l-4 4 4 4' },
  logs: { base: 'M5.5 4h13A2.5 2.5 0 0 1 21 6.5v11a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5v-11A2.5 2.5 0 0 1 5.5 4zM3 8h18', accent: 'M7 12.5l2.5 2-2.5 2M12 16.5h5' },
  sites: {
    base: 'M10 3h4a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1h-4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1zM4 16h4a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-3a1 1 0 0 1 1-1zM16 16h4a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1h-4a1 1 0 0 1-1-1v-3a1 1 0 0 1 1-1z',
    accent: 'M12 8v4M6 16v-2a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v2',
  },
}

/** Props of {@link BrandIcon}. */
export interface BrandIconProps {
  /** Which icon to draw. */
  name: BrandIconName
  /** Classes for the size, placement and outline color of the icon. */
  className?: string
}

/** A two-tone icon of the brand kit, for the headings of the main panels; Lucide draws the rest. It is decorative. */
export function BrandIcon({ name, className }: BrandIconProps) {
  const { base, accent } = shapes[name]
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      data-icon={name}
      className={cn('size-4 shrink-0', className)}
    >
      <path d={base} />
      <path d={accent} className="text-primary" />
    </svg>
  )
}
