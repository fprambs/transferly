import { useCallback, useEffect } from 'react'
import { Separator, usePanelCallbackRef, type LayoutStorage, type PanelImperativeHandle, type PanelSize, type SeparatorProps } from 'react-resizable-panels'

import { cn } from '@/lib/utils'
import { glidePanels } from '@/lib/motion'

export { Group, Panel, useDefaultLayout } from 'react-resizable-panels'

/**
 * Where panel layouts are kept: the browser's local storage, as a per-browser convenience. Reading and writing never throw, since storage is
 * refused in private windows and when site data is blocked; the default layout applies then.
 */
export const layoutStorage: LayoutStorage = {
  getItem: (key) => {
    try {
      return localStorage.getItem(key)
    } catch {
      return null
    }
  },
  setItem: (key, value) => {
    try {
      localStorage.setItem(key, value)
    } catch {
      return
    }
  },
}

/** Props of {@link ResizeHandle}. */
export interface ResizeHandleProps extends Omit<SeparatorProps, 'aria-label'> {
  /** What the handle resizes, as its accessible name. */
  'aria-label': string
}

/**
 * The separator between two resizable panels: a 1 px line with a wider hit area, highlighted on hover and while dragged. It is focusable and
 * resizes with the arrow keys, collapses or expands with Enter, moves to the next separator with F6, and restores the default size on double
 * click.
 */
export function ResizeHandle({ className, ...props }: ResizeHandleProps) {
  return (
    <Separator
      className={cn(
        'relative z-10 bg-border outline-none transition-colors',
        'aria-[orientation=vertical]:w-px aria-[orientation=vertical]:after:absolute aria-[orientation=vertical]:after:inset-y-0 aria-[orientation=vertical]:after:-start-1 aria-[orientation=vertical]:after:w-2',
        'aria-[orientation=horizontal]:h-px aria-[orientation=horizontal]:after:absolute aria-[orientation=horizontal]:after:inset-x-0 aria-[orientation=horizontal]:after:-top-1 aria-[orientation=horizontal]:after:h-2',
        'data-[separator=hover]:bg-accent-text/60 data-[separator=active]:bg-accent-text focus-visible:bg-accent-text',
        className,
      )}
      {...props}
    />
  )
}

/** What {@link useCollapsible} gives a collapsible panel. */
export interface Collapsible {
  /** Attach to the panel as `panelRef`. */
  panelRef: (handle: PanelImperativeHandle | null) => void
  /** Attach to the panel as `onResize`. */
  onResize: (size: PanelSize) => void
}

/**
 * Keeps a collapsible panel and a preference in step: the preference opening or closing collapses or expands the panel, and the user
 * dragging the panel closed, or open again, updates the preference.
 */
export function useCollapsible(open: boolean, onOpenChange: (open: boolean) => void): Collapsible {
  const [panel, setPanel] = usePanelCallbackRef()

  useEffect(() => {
    if (panel === null) {
      return
    }
    if (open && panel.isCollapsed()) {
      glidePanels()
      panel.expand()
    } else if (!open && !panel.isCollapsed()) {
      glidePanels()
      panel.collapse()
    }
  }, [panel, open])

  const onResize = useCallback(() => {
    const collapsed = panel?.isCollapsed() ?? !open
    if (collapsed === open) {
      onOpenChange(!collapsed)
    }
  }, [panel, open, onOpenChange])

  return { panelRef: setPanel, onResize }
}
