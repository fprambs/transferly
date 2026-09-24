/** Status reported by a health endpoint. */
export interface HealthStatus {
  /** `ok` when the checked component can serve traffic. */
  status: 'ok' | 'unavailable'
}

/** RFC 9457 problem document returned by the backend on errors. */
export interface Problem {
  /** URI reference identifying the problem type; `about:blank` when generic. */
  type: string
  /** Fixed English summary meant for logs, never for display. */
  title: string
  /** HTTP status code, or 0 when the server was unreachable. */
  status: number
  /** Stable machine-readable code that the interface translates. */
  code: string
  /** Values used to fill the translated message. */
  params?: Record<string, unknown>
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
