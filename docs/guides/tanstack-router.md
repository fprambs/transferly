# TanStack Router

Source: [TanStack Router docs](https://tanstack.com/router/latest/docs/framework/react/overview), [Authenticated routes](https://tanstack.com/router/latest/docs/framework/react/guide/authenticated-routes). Package: `@tanstack/react-router`.

## Setup

- Code-based routing, no file-based plugin: `createRootRouteWithContext<RouterContext>()`, `createRoute({ getParentRoute, path, component })`, `createRouter({ routeTree, context })`, `<RouterProvider router={router} />`.
- The router context carries the `QueryClient` and the session state, so guards and loaders read them without importing modules.
- Routes live in `src/app/routes/`, one module per screen, and are joined in `src/app/router.tsx`.

## Rules

- Guard with `beforeLoad` and `throw redirect({ to: '/login', search: { redirect: location.href } })`. A guard that awaits the network wraps the call in `try/catch` and rethrows redirects (`isRedirect`).
- Params and search are validated with `validateSearch` (zod); components read them with `useSearch` and `useParams`, never from `location`.
- Navigate with `<Link>` and `useNavigate`; never assign `window.location`.
- Screens are code split with `lazyRouteComponent` only when they are large (the explorer); the shell and the auth screens load with the first bundle.
- Data loading belongs to TanStack Query (`ensureQueryData` in loaders is optional); routes do not fetch on their own.

## Tests

Render routes with `createMemoryHistory({ initialEntries })` and a router built by the same factory the app uses, so the tests exercise real guards. Assert the landmarks and the final location, not the internals of the router.
