import { describe, expect, it, vi } from 'vitest'

import { FakeSocket } from '@/test/fake-socket'

import { HttpTransport } from './http-transport'

/** Creates a response promise whose delivery can be delayed across an identity transition. */
function delayedTransport() {
  let deliver: (response: Response) => void = () => {}
  let signal: AbortSignal | undefined
  const fetch = vi.fn((_input: unknown, options?: RequestInit) => {
    signal = options?.signal as AbortSignal
    return new Promise<Response>((resolve) => { deliver = resolve })
  }) as unknown as typeof globalThis.fetch
  const transport = new HttpTransport({ fetch, webSocket: FakeSocket as unknown as typeof WebSocket })
  return { transport, deliver: (response: Response) => deliver(response), aborted: () => signal?.aborted }
}

/** Checks initial discovery, late responses and socket revocation without depending on fetch honoring cancellation. */
describe('transport identity lifetime', () => {
  it('discards an expiry callback already queued when the identity changes', async () => {
    const pending: Array<() => void> = []
    const queued = vi.spyOn(globalThis, 'queueMicrotask').mockImplementation((callback) => pending.push(callback))
    const d = delayedTransport()
    const expired = vi.fn()
    d.transport.setOnUnauthenticated(expired)
    const response = d.transport.request({ method: 'GET', path: '/private' })
    const rejected = expect(response).rejects.toMatchObject({ code: 'unauthenticated' })
    const problem = { status: 401, code: 'unauthenticated' }
    const answer = Response.json(problem, { status: 401, headers: { 'Content-Type': 'application/problem+json' } })
    vi.spyOn(answer, 'json').mockResolvedValue(problem)
    d.deliver(answer)
    await rejected
    d.transport.resetSession()
    pending.forEach((callback) => callback())
    expect(expired).not.toHaveBeenCalled()
    queued.mockRestore()
  })
  it('keeps initial metadata requests alive when the first CSRF token is discovered', async () => {
    const d = delayedTransport()
    const response = d.transport.request<{ owner: string }>({ method: 'GET', path: '/metadata' })
    d.transport.setCsrfToken('first')
    expect(d.aborted()).toBe(false)
    d.deliver(Response.json({ owner: 'A' }))
    await expect(response).resolves.toEqual({ owner: 'A' })
  })

  it('rejects old successful responses and closes session sockets on an explicit identity change', async () => {
    const d = delayedTransport()
    d.transport.setCsrfToken('A')
    const response = d.transport.request({ method: 'GET', path: '/private' })
    const rejected = expect(response).rejects.toMatchObject({ name: 'AbortError' })
    const event = vi.fn()
    d.transport.events({ onOpen: vi.fn(), onClose: vi.fn(), onEvent: event })
    const socket = FakeSocket.opened.at(-1) as FakeSocket
    d.transport.resetSession()
    d.transport.setCsrfToken('B')
    expect(d.aborted()).toBe(true)
    expect(socket.closed).toBe(true)
    socket.emit('snapshot', { transfers: [] })
    expect(event).not.toHaveBeenCalled()
    d.deliver(Response.json({ owner: 'A' }))
    await rejected
  })

  it('does not let an old unauthenticated response revoke the new identity', async () => {
    const d = delayedTransport()
    const expired = vi.fn()
    d.transport.setOnUnauthenticated(expired)
    const response = d.transport.request({ method: 'GET', path: '/private' })
    const rejected = expect(response).rejects.toMatchObject({ name: 'AbortError' })
    d.transport.resetSession()
    d.transport.setCsrfToken('B')
    d.deliver(Response.json({ status: 401, code: 'unauthenticated' }, { status: 401, headers: { 'Content-Type': 'application/problem+json' } }))
    await rejected
    expect(expired).not.toHaveBeenCalled()
  })
})
