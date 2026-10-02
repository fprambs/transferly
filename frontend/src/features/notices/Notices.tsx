import { CircleCheck, X } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useUiStore } from '@/state/ui-store'

import { useNotices, type Notice } from './notices-store'

/** How long a notice stays on show, in milliseconds. */
export const noticeMs = 4000

/** One notice: what was done, a way to the connection log, and a button to take it away; it leaves by itself after a while. */
function NoticeCard({ notice }: { notice: Notice }) {
  const { t } = useTranslation('shell')
  const dismiss = useNotices((state) => state.dismiss)
  const setLogOpen = useUiStore((state) => state.setLogOpen)

  useEffect(() => {
    const timer = setTimeout(() => dismiss(notice.id), noticeMs)
    return () => clearTimeout(timer)
  }, [dismiss, notice.id])

  return (
    <li className="pointer-events-auto flex items-center gap-3 rounded-xl border bg-popover py-2.5 ps-3.5 pe-2 text-sm text-popover-foreground shadow-lg ring-1 ring-foreground/5 duration-200 animate-in slide-in-from-bottom-2">
      <CircleCheck aria-hidden="true" className="size-4 shrink-0 text-ok" />
      <span className="min-w-0 flex-1 break-words">{notice.text}</span>
      <Button
        variant="ghost"
        size="xs"
        onClick={() => {
          setLogOpen(true)
          dismiss(notice.id)
        }}
      >
        {t(($) => $.notices.log)}
      </Button>
      <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.notices.dismiss)} onClick={() => dismiss(notice.id)}>
        <X />
      </Button>
    </li>
  )
}

/**
 * Short notices in the corner of the shell that say an action was done, such as a renamed file, read aloud politely; each offers the
 * connection log, where the details are, and leaves after a few seconds or when dismissed.
 */
export function Notices() {
  const { t } = useTranslation('shell')
  const notices = useNotices((state) => state.notices)

  return (
    <section aria-label={t(($) => $.notices.label)} aria-live="polite" className="pointer-events-none fixed end-4 bottom-16 z-40 w-[min(22rem,calc(100vw-2rem))]">
      <ul className="flex flex-col gap-2">
        {notices.map((notice) => (
          <NoticeCard key={notice.id} notice={notice} />
        ))}
      </ul>
    </section>
  )
}
