import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'
import type { ConnectionPrompt } from '@/lib/transport'
import { mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { PromptDialog } from './PromptDialog'
import { usePrompts } from './prompts-store'

/** An interactive site that asks for a password and a code. */
const otp: Site = {
  id: 's1',
  name: 'Bastion',
  protocol: 'sftp',
  host: 'bastion.example.com',
  port: 22,
  username: 'deploy',
  authMethod: 'interactive',
  listTimezone: 'UTC',
  charset: 'utf-8',
  speedLimitBps: 0,
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** The challenge the site sends: a hidden password and a code that may be shown. */
const challenge: ConnectionPrompt = {
  id: 'p1',
  workspaceId: 'w1',
  siteId: 's1',
  name: 'Two steps',
  instruction: 'Enter your password and the code of your app',
  questions: [
    { text: 'Password: ', echo: false },
    { text: 'Code: ', echo: true },
  ],
}

/** Records the answers and declines the dialog sends, answering each with `status`, or a problem with `code`. */
function mockPrompts(code?: string) {
  const answered: { id: string; answers: string[] }[] = []
  const declined: string[] = []
  server.use(
    http.post('/api/v1/prompts/:id', async ({ params, request }) => {
      if (code !== undefined) {
        return problem(code === 'prompt_not_found' ? 404 : 500, code)
      }
      const body = (await request.json()) as { answers: string[] }
      answered.push({ id: String(params.id), answers: body.answers })
      return new HttpResponse(null, { status: 204 })
    }),
    http.delete('/api/v1/prompts/:id', ({ params }) => {
      declined.push(String(params.id))
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return { answered, declined }
}

/**
 * Covers answering what a server asks while an interactive site connects (ADR-0016).
 *
 * Scenarios:
 * - nothing shows while no prompt waits; a prompt opens the dialog naming the site, its host and account, the challenge and its questions
 * - an answer the server says must not be shown is hidden, and can be shown on demand; one that may be shown is plain text
 * - every question needs an answer; the answers go to the prompt in order and the dialog closes
 * - prompts queue: the dialog says how many more wait, and shows the next once one is done
 * - cancel, and dismissing the dialog, decline the prompt; a decline the server does not take still closes the dialog
 * - a prompt the connection no longer waits for says so; another failure asks to try again
 * - a prompt closed elsewhere, answered in another tab or given up, closes the dialog; the same prompt is never queued twice
 * - a prompt from a site that is not loaded, with no title, instruction or wording, still asks
 */
describe('PromptDialog', () => {
  beforeEach(() => {
    mockWorkspaceData({ sites: [otp] })
  })

  it('answers every question in order', async () => {
    const { answered } = mockPrompts()
    await renderWithProviders(<PromptDialog />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    act(() => usePrompts.getState().add(challenge))
    expect(await screen.findByRole('heading', { name: 'Bastion asks' })).toBeInTheDocument()
    expect(await screen.findByText('bastion.example.com')).toBeInTheDocument()
    expect(screen.getByText('Two steps')).toBeInTheDocument()
    expect(screen.getByText('Enter your password and the code of your app')).toBeInTheDocument()
    const password = screen.getByLabelText('Password:')
    const code = screen.getByLabelText('Code:')
    expect(password).toHaveAttribute('type', 'password')
    expect(code).toHaveAttribute('type', 'text')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Show' }))
    expect(password).toHaveAttribute('type', 'text')
    await user.click(screen.getByRole('button', { name: 'Hide' }))
    expect(password).toHaveAttribute('type', 'password')

    await user.type(password, 'secret')
    await user.click(screen.getByRole('button', { name: 'Answer' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Answer every question.')
    await user.type(code, '123456')
    await user.click(screen.getByRole('button', { name: 'Answer' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(answered).toEqual([{ id: 'p1', answers: ['secret', '123456'] }])
  })

  it('queues prompts and declines them', async () => {
    const { declined } = mockPrompts()
    await renderWithProviders(<PromptDialog />)
    act(() => {
      usePrompts.getState().add(challenge)
      usePrompts.getState().add(challenge)
      usePrompts.getState().add({ ...challenge, id: 'p2', siteId: 'gone', name: '', instruction: '', questions: [{ text: '', echo: false }] })
    })
    expect(await screen.findByText('1 more waiting')).toBeInTheDocument()

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByRole('heading', { name: 'The server asks' })).toBeInTheDocument()
    expect(screen.queryByText('1 more waiting')).not.toBeInTheDocument()
    expect(screen.getByLabelText('The server asks', { selector: 'input' })).toHaveAttribute('type', 'password')

    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(declined).toEqual(['p1', 'p2'])
  })

  it('says when the answer cannot reach the connection', async () => {
    mockPrompts('prompt_not_found')
    await renderWithProviders(<PromptDialog />)
    act(() => usePrompts.getState().add({ ...challenge, questions: [{ text: 'Code:', echo: true }] }))
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Code:'), '123456')
    await user.click(screen.getByRole('button', { name: 'Answer' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The connection stopped waiting for this answer.')

    mockPrompts('internal')
    await user.click(screen.getByRole('button', { name: 'Answer' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('The answer did not reach the connection.'))

    act(() => usePrompts.getState().close('p1'))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('closes even when the decline does not reach the server', async () => {
    server.use(http.delete('/api/v1/prompts/:id', () => problem(500, 'internal')))
    await renderWithProviders(<PromptDialog />)
    act(() => usePrompts.getState().add(challenge))
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
