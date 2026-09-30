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

/** Builds a fetch stub that records the init of each call and answers with the given response. */
function recorder(make: () => Response | Error) {
  const inits: RequestInit[] = []
  const urls: string[] = []
  const fetchStub: typeof fetch = (input, init) => {
    urls.push(String(input))
    inits.push(init ?? {})
    const response = make()
    return response instanceof Error ? Promise.reject(response) : Promise.resolve(response)
  }
  return { fetchStub, inits, urls }
}

/**
 * Covers the `request`, `stream`, `url` and `setCsrfToken` methods of {@link HttpTransport}.
 *
 * Scenarios:
 * - a JSON call sends the body as JSON, accepts JSON and returns the parsed answer
 * - the anti-CSRF token is sent with changes, never with reads, and can be forgotten
 * - binary bodies (blobs, buffers, typed arrays) are sent as they are, not as JSON, with the media type they are given
 *   or octet-stream
 * - problem documents become an ApiError, network failures a `network_error`, and answers without a body resolve empty
 * - a stream call returns the body of the answer, asks for a byte range when given one, and reports problems, network failures and answers
 *   without a body
 * - the URL joins the base URL, the path and the encoded query, repeating a parameter for each item of a list and leaving out undefined values
 * - the abort signal is passed to fetch
 */
describe('HttpTransport.request', () => {
  it('streams the body of a GET answer', async () => {
    const { fetchStub, inits, urls } = recorder(() => new Response('file bytes'))
    const stream = await new HttpTransport({ baseUrl: 'http://api.test', fetch: fetchStub }).stream({ method: 'GET', path: '/f', query: { path: '/a' } })

    expect(await new Response(stream).text()).toBe('file bytes')
    expect(urls).toEqual(['http://api.test/f?path=%2Fa'])
    expect(inits[0]).toMatchObject({ method: 'GET' })
    expect((inits[0].headers as Record<string, string>).Range).toBeUndefined()
  })

  it('asks for a range of a download', async () => {
    const { fetchStub, inits } = recorder(() => new Response('part', { status: 206 }))
    await new HttpTransport({ fetch: fetchStub }).stream({ method: 'GET', path: '/f', range: { start: 0, end: 4095 } })

    expect((inits[0].headers as Record<string, string>).Range).toBe('bytes=0-4095')
  })

  it('reports the problem, the network failure and the missing body of a stream', async () => {
    const failing = recorder(() => jsonResponse({ type: 'about:blank', title: 'Not found', status: 404, code: 'path_not_found' }, 404, 'application/problem+json'))
    await expect(new HttpTransport({ fetch: failing.fetchStub }).stream({ method: 'GET', path: '/f' })).rejects.toMatchObject({ code: 'path_not_found' })

    const offline = recorder(() => new TypeError('offline'))
    await expect(new HttpTransport({ fetch: offline.fetchStub }).stream({ method: 'GET', path: '/f' })).rejects.toMatchObject({ code: 'network_error' })

    const empty = recorder(() => new Response(null, { status: 204 }))
    await expect(new HttpTransport({ fetch: empty.fetchStub }).stream({ method: 'GET', path: '/f' })).rejects.toMatchObject({ code: 'invalid_response' })
  })

  it('sends JSON and returns the parsed answer', async () => {
    const { fetchStub, inits, urls } = recorder(() => jsonResponse({ id: 1 }, 200))
    const transport = new HttpTransport({ baseUrl: 'http://api.test', fetch: fetchStub })

    await expect(transport.request({ method: 'POST', path: '/api/v1/x', body: { a: 1 } })).resolves.toEqual({ id: 1 })

    expect(urls).toEqual(['http://api.test/api/v1/x'])
    expect(inits[0]).toMatchObject({ method: 'POST', body: '{"a":1}' })
    expect(inits[0]?.headers).toMatchObject({ Accept: 'application/json', 'Content-Type': 'application/json' })
  })

  it('sends the anti-CSRF token with changes only, until it is forgotten', async () => {
    const { fetchStub, inits } = recorder(() => jsonResponse({}, 200))
    const transport = new HttpTransport({ fetch: fetchStub })
    transport.setCsrfToken('token')

    await transport.request({ method: 'GET', path: '/a' })
    await transport.request({ method: 'DELETE', path: '/a' })
    transport.setCsrfToken(undefined)
    await transport.request({ method: 'DELETE', path: '/a' })

    expect(inits.map((init) => (init.headers as Record<string, string>)['X-CSRF-Token'])).toEqual([undefined, 'token', undefined])
  })

  it.each([
    ['a blob', new Blob(['x'])],
    ['an array buffer', new ArrayBuffer(2)],
    ['a typed array', new Uint8Array([1, 2])],
  ])('sends %s as binary data', async (_name, body) => {
    const { fetchStub, inits } = recorder(() => jsonResponse({}, 200))

    await new HttpTransport({ fetch: fetchStub }).request({ method: 'PUT', path: '/a', body })

    expect(inits[0]?.body).toBe(body)
    expect(inits[0]?.headers).toMatchObject({ 'Content-Type': 'application/octet-stream' })
  })

  it('sends a binary body with the media type it is given', async () => {
    const { fetchStub, inits } = recorder(() => jsonResponse({}, 200))

    await new HttpTransport({ fetch: fetchStub }).request({ method: 'POST', path: '/a', body: new Uint8Array([1]), contentType: 'application/xml' })

    expect(inits[0]?.headers).toMatchObject({ 'Content-Type': 'application/xml' })
  })

  it('throws the problem of a failed call and resolves empty for answers without a body', async () => {
    const failing = recorder(() => jsonResponse({ type: 'about:blank', title: 'Conflict', status: 409, code: 'path_exists' }, 409, 'application/problem+json'))
    await expect(new HttpTransport({ fetch: failing.fetchStub }).request({ method: 'POST', path: '/a' })).rejects.toMatchObject({ code: 'path_exists' })

    const empty = recorder(() => new Response(null, { status: 204 }))
    await expect(new HttpTransport({ fetch: empty.fetchStub }).request({ method: 'DELETE', path: '/a' })).resolves.toBeUndefined()
  })

  it('maps network failures to a network_error', async () => {
    const { fetchStub } = recorder(() => new TypeError('offline'))

    await expect(new HttpTransport({ fetch: fetchStub }).request({ method: 'GET', path: '/a' })).rejects.toMatchObject({ code: 'network_error', problem: { status: 0 } })
  })

  it('builds URLs with the encoded query, repeated parameters for lists and without undefined values', () => {
    const transport = new HttpTransport({ baseUrl: 'http://api.test' })

    expect(transport.url('/p', { inner: ['a.zip', 'b c.tar'], empty: [] })).toBe('http://api.test/p?inner=a.zip&inner=b+c.tar')
    expect(transport.url('/p', { path: '/a b', n: 2, flag: true, none: undefined })).toBe('http://api.test/p?path=%2Fa+b&n=2&flag=true')
    expect(transport.url('/p')).toBe('http://api.test/p')
    expect(transport.url('/p', { none: undefined })).toBe('http://api.test/p')
  })

  it('passes the abort signal to fetch', async () => {
    const { fetchStub, inits } = recorder(() => jsonResponse({}, 200))
    const controller = new AbortController()

    await new HttpTransport({ fetch: fetchStub }).request({ method: 'GET', path: '/a', signal: controller.signal })

    expect(inits[0]?.signal).toBe(controller.signal)
  })
})
