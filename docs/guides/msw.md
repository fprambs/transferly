# Mock Service Worker

Source: [MSW quick start](https://mswjs.io/docs/quick-start). Version 2: `http` and `HttpResponse` from `msw`, `setupServer` from `msw/node`.

## Setup

- One server for the unit tests in `src/test/msw.ts`, started in `src/test/setup.ts`: `server.listen({ onUnhandledRequest: 'error' })` in `beforeAll`, `server.resetHandlers()` in `afterEach`, `server.close()` in `afterAll`. An unhandled request fails the test, so a screen cannot call an endpoint nobody mocked.
- Default handlers live in `src/test/handlers/` per API area and describe the happy path; a test overrides one with `server.use(http.get(...))`.
- Handlers answer with the same shapes as the OpenAPI contract, typed with the generated schema types, and errors as `application/problem+json` problems.

## Rules

- Tests assert what the user sees and what was sent (method, path, body, CSRF header), never how the client is implemented.
- Requests are recorded through the handler (`request.clone().json()`), not by spying on `fetch`.

## End to end

The Playwright tests do not use MSW: they run against the production image and a real Postgres, with the SFTP server of the test environment.
