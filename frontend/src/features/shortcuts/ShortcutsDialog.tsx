import { Keyboard } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { formatShortcut } from '@/lib/shortcuts'
import { useDialogsStore } from '@/state/dialogs-store'

/** Whether a key press happens while typing, where ? belongs to the field. */
function typing(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
}

/** The shortcuts of each group, as key specs (one or more ways to press it) and the key of what they do. */
const shortcutGroups = [
  {
    key: 'general',
    items: [
      { keys: ['Mod+K'], does: 'palette' },
      { keys: ['Mod+Shift+K'], does: 'quickConnect' },
      { keys: ['Mod+B'], does: 'sites' },
      { keys: ['?'], does: 'help' },
    ],
  },
  {
    key: 'tabs',
    items: [
      { keys: ['Ctrl+Tab', 'Alt+PageDown'], does: 'nextTab' },
      { keys: ['Ctrl+Shift+Tab', 'Alt+PageUp'], does: 'previousTab' },
      { keys: ['Ctrl+W', 'Alt+W'], does: 'closeTab' },
    ],
  },
  {
    key: 'files',
    items: [
      { keys: ['↑', '↓'], does: 'move' },
      { keys: ['Enter'], does: 'open' },
      { keys: ['Space'], does: 'preview' },
      { keys: ['Backspace'], does: 'up' },
      { keys: ['F2'], does: 'rename' },
      { keys: ['Delete'], does: 'delete' },
      { keys: ['/', 'Mod+F'], does: 'filter' },
    ],
  },
] as const

/**
 * The help of the keyboard shortcuts: ? opens it anywhere but in a field, as does its entry in the command palette, and it lists the
 * shortcuts of the shell, the tabs and the files, written for the system of the user.
 */
export function ShortcutsDialog() {
  const { t } = useTranslation('shell')
  const open = useDialogsStore((state) => state.shortcutsOpen)
  const setOpen = useDialogsStore((state) => state.setShortcutsOpen)

  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => {
      if (event.key === '?' && !event.ctrlKey && !event.metaKey && !event.altKey && !typing(event.target)) {
        event.preventDefault()
        setOpen(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [setOpen])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-h-[85svh] gap-5 overflow-y-auto sm:max-w-lg">
        <div className="flex items-start gap-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/12 text-accent-text">
            <Keyboard aria-hidden="true" className="size-[18px]" />
          </span>
          <div className="flex flex-col gap-0.5">
            <DialogTitle>{t(($) => $.shortcuts.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.shortcuts.description)}</DialogDescription>
          </div>
        </div>
        {shortcutGroups.map((group) => (
          <section key={group.key} aria-labelledby={`shortcuts-${group.key}`} className="flex flex-col gap-1.5">
            <h3 id={`shortcuts-${group.key}`} className="text-[11px] font-medium tracking-widest text-muted-foreground uppercase">
              {t(($) => $.shortcuts.groups[group.key])}
            </h3>
            <dl className="overflow-hidden rounded-xl border">
              {group.items.map((item) => (
                <div key={item.does} className="flex items-center justify-between gap-4 border-b px-3.5 py-2 text-sm last:border-b-0">
                  <dt>{t(($) => $.shortcuts.does[item.does])}</dt>
                  <dd className="flex shrink-0 items-center gap-1.5">
                    {item.keys.map((spec, index) => (
                      <span key={spec} className="flex items-center gap-1.5">
                        {index > 0 && <span className="text-xs text-muted-foreground">{t(($) => $.shortcuts.or)}</span>}
                        <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-[11px]">{formatShortcut(spec)}</kbd>
                      </span>
                    ))}
                  </dd>
                </div>
              ))}
            </dl>
          </section>
        ))}
      </DialogContent>
    </Dialog>
  )
}
