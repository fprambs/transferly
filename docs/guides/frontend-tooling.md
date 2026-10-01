# Frontend Tooling

Sources: [Vite guide](https://vite.dev/guide/), [Vitest guide](https://vitest.dev/guide/), [Vitest config](https://vitest.dev/config/), [Testing Library](https://testing-library.com/docs/react-testing-library/intro/), [oxlint rules](https://oxc.rs/docs/guide/usage/linter/rules.html).

## Versions

Vite 8, Vitest 5 (needs Node 22.12 or later), TypeScript 6, React 19, oxlint (Vite template default), jsdom, Testing Library.

## Setup

- Scaffold: `pnpm create vite frontend --template react-ts --no-interactive`.
- The current Vite template lints with oxlint, not ESLint; Transferly keeps that default.
- Tests share `vite.config.ts` through `vitest/config`; `vitest run` is the CI mode.
- Dev server inside Docker binds with `--host 0.0.0.0`.

## Rules

- Test with Testing Library the way users interact: query by role, then label and text.
- Use `userEvent` for interaction; clean up in a shared setup file.
- Inject dependencies (for example `fetch`) instead of stubbing globals.
- oxlint enforces `no-restricted-imports`, `no-restricted-globals` and `no-inline-comments`; `frontend/guards` enforces documentation.

## Commands

`pnpm lint`, `pnpm typecheck`, `pnpm test`, `pnpm test:coverage`, `pnpm build`.
