import { Dialog as DialogPrimitive } from '@base-ui/react/dialog'
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

/** Props of {@link Sheet}. */
export interface SheetProps {
  /** Whether the sheet is open. */
  open: boolean
  /** Called when the sheet asks to open or close: Escape, a click outside, or the close button of the content. */
  onOpenChange: (open: boolean) => void
  /** Accessible name of the sheet. */
  title: string
  /** Content of the sheet. */
  children: ReactNode
  /** Extra classes of the panel. */
  className?: string
}

/**
 * A panel that slides in from the start edge over the page, as a modal dialog: focus stays inside, Escape and a click outside close it. Used
 * to reach a panel the user hid, such as the sites, without giving it room in the layout.
 */
export function Sheet({ open, onOpenChange, title, children, className }: SheetProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-black/30 duration-150 supports-backdrop-filter:backdrop-blur-xs data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0" />
        <DialogPrimitive.Popup
          className={cn(
            'fixed inset-y-0 start-0 z-50 flex w-80 max-w-[85vw] flex-col border-e bg-card shadow-2xl outline-none duration-200 data-open:animate-in data-open:slide-in-from-left data-closed:animate-out data-closed:slide-out-to-left rtl:data-open:slide-in-from-right rtl:data-closed:slide-out-to-right',
            className,
          )}
        >
          <DialogPrimitive.Title className="sr-only">{title}</DialogPrimitive.Title>
          {children}
        </DialogPrimitive.Popup>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
