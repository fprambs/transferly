import { HttpTransport } from '@/lib/transport'

/** A WebSocket that records how it was used and lets a test play the server's part. */
export class FakeSocket {
  /** Every socket opened since the last {@link FakeSocket.reset}. */
  static opened: FakeSocket[] = []
  /** Address the socket was opened with. */
  readonly url: string
  /** Whether the client closed it. */
  closed = false
  /** Called when the connection opens. */
  onopen: (() => void) | null = null
  /** Called when the connection ends. */
  onclose: (() => void) | null = null
  /** Called for each message. */
  onmessage: ((message: { data: unknown }) => void) | null = null
  /** What the client asked binary messages to arrive as. */
  binaryType = 'blob'
  /** The ready state: 0 while connecting, 1 once open. */
  readyState = 0
  /** Every message the client sent. */
  sent: unknown[] = []
  /** Cleanup listeners independent of the handler installed by a terminal component. */
  private closeListeners: Array<() => void> = []

  /** Records close listeners for transport resource cleanup. */
  addEventListener(_type: string, listener: () => void): void {
    this.closeListeners.push(listener)
  }

  /** Records the socket. */
  constructor(url: string) {
    this.url = url
    FakeSocket.opened.push(this)
  }

  /** Forgets the sockets of earlier tests. */
  static reset(): void {
    FakeSocket.opened = []
  }

  /** Records that the client closed the socket. */
  close(): void {
    this.closed = true
    this.closeListeners.forEach((listener) => listener())
  }

  /** Records a message the client sent. */
  send(data: unknown): void {
    this.sent.push(data)
  }

  /** Plays the server accepting the connection. */
  open(): void {
    this.readyState = 1
    this.onopen?.()
  }

  /** Plays the server sending a raw message: a string, or the bytes of a binary one. */
  receive(data: string | ArrayBuffer): void {
    this.onmessage?.({ data })
  }

  /** Plays the connection ending. */
  drop(): void {
    this.readyState = 3
    this.closeListeners.forEach((listener) => listener())
    this.onclose?.()
  }

  /** Plays the server sending an event. */
  emit(type: string, data: unknown): void {
    this.onmessage?.({ data: JSON.stringify({ type, data }) })
  }
}

/** Builds a transport whose event stream opens {@link FakeSocket}s, so tests can play the server. */
export function fakeSocketTransport(): HttpTransport {
  return new HttpTransport({ webSocket: FakeSocket as unknown as typeof WebSocket })
}
