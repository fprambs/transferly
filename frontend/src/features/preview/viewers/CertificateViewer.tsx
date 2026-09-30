import { FileKey, KeyRound, ShieldAlert, ShieldCheck, TriangleAlert } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { formatInstant, resolveTimeZone } from '@/lib/datetime'
import { useNow } from '@/lib/use-now'
import { cn } from '@/lib/utils'

import { CodeView } from './CodeView'
import { decodeText } from './text'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'
import { readKeyFile, type CertificateInfo, type KeyFileItem, type KeyInfo, type NameAttribute } from './x509'

/** The largest certificate or key file the viewer reads. */
const certificateLimit = 1024 * 1024

/** Writes a distinguished name as its attributes, most specific last, as OpenSSL does. */
function nameText(name: NameAttribute[]): string {
  return name.map((attribute) => `${attribute.name}=${attribute.value}`).join(', ')
}

/** Writes a key as its algorithm with its size or curve. */
function keyText(key: KeyInfo, bits: (count: number) => string): string {
  return [key.algorithm, key.curve, key.bits === undefined ? undefined : bits(key.bits)].filter(Boolean).join(' · ')
}

/** A card of the viewer: an icon, a title, badges and rows of facts. */
function Card({ icon: Icon, title, badges, tone = 'default', children }: { icon: typeof FileKey; title: string; badges?: ReactNode; tone?: 'default' | 'warn'; children?: ReactNode }) {
  return (
    <section className={cn('rounded-xl border bg-card p-4', tone === 'warn' && 'border-warn/40 bg-warn/5')}>
      <header className="mb-3 flex flex-wrap items-center gap-2">
        <Icon aria-hidden="true" className={cn('size-4', tone === 'warn' ? 'text-warn' : 'text-accent-text')} />
        <h3 className="text-sm font-semibold">{title}</h3>
        {badges}
      </header>
      {children}
    </section>
  )
}

/** A badge of a card. */
function Badge({ tone, children }: { tone: 'ok' | 'warn' | 'danger' | 'muted'; children: ReactNode }) {
  const tones = { ok: 'bg-ok/15 text-ok', warn: 'bg-warn/15 text-warn', danger: 'bg-destructive/15 text-destructive', muted: 'bg-muted text-muted-foreground' }
  return <span className={cn('rounded px-1.5 py-0.5 text-[11px] font-medium', tones[tone])}>{children}</span>
}

/** Rows of facts, each left out when it has no value. */
function Facts({ rows }: { rows: Array<[string, ReactNode]> }) {
  return (
    <dl className="grid grid-cols-[minmax(8rem,auto)_1fr] gap-x-4 gap-y-1.5 text-xs">
      {rows
        .filter(([, value]) => value !== '')
        .map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words">{value}</dd>
          </div>
        ))}
    </dl>
  )
}

/** A certificate: whom it names, whether it is valid now, and its details and fingerprints. */
function Certificate({ info }: { info: CertificateInfo }) {
  const { t, i18n } = useTranslation('preview')
  const timeZone = resolveTimeZone()
  const date = (value: Date): string => formatInstant(value, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })
  const now = useNow(60_000).getTime()
  const status =
    now > info.notAfter.getTime() ? (
      <Badge tone="danger">{t(($) => $.certificate.expired, { date: date(info.notAfter) })}</Badge>
    ) : now < info.notBefore.getTime() ? (
      <Badge tone="warn">{t(($) => $.certificate.notYet, { date: date(info.notBefore) })}</Badge>
    ) : (
      <Badge tone="ok">{t(($) => $.certificate.valid, { date: date(info.notAfter) })}</Badge>
    )
  const title = info.subject.find((attribute) => attribute.name === 'CN')?.value ?? t(($) => $.certificate.certificate)
  const mono = (text: string) => <span className="font-mono break-all">{text}</span>
  return (
    <Card
      icon={now > info.notAfter.getTime() ? ShieldAlert : ShieldCheck}
      title={title}
      badges={
        <>
          {status}
          {info.authority === true && <Badge tone="muted">{t(($) => $.certificate.authority)}</Badge>}
          {info.selfSigned && <Badge tone="muted">{t(($) => $.certificate.selfSigned)}</Badge>}
        </>
      }
    >
      <Facts
        rows={[
          [t(($) => $.certificate.subject), nameText(info.subject)],
          [t(($) => $.certificate.issuer), nameText(info.issuer)],
          [t(($) => $.certificate.notBefore), date(info.notBefore)],
          [t(($) => $.certificate.notAfter), date(info.notAfter)],
          [t(($) => $.certificate.altNames), info.altNames.join(', ')],
          [t(($) => $.certificate.key), keyText(info.key, (count) => t(($) => $.certificate.bits, { count }))],
          [t(($) => $.certificate.signature), info.signature],
          [t(($) => $.certificate.keyUsage), info.keyUsage.join(', ')],
          [t(($) => $.certificate.extendedKeyUsage), info.extendedKeyUsage.join(', ')],
          [t(($) => $.certificate.serial), mono(info.serial)],
          ['SHA-256', mono(info.sha256)],
          ['SHA-1', mono(info.sha1)],
        ]}
      />
    </Card>
  )
}

/** One item of the file, as its card. */
function Item({ item }: { item: KeyFileItem }) {
  const { t } = useTranslation('preview')
  const bits = (count: number) => t(($) => $.certificate.bits, { count })
  switch (item.kind) {
    case 'certificate':
      return <Certificate info={item} />
    case 'publicKey':
      return (
        <Card icon={KeyRound} title={t(($) => $.certificate.publicKey)}>
          <Facts rows={[[t(($) => $.certificate.key), keyText(item.key, bits)], ['SHA-256', <span key="sha256" className="font-mono break-all">{item.sha256}</span>]]} />
        </Card>
      )
    case 'request':
      return (
        <Card icon={FileKey} title={t(($) => $.certificate.request)}>
          <Facts rows={[[t(($) => $.certificate.subject), nameText(item.subject)], [t(($) => $.certificate.key), keyText(item.key, bits)]]} />
        </Card>
      )
    case 'sshKey':
      return (
        <Card icon={KeyRound} title={item.comment === '' ? t(($) => $.certificate.sshKey) : item.comment}>
          <Facts rows={[[t(($) => $.certificate.type), item.type], [t(($) => $.certificate.key), keyText(item.key, bits)], [t(($) => $.certificate.fingerprint), <span key="fingerprint" className="font-mono break-all">{item.fingerprint}</span>]]} />
        </Card>
      )
    case 'privateKey':
      return (
        <Card icon={TriangleAlert} tone="warn" title={t(($) => $.certificate.privateKey)} badges={<Badge tone="warn">{item.label}</Badge>}>
          <p className="text-xs text-muted-foreground">{t(($) => $.certificate.privateKeyBody)}</p>
        </Card>
      )
    default:
      return (
        <Card icon={ShieldAlert} title={t(($) => $.certificate.unreadable)} badges={<Badge tone="muted">{item.label}</Badge>}>
          <p className="text-xs text-muted-foreground">{t(($) => $.certificate.unreadableBody)}</p>
        </Card>
      )
  }
}

/** The items of a file already read, and its source a click away when it is text without private keys. */
function KeyFile({ name, bytes }: { name: string; bytes: Uint8Array }) {
  const { t } = useTranslation('preview')
  const items = useMemo(() => readKeyFile(bytes), [bytes])
  const [source, setSource] = useState(false)
  const text = decodeText(bytes)
  const readable = text.includes('-----BEGIN') || items.some((item) => item.kind === 'sshKey')
  const showable = readable && !items.some((item) => item.kind === 'privateKey')
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b px-3 py-1 text-xs text-muted-foreground">
        <span>{t(($) => $.certificate.items, { count: items.length })}</span>
        {showable && (
          <Button variant={source ? 'secondary' : 'ghost'} size="xs" className="ms-auto" aria-pressed={source} onClick={() => setSource(!source)}>
            {t(($) => $.views.source)}
          </Button>
        )}
      </div>
      {source ? (
        <CodeView name={name} text={text} />
      ) : (
        <div className="min-h-0 flex-1 overflow-auto">
          <div className="mx-auto flex max-w-3xl flex-col gap-3 p-4">
            {items.map((item, index) => (
              <Item key={index} item={item} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * Shows certificates and keys (D2.7): each certificate of a PEM or DER file with whom it names, whether it is valid now, its key, other
 * names, usages and fingerprints; public keys, certificate requests and OpenSSH keys with their size and fingerprint. Private keys are
 * never shown, only named with a warning, and their files do not offer their source. Everything is read here, as data: nothing of the
 * file is rendered as markup.
 */
export function CertificateViewer({ source }: ViewerProps) {
  return <WholeFile source={source} limit={certificateLimit}>{(bytes) => <KeyFile name={source.name} bytes={bytes} />}</WholeFile>
}
