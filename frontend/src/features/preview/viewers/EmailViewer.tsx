import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, Download, MailWarning, Paperclip, ShieldOff } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { PanelState } from '@/components/ui/panel-state'
import { formatInstant, resolveTimeZone } from '@/lib/datetime'

import { formatSize } from '../../explorer/entries'
import { PreviewBody } from '../PreviewBody'
import { SandboxFrame } from '../sandbox/SandboxFrame'
import { memorySource } from '../source'
import { readMail, type Mail, type MailAttachment } from './mail'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** The largest message the viewer reads. */
const mailLimit = 25 * 1024 * 1024

/** The views of a message. */
type View = 'message' | 'text' | 'headers'

/** Saves an attachment on the computer, from a blob address let go once the download started. */
function save(attachment: MailAttachment): void {
  const url = URL.createObjectURL(new Blob([attachment.data as Uint8Array<ArrayBuffer>], { type: attachment.type }))
  const link = document.createElement('a')
  link.href = url
  link.download = attachment.name
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}

/** The body of a message: its HTML in the isolated page, or its text. */
function Body({ mail, name }: { mail: Mail; name: string }) {
  const { t } = useTranslation('preview')
  const [failed, setFailed] = useState(false)
  if (mail.html === undefined || failed) {
    return <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words p-4 font-sans text-sm">{mail.text ?? ''}</pre>
  }
  return (
    <>
      {mail.remote && (
        <p className="flex items-center gap-2 border-b bg-muted/40 px-4 py-1.5 text-xs text-muted-foreground">
          <ShieldOff aria-hidden="true" className="size-3.5" />
          {t(($) => $.mail.remote)}
        </p>
      )}
      <SandboxFrame kind="email" name={name} bytes={new TextEncoder().encode(mail.html)} title={t(($) => $.mail.label, { name })} onFailed={() => setFailed(true)} />
    </>
  )
}

/** A message already read: its headers, its attachments, and its body, text or headers by view. */
function Message({ mail, name, sourceKey }: { mail: Mail; name: string; sourceKey: string }) {
  const { t, i18n } = useTranslation('preview')
  const [view, setView] = useState<View>('message')
  const [opened, setOpened] = useState<MailAttachment | null>(null)
  const when = mail.date === undefined || Number.isNaN(Date.parse(mail.date)) ? mail.date : formatInstant(new Date(mail.date), { locale: i18n.language, timeZone: resolveTimeZone(), dateStyle: 'medium', timeStyle: 'short' })
  if (opened !== null) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="flex items-center gap-2 border-b bg-muted/40 px-2 py-1 text-xs">
          <Button variant="ghost" size="xs" onClick={() => setOpened(null)}>
            <ArrowLeft />
            {name}
          </Button>
          <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground">{opened.name}</span>
        </div>
        <PreviewBody source={memorySource(`${sourceKey}!${opened.name}`, opened.name, '', opened.data)} />
      </div>
    )
  }
  const views: View[] = ['message', ...(mail.html !== undefined && mail.text !== undefined ? (['text'] as const) : []), 'headers']
  const rows: Array<[string, string | undefined]> = [
    [t(($) => $.mail.from), mail.from],
    [t(($) => $.mail.to), mail.to],
    [t(($) => $.mail.cc), mail.cc],
    [t(($) => $.mail.date), when],
  ]
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="border-b px-4 py-3">
        <h2 className="mb-2 text-base font-semibold break-words">{mail.subject === '' ? t(($) => $.mail.noSubject) : mail.subject}</h2>
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
          {rows
            .filter(([, value]) => value !== undefined && value !== '')
            .map(([label, value]) => (
              <div key={label} className="contents">
                <dt className="text-muted-foreground">{label}</dt>
                <dd className="min-w-0 break-words">{value}</dd>
              </div>
            ))}
        </dl>
        {mail.attachments.length > 0 && (
          <ul aria-label={t(($) => $.mail.attachments)} className="mt-3 flex flex-wrap gap-2">
            {mail.attachments.map((attachment, index) => (
              <li key={index} className="flex items-center rounded-lg border text-xs">
                <button type="button" className="flex items-center gap-1.5 px-2 py-1 hover:bg-muted" onClick={() => setOpened(attachment)}>
                  <Paperclip aria-hidden="true" className="size-3.5" />
                  <span className="max-w-60 truncate">{attachment.name}</span>
                  <span className="text-muted-foreground">{formatSize(attachment.data.length)}</span>
                </button>
                <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.mail.save, { name: attachment.name })} onClick={() => save(attachment)}>
                  <Download />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </header>
      <div role="group" aria-label={t(($) => $.views.label)} className="flex gap-1 border-b px-2 py-1">
        {views.map((option) => (
          <Button key={option} variant={view === option ? 'secondary' : 'ghost'} size="xs" aria-pressed={view === option} onClick={() => setView(option)}>
            {t(($) => $.mail.views[option])}
          </Button>
        ))}
      </div>
      {view === 'message' && <Body mail={mail} name={name} />}
      {view === 'text' && <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words p-4 font-sans text-sm">{mail.text}</pre>}
      {view === 'headers' && <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all p-4 font-mono text-xs">{mail.headers}</pre>}
    </div>
  )
}

/** Reads a message already fetched and shows it, or says it cannot be read. */
function ReadMessage({ name, bytes, sourceKey }: { name: string; bytes: Uint8Array; sourceKey: string }) {
  const { t } = useTranslation('preview')
  const mail = useQuery({ queryKey: ['preview', sourceKey, 'mail'], queryFn: () => readMail(bytes), staleTime: Infinity, retry: false })
  if (mail.isPending) {
    return <Loading label={t(($) => $.loading)} className="m-auto" />
  }
  if (mail.isError) {
    return <PanelState icon={MailWarning} tone="warn" title={t(($) => $.mail.broken)} description={t(($) => $.mail.brokenBody)} />
  }
  return <Message mail={mail.data} name={name} sourceKey={sourceKey} />
}

/**
 * Shows a message (D2.7): its subject, addresses and date as text, its HTML body cleaned and drawn in the isolated page with nothing
 * loaded from the network, its plain text and header lines, and its attachments, which open with the viewer of their kind or save on the
 * computer. Everything is read here, from the file; nothing is sent anywhere.
 */
export function EmailViewer({ source }: ViewerProps) {
  return <WholeFile source={source} limit={mailLimit}>{(bytes) => <ReadMessage name={source.name} bytes={bytes} sourceKey={source.key} />}</WholeFile>
}
