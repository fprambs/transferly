import { ApiError, type HealthStatus, type Problem, type Transport } from './types'

const problemMediaType = 'application/problem+json'
const jsonMediaType = 'application/json'

/** Options for {@link HttpTransport}. */
export interface HttpTransportOptions {
  /** Prefix prepended to every request path; empty for same-origin requests. */
  baseUrl?: string
  /** Function used to send requests; defaults to the global `fetch`. */
  fetch?: typeof fetch
}

/** {@link Transport} that talks to the Sentinel backend over HTTP. */
export class HttpTransport implements Transport {
  /** Prefix prepended to every request path. */
  private readonly baseUrl: string
  /** Function used to send requests. */
  private readonly send: typeof fetch

  /** Creates a transport from the given options. */
  constructor(options: HttpTransportOptions = {}) {
    this.baseUrl = options.baseUrl ?? ''
    this.send = options.fetch ?? ((input, init) => fetch(input, init))
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
