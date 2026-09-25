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
