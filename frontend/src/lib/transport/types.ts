import type { components } from '../api/schema'

/** Status reported by a health endpoint; generated from the OpenAPI contract. */
export type HealthStatus = components['schemas']['HealthStatus']

/** RFC 9457 problem document returned by the backend on errors; generated from the OpenAPI contract. */
export type Problem = components['schemas']['Problem']

/** HTTP methods of the API. */
export type ApiMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

/** Values that may appear in a query string; `undefined` ones are left out. */
export type ApiQuery = Readonly<Record<string, string | number | boolean | undefined>>

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
  /** Aborts the call. */
  signal?: AbortSignal
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
  /** Returns the URL of a path, for links the browser follows itself such as downloads. */
  url(path: string, query?: ApiQuery): string
  /** Sets the anti-CSRF token sent with every state-changing call; `undefined` forgets it. */
  setCsrfToken(token: string | undefined): void
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
