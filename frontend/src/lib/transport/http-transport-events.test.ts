import { afterEach, describe, expect, it, vi } from 'vitest'

import { FakeSocket } from '@/test/fake-socket'

import { HttpTransport } from './http-transport'
import type { EventHandlers } from './types'

/** Builds handlers that record what they receive. */
function recorder() {
  const log: string[] = []
  const handlers: EventHandlers = {
    onEvent: (event) => log.push(event.type),
    onOpen: () => log.push('open'),
    onClose: () => log.push('close'),
  }
  return { log, handlers }
}

/**
 * Covers {@link HttpTransport.events}.
 *
 * Scenarios:
 * - the stream opens on the page's origin with `ws`, or `wss` for https pages, and on the base URL when given
 * - open, close and known events reach the handlers; messages that are not JSON or not known events are ignored
 * - the returned function closes the socket and silences `onClose`
 * - without an injected constructor the global WebSocket is used
 */
describe('HttpTransport.events', () => {
  afterEach(() => {
    FakeSocket.reset()
  })

  it('opens the stream on the page origin', () => {
    const transport = new HttpTransport({ webSocket: FakeSocket as unknown as typeof WebSocket })

    transport.events(recorder().handlers)

    expect(FakeSocket.opened[0].url).toBe(`ws://${window.location.host}/api/v1/ws`)
  })

  it('uses wss for a secure base URL and ws for a plain one', () => {
    const secure = new HttpTransport({ baseUrl: 'https://transferly.example.com', webSocket: FakeSocket as unknown as typeof WebSocket })
    const plain = new HttpTransport({ baseUrl: 'http://localhost:8080', webSocket: FakeSocket as unknown as typeof WebSocket })

    secure.events(recorder().handlers)
    plain.events(recorder().handlers)

    expect(FakeSocket.opened.map((socket) => socket.url)).toEqual(['wss://transferly.example.com/api/v1/ws', 'ws://localhost:8080/api/v1/ws'])
  })

  it('delivers open, close and known events and ignores the rest', () => {
    const { log, handlers } = recorder()
    new HttpTransport({ webSocket: FakeSocket as unknown as typeof WebSocket }).events(handlers)
    const socket = FakeSocket.opened[0]

    socket.onopen?.()
    socket.onmessage?.({ data: JSON.stringify({ type: 'snapshot', data: { transfers: [] } }) })
    socket.onmessage?.({ data: JSON.stringify({ type: 'vault.locked', data: null }) })
    socket.onmessage?.({ data: JSON.stringify({ type: 'connection.log', data: { step: 'connected' } }) })
    socket.onmessage?.({ data: JSON.stringify({ type: 'something.else', data: null }) })
    socket.onmessage?.({ data: JSON.stringify({ data: null }) })
    socket.onmessage?.({ data: 'not json' })
    socket.onclose?.()

    expect(log).toEqual(['open', 'snapshot', 'vault.locked', 'connection.log', 'close'])
  })

  it('closes the socket without reporting it when asked to stop', () => {
    const { log, handlers } = recorder()
    const stop = new HttpTransport({ webSocket: FakeSocket as unknown as typeof WebSocket }).events(handlers)
    const socket = FakeSocket.opened[0]

    stop()

    expect(socket.closed).toBe(true)
    expect(socket.onclose).toBeNull()
    expect(log).toEqual([])
  })

  it('uses the global WebSocket by default', () => {
    vi.stubGlobal('WebSocket', FakeSocket)

    new HttpTransport().events(recorder().handlers)

    expect(FakeSocket.opened).toHaveLength(1)
  })
})
