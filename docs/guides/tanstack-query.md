# TanStack Query v5

Source: [Important defaults](https://tanstack.com/query/latest/docs/framework/react/guides/important-defaults).

## Defaults

- Data is stale immediately (`staleTime: 0`); refetch on mount, window focus and reconnect.
- Inactive queries are garbage collected after 5 minutes (`gcTime`).
- Failed queries retry 3 times with exponential backoff.
- Results are structurally shared.

## Rules

1. Set `staleTime` per data type.
2. Invalidate with `invalidateQueries` after mutations; use hierarchical keys (`['connections', id, 'fs', path]`).
3. Define queries with `queryOptions` in one module per feature.
4. WebSocket events update the cache (`setQueryData`, `invalidateQueries`), not Zustand.
5. Query functions call the `Transport` interface, never `fetch`.
6. Disable retries for 401, 403 and 404.

## Tests

A fresh `QueryClient` per test with `retry: false`; API mocked with MSW.
