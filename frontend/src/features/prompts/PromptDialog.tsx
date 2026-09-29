import { Eye, EyeOff, MessageSquareLock } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, type ConnectionPrompt } from '@/lib/transport'
import { useApi } from '@/lib/api/use-api'

import { useSitesData } from '../sites/use-sites-data'

import { usePrompts } from './prompts-store'

/** What the dialog shows of the site that asks: its name, host and account. */
interface Asker {
  /** Name of the site. */
  name: string
  /** Host of the server. */
  host: string
  /** Account that signs in. */
  username: string
}

/** Finds the saved site that asks, the only kind with the interactive logon type; `undefined` until the sites of its workspace load, or
 * when it is not among them. */
function useAsker(prompt: ConnectionPrompt): Asker | undefined {
  return useSitesData(prompt.workspaceId).sites.find((candidate) => candidate.id === prompt.siteId)
}

/** Why an answer could not be sent. */
type Failure = 'required' | 'failed' | 'expired'

/** One question: a text field, hidden unless the server says the answer may be shown, with a switch to show it. */
function QuestionField({ id, text, echo, value, autoFocus, onChange }: { id: string; text: string; echo: boolean; value: string; autoFocus: boolean; onChange: (value: string) => void }) {
  const { t } = useTranslation('sites')
  const [shown, setShown] = useState(false)
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="font-mono">
        {text.trim() === '' ? t(($) => $.prompt.titleUnknown) : text.trim()}
      </Label>
      <div className="flex gap-2">
        <Input id={id} type={echo || shown ? 'text' : 'password'} autoFocus={autoFocus} autoComplete="off" spellCheck={false} value={value} onChange={(event) => onChange(event.target.value)} />
        {!echo && (
          <Button type="button" variant="outline" size="icon" aria-label={shown ? t(($) => $.prompt.hide) : t(($) => $.prompt.show)} aria-pressed={shown} onClick={() => setShown(!shown)}>
            {shown ? <EyeOff /> : <Eye />}
          </Button>
        )}
      </div>
    </div>
  )
}

/** The answer form of one prompt. Its submit event stops here, so it never submits a form it was opened from. */
function PromptForm({ prompt, more, onDecline }: { prompt: ConnectionPrompt; more: number; onDecline: () => void }) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const asker = useAsker(prompt)
  const close = usePrompts((state) => state.close)
  const [answers, setAnswers] = useState(() => prompt.questions.map(() => ''))
  const [failure, setFailure] = useState<Failure | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    event.stopPropagation()
    if (answers.some((answer) => answer === '')) {
      setFailure('required')
      return
    }
    setBusy(true)
    setFailure(null)
    try {
      await api.prompts.answer(prompt.id, answers)
      close(prompt.id)
    } catch (error) {
      setFailure(error instanceof ApiError && error.code === 'prompt_not_found' ? 'expired' : 'failed')
      setBusy(false)
    }
  }

  return (
    <form onSubmit={(event) => void submit(event)} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{asker === undefined ? t(($) => $.prompt.titleUnknown) : t(($) => $.prompt.title, { site: asker.name })}</DialogTitle>
        <DialogDescription>{t(($) => $.prompt.description)}</DialogDescription>
      </DialogHeader>
      {asker !== undefined && (
        <dl aria-label={asker.name} className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-lg border bg-muted/40 px-3 py-2.5 text-sm">
          <dt className="text-muted-foreground">{t(($) => $.site.host)}</dt>
          <dd className="truncate font-mono">{asker.host}</dd>
          <dt className="text-muted-foreground">{t(($) => $.site.username)}</dt>
          <dd className="truncate font-mono">{asker.username}</dd>
        </dl>
      )}
      {(prompt.name !== '' || prompt.instruction !== '') && (
        <div className="flex flex-col gap-1 text-sm">
          {prompt.name !== '' && <p className="font-medium">{prompt.name}</p>}
          {prompt.instruction !== '' && <p className="whitespace-pre-wrap break-words text-muted-foreground">{prompt.instruction}</p>}
        </div>
      )}
      {prompt.questions.map((question, index) => (
        <QuestionField
          key={index}
          id={`prompt-answer-${index}`}
          text={question.text}
          echo={question.echo}
          value={answers[index]}
          autoFocus={index === 0}
          onChange={(value) => setAnswers(answers.map((answer, other) => (other === index ? value : answer)))}
        />
      ))}
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.prompt[failure])}
        </p>
      )}
      <DialogFooter className="items-center">
        {more > 0 && <span className="me-auto text-xs text-muted-foreground">{t(($) => $.prompt.queue, { count: more })}</span>}
        <Button type="button" variant="outline" onClick={onDecline}>
          {t(($) => $.prompt.cancel)}
        </Button>
        <Button type="submit" disabled={busy}>
          <MessageSquareLock />
          {t(($) => $.prompt.submit)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/**
 * Answers what a server asks while an interactive site connects (ADR-0016): a password, a one-time code or any other challenge. It shows the
 * oldest prompt waiting, one at a time, and closes when it is answered, declined, or given up by the server, here or in another tab. Answers
 * go straight to the connection that waits for them; the browser keeps none.
 */
export function PromptDialog() {
  const waiting = usePrompts((state) => state.waiting)
  const close = usePrompts((state) => state.close)
  const api = useApi()
  const prompt = waiting.at(0)
  const decline = (id: string) => {
    close(id)
    void api.prompts.cancel(id).catch(() => undefined)
  }
  return (
    <Dialog open={prompt !== undefined} onOpenChange={() => prompt !== undefined && decline(prompt.id)}>
      <DialogContent className="sm:max-w-[480px]">
        {prompt !== undefined && <PromptForm key={prompt.id} prompt={prompt} more={waiting.length - 1} onDecline={() => decline(prompt.id)} />}
      </DialogContent>
    </Dialog>
  )
}
