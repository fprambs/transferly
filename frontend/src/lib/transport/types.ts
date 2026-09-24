import type { components } from '../api/schema'

/** Status reported by a health endpoint; generated from the OpenAPI contract. */
export type HealthStatus = components['schemas']['HealthStatus']

/** RFC 9457 problem document returned by the backend on errors; generated from the OpenAPI contract. */
export type Problem = components['schemas']['Problem']

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
