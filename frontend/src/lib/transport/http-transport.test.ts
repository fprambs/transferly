import { describe, expect, it, vi } from 'vitest'

import { HttpTransport } from './http-transport'
import { ApiError } from './types'

/** Builds a fetch stub that answers every request with the given response and records the calls. */
function stubFetch(response: Response | Error) {
  const calls: string[] = []
  const fetchStub: typeof fetch = (input) => {
    calls.push(String(input))
    return response instanceof Error ? Promise.reject(response) : Promise.resolve(response)
  }
  return { fetchStub, calls }
}

/** Builds a JSON response with the given status and content type. */
function jsonResponse(body: unknown, status: number, contentType = 'application/json'): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': contentType },
  })
}

/**
 * Covers {@link HttpTransport}.
 *
 * Scenarios:
 * - health and readiness return the reported status and use the base URL
 * - a 503 readiness answer is a valid `unavailable` status
 * - RFC 9457 problem documents become an ApiError carrying the stable code and params
 * - failures without a problem document, with or without a content type, become an ApiError with the generic code
 * - a 503 readiness answer that is not JSON (for example from a proxy) is an error, not a status
 * - a successful answer with a malformed body becomes an ApiError with code `invalid_response`
 * - unreachable servers become an ApiError with status 0 and code `network_error`
 * - without an injected fetch, the global fetch is used, resolved at call time
 */
describe('HttpTransport', () => {
  it('reads the health status and requests /healthz under the base URL', async () => {
    const { fetchStub, calls } = stubFetch(jsonResponse({ status: 'ok' }, 200))
    const transport = new HttpTransport({ baseUrl: 'http://api.test', fetch: fetchStub })

    await expect(transport.health()).resolves.toEqual({ status: 'ok' })
    expect(calls).toEqual(['http://api.test/healthz'])
  })

  it('reads the readiness status', async () => {
    const { fetchStub, calls } = stubFetch(jsonResponse({ status: 'ok' }, 200))
    const transport = new HttpTransport({ fetch: fetchStub })

    await expect(transport.ready()).resolves.toEqual({ status: 'ok' })
    expect(calls).toEqual(['/readyz'])
  })

  it('treats a 503 readiness answer as an unavailable status, not an error', async () => {
    const { fetchStub } = stubFetch(jsonResponse({ status: 'unavailable' }, 503))
    const transport = new HttpTransport({ fetch: fetchStub })

    await expect(transport.ready()).resolves.toEqual({ status: 'unavailable' })
  })

  it('turns a problem document into an ApiError with its stable code and params', async () => {
    const problem = {
      type: 'about:blank',
      title: 'Not Found',
      status: 404,
      code: 'not_found',
      params: { id: 7 },
    }
    const { fetchStub } = stubFetch(jsonResponse(problem, 404, 'application/problem+json'))
    const transport = new HttpTransport({ fetch: fetchStub })

    const error = await transport.health().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).code).toBe('not_found')
    expect((error as ApiError).problem).toEqual(problem)
  })

  it('turns a failure without a problem document into a generic ApiError', async () => {
    const { fetchStub } = stubFetch(new Response('boom', { status: 500, statusText: 'Server Error' }))
    const transport = new HttpTransport({ fetch: fetchStub })

    const error = await transport.health().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).problem).toMatchObject({ status: 500, code: 'unknown_error' })
  })

  it('reports an unreachable server as a network_error with status 0', async () => {
    const { fetchStub } = stubFetch(new TypeError('Failed to fetch'))
    const transport = new HttpTransport({ fetch: fetchStub })

    const error = await transport.ready().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).problem).toMatchObject({ status: 0, code: 'network_error' })
  })

  it('turns a failure without a content type into a generic ApiError', async () => {
    const { fetchStub } = stubFetch(new Response(null, { status: 500 }))
    const transport = new HttpTransport({ fetch: fetchStub })

    const error = await transport.health().catch((e: unknown) => e)
    expect((error as ApiError).problem).toMatchObject({ status: 500, code: 'unknown_error' })
  })

  it('treats a non-JSON 503 readiness answer as an error', async () => {
    const html = new Response('<h1>Bad gateway</h1>', {
      status: 503,
      headers: { 'Content-Type': 'text/html' },
    })
    const { fetchStub } = stubFetch(html)
    const transport = new HttpTransport({ fetch: fetchStub })

    const error = await transport.ready().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).problem).toMatchObject({ status: 503, code: 'unknown_error' })
  })

  it('reports a successful answer with a malformed body as invalid_response', async () => {
    const { fetchStub } = stubFetch(new Response('not json', { status: 200 }))
    const transport = new HttpTransport({ fetch: fetchStub })

    const error = await transport.health().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).problem).toMatchObject({ status: 200, code: 'invalid_response' })
  })

  it('uses the global fetch, looked up when the request is sent', async () => {
    const transport = new HttpTransport()
    const globalFetch = vi.fn(() => Promise.resolve(jsonResponse({ status: 'ok' }, 200)))
    vi.stubGlobal('fetch', globalFetch)

    await expect(transport.health()).resolves.toEqual({ status: 'ok' })
    expect(globalFetch).toHaveBeenCalledWith('/healthz', { method: 'GET' })
  })
})
