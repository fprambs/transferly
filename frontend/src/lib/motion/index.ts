/** How long the panels of the layout glide when a button opens or closes them, in milliseconds, with a margin over the transition. */
export const panelGlideMs = 260

/** Timer of the glide in course, so a second toggle extends it instead of cutting it short. */
let glideTimer: ReturnType<typeof setTimeout> | undefined

/**
 * Lets the collapsible panels of the layout animate their size for a moment, while a button opens or closes one; outside that moment
 * dragging a separator and resizing the window stay immediate (`html[data-panels-moving]` in `index.css`).
 */
export function glidePanels(root: HTMLElement = document.documentElement): void {
  root.dataset.panelsMoving = ''
  clearTimeout(glideTimer)
  glideTimer = setTimeout(() => {
    delete root.dataset.panelsMoving
  }, panelGlideMs)
}

/** Whether the person asked the system for reduced motion. */
export function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * Fades an element in again after its content changed in place, such as a folder or a tab replacing another, without remounting it,
 * so focus and scroll stay where they are. Does nothing when the browser cannot animate or the person prefers reduced motion.
 */
export function refresh(element: HTMLElement | null): void {
  if (element === null || typeof element.animate !== 'function' || prefersReducedMotion()) {
    return
  }
  element.animate([{ opacity: 0.35, transform: 'translateY(2px)' }, { opacity: 1, transform: 'none' }], { duration: 180, easing: 'cubic-bezier(0.2, 0, 0, 1)' })
}

/** The part of the View Transitions API that {@link crossfade} uses, which not every browser has yet. */
interface ViewTransitionDocument {
  /** Starts a view transition that runs the update between the snapshots of the old and the new page. */
  startViewTransition?: (update: () => void) => unknown
}

/**
 * Runs a change of the whole page, such as a new theme, as a short crossfade when the browser supports view transitions and the person
 * does not prefer reduced motion; otherwise the change happens at once.
 */
export function crossfade(update: () => void, target: ViewTransitionDocument = document as ViewTransitionDocument): void {
  if (typeof target.startViewTransition !== 'function' || prefersReducedMotion()) {
    update()
    return
  }
  target.startViewTransition(update)
}
