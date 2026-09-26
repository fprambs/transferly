import {
  type ApiQuery,
  type ApiRequest,
  ApiError,
  type EventHandlers,
  type HealthStatus,
  type Problem,
  type ServerEvent,
  type Transport,
} from './types'

const problemMediaType = 'application/problem+json'
const jsonMediaType = 'application/json'

/** Options for {@link HttpTransport}. */
export interface HttpTransportOptions {
  /** Prefix prepended to every request path; empty for same-origin requests. */
  baseUrl?: string
  /** Function used to send requests; defaults to the global `fetch`. */
  fetch?: typeof fetch
  /** Constructor used to open the event stream; defaults to the global `WebSocket`. */
  webSocket?: typeof WebSocket
}

/** {@link Transport} that talks to the Sentinel backend over HTTP. */
export class HttpTransport implements Transport {
  /** Prefix prepended to every request path. */
  private readonly baseUrl: string
  /** Function used to send requests. */
  private readonly send: typeof fetch
  /** Constructor used to open the event stream. */
  private readonly openSocket: (url: string) => WebSocket
  /** Anti-CSRF token sent with state-changing calls. */
  private csrfToken: string | undefined

  /** Creates a transport from the given options. */
  constructor(options: HttpTransportOptions = {}) {
    this.baseUrl = options.baseUrl ?? ''
    this.send = options.fetch ?? ((input, init) => fetch(input, init))
    const Socket = options.webSocket
    this.openSocket = Socket === undefined ? (url) => new WebSocket(url) : (url) => new Socket(url)
  }

  /** Sets the anti-CSRF token sent with every call that is not a read. */
  setCsrfToken(token: string | undefined): void {
    this.csrfToken = token
  }

  /** Builds the URL of a path under the base URL, with its query string. */
  url(path: string, query?: ApiQuery): string {
    const parameters = new URLSearchParams()
    for (const [name, value] of Object.entries(query ?? {})) {
      if (value !== undefined) {
        parameters.set(name, String(value))
      }
    }
    const text = parameters.toString()
    return `${this.baseUrl}${path}${text === '' ? '' : `?${text}`}`
  }

  /** Opens `GET /api/v1/ws`, on the same origin as the page unless a base URL was given. */
  events(handlers: EventHandlers): () => void {
    const socket = this.openSocket(eventsUrl(this.baseUrl))
    socket.onopen = () => handlers.onOpen()
    socket.onclose = () => handlers.onClose()
    socket.onmessage = (message) => {
      const event = parseEvent(message.data)
      if (event !== undefined) {
        handlers.onEvent(event)
      }
    }
    return () => {
      socket.onclose = null
      socket.close()
    }
  }

  /** Performs an API call, sending the session cookie and, for changes, the anti-CSRF token. */
  async request<T>(request: ApiRequest): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    let body: BodyInit | undefined
    if (isBinary(request.body)) {
      body = request.body as BodyInit
      headers['Content-Type'] = request.contentType ?? 'application/octet-stream'
    } else if (request.body !== undefined) {
      body = JSON.stringify(request.body)
      headers['Content-Type'] = jsonMediaType
    }
    if (request.method !== 'GET' && this.csrfToken !== undefined) {
      headers['X-CSRF-Token'] = this.csrfToken
    }
    let response: Response
    try {
      response = await this.send(this.url(request.path, request.query), { method: request.method, headers, body, signal: request.signal })
    } catch {
      throw new ApiError({ type: 'about:blank', title: 'Network Error', status: 0, code: 'network_error' })
    }
    if (!response.ok) {
      throw new ApiError(await readProblem(response))
    }
    if (response.status === 204) {
      return undefined as T
    }
    return (await this.parse(response)) as T
  }

  /** Performs a GET call and returns the body as a stream, with the session cookie. */
  async stream(request: ApiRequest): Promise<ReadableStream<Uint8Array>> {
    let response: Response
    try {
      response = await this.send(this.url(request.path, request.query), { method: 'GET', signal: request.signal })
    } catch {
      throw new ApiError({ type: 'about:blank', title: 'Network Error', status: 0, code: 'network_error' })
    }
    if (!response.ok) {
      throw new ApiError(await readProblem(response))
    }
    if (response.body === null) {
      throw new ApiError({ type: 'about:blank', title: 'Invalid Response', status: response.status, code: 'invalid_response' })
    }
    return response.body
  }

  /** Calls `GET /healthz`. */
  async health(): Promise<HealthStatus> {
    return this.readStatus(await this.get('/healthz'))
  }

  /** Calls `GET /readyz`; a JSON 503 answer is a valid `unavailable` status, not an error. */
  async ready(): Promise<HealthStatus> {
    const response = await this.get('/readyz')
    if (response.status === 503 && hasMediaType(response, jsonMediaType)) {
      return this.parse(response)
    }
    return this.readStatus(response)
  }

  /** Sends a GET request, mapping network failures to an {@link ApiError} with code `network_error`. */
  private async get(path: string): Promise<Response> {
    try {
      return await this.send(`${this.baseUrl}${path}`, { method: 'GET' })
    } catch {
      throw new ApiError({ type: 'about:blank', title: 'Network Error', status: 0, code: 'network_error' })
    }
  }

  /** Reads a health document from a successful response, or throws the problem of a failed one. */
  private async readStatus(response: Response): Promise<HealthStatus> {
    if (!response.ok) {
      throw new ApiError(await readProblem(response))
    }
    return this.parse(response)
  }

  /** Parses a JSON health document, mapping malformed bodies to an {@link ApiError} with code `invalid_response`. */
  private async parse(response: Response): Promise<HealthStatus> {
    try {
      return (await response.json()) as HealthStatus
    } catch {
      throw new ApiError({
        type: 'about:blank',
        title: 'Invalid Response',
        status: response.status,
        code: 'invalid_response',
      })
    }
  }
}

/** Event types the server sends. */
const eventTypes = ['snapshot', 'transfer.state', 'transfer.progress', 'vault.locked']

/** Builds the address of the event stream: `ws` or `wss` to match the page, or the base URL. */
function eventsUrl(baseUrl: string): string {
  const url = new URL('/api/v1/ws', baseUrl === '' ? window.location.href : baseUrl)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return url.toString()
}

/** Reads a stream message, or returns `undefined` when it is not JSON or not an event the client knows. */
function parseEvent(data: unknown): ServerEvent | undefined {
  try {
    const event = JSON.parse(String(data)) as { type?: unknown }
    return typeof event.type === 'string' && eventTypes.includes(event.type) ? (event as ServerEvent) : undefined
  } catch {
    return undefined
  }
}

/** Reports whether a request body is binary data, which is sent as it is instead of as JSON. */
function isBinary(body: unknown): boolean {
  return body instanceof Blob || body instanceof ArrayBuffer || ArrayBuffer.isView(body)
}

/** Reports whether a response carries the given media type. */
function hasMediaType(response: Response, mediaType: string): boolean {
  return response.headers.get('Content-Type')?.includes(mediaType) ?? false
}

/** Reads the problem document of a failed response, or builds a generic one when it has none. */
async function readProblem(response: Response): Promise<Problem> {
  if (hasMediaType(response, problemMediaType)) {
    return (await response.json()) as Problem
  }
  return {
    type: 'about:blank',
    title: response.statusText,
    status: response.status,
    code: 'unknown_error',
  }
}
