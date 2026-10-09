import type { components } from '../api/schema'

/** Status reported by a health endpoint; generated from the OpenAPI contract. */
export type HealthStatus = components['schemas']['HealthStatus']

/** RFC 9457 problem document returned by the backend on errors; generated from the OpenAPI contract. */
export type Problem = components['schemas']['Problem']

/** A recorded upload or download; generated from the OpenAPI contract. */
export type TransferRecord = components['schemas']['Transfer']

/** One step of a connection to a site, as the server tells it (ADR-0011): a stable step code and the values its message shows. */
export interface ConnectionLogEntry {
  /** Workspace of the site. */
  workspaceId: string
  /** Site, or quick connection, the step is about. */
  siteId: string
  /** When it happened, in RFC 3339. */
  time: string
  /** How it reads. */
  level: 'info' | 'warn' | 'error'
  /** Stable code of the step. */
  step: string
  /** Values of the message; never a secret. */
  params: Record<string, string>
}

/** One question of a {@link ConnectionPrompt}. */
export interface PromptQuestion {
  /** The question as the server words it, such as "Verification code:". */
  text: string
  /** Whether the answer may be shown while typed; a password or a code is not. */
  echo: boolean
}

/** What a server asks while an interactive site connects (ADR-0016), for the person connecting to answer. */
export interface ConnectionPrompt {
  /** Identifier to answer or decline it with. */
  id: string
  /** Workspace of the site. */
  workspaceId: string
  /** Site, or quick connection, that asks. */
  siteId: string
  /** The server's title of the challenge; may be empty. */
  name: string
  /** The server's explanation of the challenge; may be empty. */
  instruction: string
  /** The questions, answered in order. */
  questions: PromptQuestion[]
}

/** A message of the server's event stream. */
export type ServerEvent =
  | { type: 'snapshot'; data: { transfers: TransferRecord[] } }
  | { type: 'transfer.state'; data: TransferRecord }
  | { type: 'transfer.progress'; data: { id: string; bytesDone: number; totalBytes: number } }
  | { type: 'vault.locked'; data: null }
  | { type: 'connection.log'; data: ConnectionLogEntry }
  | { type: 'connection.state'; data: { workspaceId: string; siteId: string; open: boolean } }
  | { type: 'connection.prompt'; data: ConnectionPrompt }
  | { type: 'connection.prompt_closed'; data: { id: string } }

/** What to do with the events of the server's stream and with the life of its connection. */
export interface EventHandlers {
  /** Called for every event that arrives. */
  onEvent: (event: ServerEvent) => void
  /** Called when the stream is open, the first time and after every reconnection made by the caller. */
  onOpen: () => void
  /** Called when the stream ends without the caller asking for it. */
  onClose: () => void
}

/** HTTP methods of the API. */
export type ApiMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

/** Values that may appear in a query string; `undefined` ones are left out, and a list repeats its parameter once per item. */
export type ApiQuery = Readonly<Record<string, string | number | boolean | readonly string[] | undefined>>

/** One call to the API. */
export interface ApiRequest {
  /** HTTP method. */
  method: ApiMethod
  /** Path below the API root, such as `/api/v1/me`. */
  path: string
  /** Query string parameters. */
  query?: ApiQuery
  /** JSON body, or binary data (a `Blob`, `ArrayBuffer` or typed array) sent as it is. */
  body?: unknown
  /** Media type of a binary body; `application/octet-stream` when left out. */
  contentType?: string
  /** Aborts the call. */
  signal?: AbortSignal
  /** Asks for only the bytes from `start` to `end`, both included, of a download (ADR-0018). */
  range?: { start: number; end: number }
  /** Extra headers; values are sent only for the request. */
  headers?: Readonly<Record<string, string>>
}

/**
 * Boundary between the interface and the backend.
 * The web build uses `HttpTransport`; a desktop build can provide its own implementation.
 */
export interface Transport {
  /** Checks that the backend process is alive. */
  health(): Promise<HealthStatus>
  /** Checks that the backend and its dependencies can serve traffic. */
  ready(): Promise<HealthStatus>
  /**
   * Performs an API call and returns its JSON answer, or `undefined` for answers without a body.
   * Failures are thrown as {@link ApiError}.
   */
  request<T>(request: ApiRequest): Promise<T>
  /**
   * Performs a call and returns the body of the answer as a stream of bytes, as file downloads need it.
   * Failures, and answers without a body, are thrown as {@link ApiError}.
   */
  stream(request: ApiRequest): Promise<ReadableStream<Uint8Array>>
  /** Returns the URL of a path, for links the browser follows itself such as downloads. */
  url(path: string, query?: ApiQuery): string
  /**
   * Opens the server's event stream: transfer state and progress, and vault locks.
   *
   * @returns A function that closes the stream; after it, `onClose` is not called.
   */
  events(handlers: EventHandlers): () => void
  /** Opens a WebSocket to a path of the server, with a query, as a terminal needs (ADR-0027). */
  socket(path: string, query?: ApiQuery): WebSocket
  /** Sets the anti-CSRF token sent with every state-changing call; `undefined` forgets it. */
  setCsrfToken(token: string | undefined): void
  /** Cancels resources of an old identity before an explicit login or logout transition. */
  resetSession?(): void
  /**
   * Registers a callback for answers with code `unauthenticated` (session cookie expired or invalid).
   * Implementations that do not track sessions may omit this.
   */
  setOnUnauthenticated?(handler: (() => void) | undefined): void
}

/** Error raised by a {@link Transport} when a request fails. */
export class ApiError extends Error {
  /** The problem document that describes the failure. */
  readonly problem: Problem

  /** Creates an error from a problem document. */
  constructor(problem: Problem) {
    super(`${problem.status} ${problem.code}`)
    this.name = 'ApiError'
    this.problem = problem
  }

  /** Stable machine-readable code of the failure. */
  get code(): string {
    return this.problem.code
  }
}
