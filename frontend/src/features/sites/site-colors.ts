import type { SiteColor } from '@/lib/api'

/** The label colors a site may wear, in the order the site dialog offers them (ADR-0021). */
export const siteColors: readonly SiteColor[] = ['red', 'orange', 'yellow', 'green', 'teal', 'blue', 'violet', 'pink']

/** The fill of each label color, from the `--label-*` tones of `index.css`, which follow the light or dark scheme. */
const fills: Record<SiteColor, string> = {
  red: 'bg-(--label-red)',
  orange: 'bg-(--label-orange)',
  yellow: 'bg-(--label-yellow)',
  green: 'bg-(--label-green)',
  teal: 'bg-(--label-teal)',
  blue: 'bg-(--label-blue)',
  violet: 'bg-(--label-violet)',
  pink: 'bg-(--label-pink)',
}

/** The top edge each label color draws on a tab. */
const edges: Record<SiteColor, string> = {
  red: 'border-t-(--label-red)',
  orange: 'border-t-(--label-orange)',
  yellow: 'border-t-(--label-yellow)',
  green: 'border-t-(--label-green)',
  teal: 'border-t-(--label-teal)',
  blue: 'border-t-(--label-blue)',
  violet: 'border-t-(--label-violet)',
  pink: 'border-t-(--label-pink)',
}

/** The fill class of a label color, or `undefined` for a site without one. */
export function colorFill(color: SiteColor | undefined): string | undefined {
  return color === undefined ? undefined : fills[color]
}

/** The class that draws a label color as the top edge of a tab, or `undefined` for a site without one. */
export function colorEdge(color: SiteColor | undefined): string | undefined {
  return color === undefined ? undefined : edges[color]
}
