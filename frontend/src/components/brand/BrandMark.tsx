/** Props of {@link BrandMark}. */
export interface BrandMarkProps {
  /** Classes for the size and placement of the mark. */
  className?: string
}

/**
 * The Transferly mark: a return arrow whose lower bar flows into a ribbon that draws the T, turns at the bottom and climbs back as an
 * upload arrow (see `branding/`). It is decorative; the product name is always written next to it. Its colors are the brand colors of
 * `--brand-arrow` and `--brand-ribbon`, which do not change with the theme.
 */
export function BrandMark({ className }: BrandMarkProps) {
  return (
    <svg viewBox="5 6 88 84" fill="none" aria-hidden="true" className={className}>
      <g className="text-(--brand-arrow)">
        <path d="M32.5 18H21.25a9.5 9.5 0 0 0 0 19H32" stroke="currentColor" strokeWidth="7" />
        <path d="M31.5 9.5 47 18.25 31.5 27z" fill="currentColor" />
      </g>
      <g className="text-(--brand-ribbon)">
        <path
          d="M57.5 37.5h19.25a9.75 9.75 0 0 0 0-19.5H60C50 18 45 37 31 37M43 32c-2.5 3-4 6-4 12v29.75a9.75 9.75 0 0 0 19.5 0V59"
          stroke="currentColor"
          strokeWidth="7"
        />
        <path d="M49.5 59.5 58.5 45l9 14.5z" fill="currentColor" />
      </g>
    </svg>
  )
}
