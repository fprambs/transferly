import { cn } from '@/lib/utils'

/** The scenes the empty states can show. */
export type Scene = 'start' | 'folder' | 'transfers' | 'sites'

/** Props of {@link Illustration}. */
export interface IllustrationProps {
  /** What the empty state is about. */
  scene: Scene
  /** Classes for the size and placement of the drawing. */
  className?: string
}

/** Classes of the three inks of every scene: the outline that sets the place, and the two brand colors that move through it. */
const ink = {
  base: 'text-faint',
  arrow: 'text-(--brand-arrow)',
  ribbon: 'text-(--brand-ribbon)',
} as const

/** The drawing of each scene on a 120 by 80 grid, in the stroke of the brand icons. */
function drawing(scene: Scene) {
  switch (scene) {
    case 'start':
      return (
        <>
          <circle cx="60" cy="40" r="34" className={ink.base} strokeDasharray="3 6" />
          <path d="M44 30h-6a5 5 0 0 0 0 10h12" className={ink.arrow} />
          <path d="M42 25l6 5-6 5" className={ink.arrow} />
          <path d="M66 40h9a5 5 0 0 0 0-10h-8c-6 0-9 5-15 10M58 36v22a5 5 0 0 0 10 0v-7" className={ink.ribbon} />
          <path d="M63 54l5-6 5 6" className={ink.ribbon} />
        </>
      )
    case 'folder':
      return (
        <>
          <path d="M22 26a5 5 0 0 1 5-5h18l7 7h41a5 5 0 0 1 5 5v31a5 5 0 0 1-5 5H27a5 5 0 0 1-5-5z" className={ink.base} />
          <path d="M42 50h36" className={ink.base} strokeDasharray="3 5" />
          <path d="M60 58V34M51 43l9-9 9 9" className={ink.ribbon} />
          <path d="M84 16l4 4M92 12v6M96 22h-6" className={ink.arrow} />
        </>
      )
    case 'transfers':
      return (
        <>
          <path d="M14 30h92M14 52h92" className={ink.base} strokeDasharray="2 7" />
          <path d="M24 30h52M68 22l8 8-8 8" className={ink.ribbon} />
          <path d="M96 52H44M52 44l-8 8 8 8" className={ink.arrow} />
        </>
      )
    case 'sites':
      return (
        <>
          <rect x="16" y="22" width="34" height="14" rx="4" className={ink.base} />
          <rect x="16" y="44" width="34" height="14" rx="4" className={ink.base} />
          <path d="M23 29h.01M23 51h.01" className={ink.base} strokeWidth="3.5" />
          <path d="M50 40h22" className={ink.base} strokeDasharray="2 5" />
          <rect x="72" y="26" width="32" height="28" rx="8" className={ink.ribbon} />
          <path d="M88 33v14M81 40h14" className={ink.ribbon} />
          <path d="M60 18l4 4M68 14v6" className={ink.arrow} />
        </>
      )
  }
}

/** A small drawing for an empty state, in the colors of the brand, so an empty place still looks like Transferly. It is decorative. */
export function Illustration({ scene, className }: IllustrationProps) {
  return (
    <svg
      viewBox="0 0 120 80"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      data-scene={scene}
      className={cn('h-20 w-30 shrink-0', className)}
    >
      {drawing(scene)}
    </svg>
  )
}
