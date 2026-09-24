# Playwright

Source: [Best practices](https://playwright.dev/docs/best-practices).

## Rules

1. Locators by role and accessible text; `data-testid` as last resort; no brittle CSS or XPath.
2. Web-first assertions (`await expect(locator).toBeVisible()`); no `waitForTimeout`.
3. Isolated tests: own data, cookies and storage; shared setup in `beforeEach` or fixtures.
4. Mock third-party services; use the local `testenv` FTP and SFTP servers for real.
5. Visual regression only inside the official Playwright container, with versioned baselines.
6. CI: install only needed browsers, `trace: 'on-first-retry'`, keep artifacts on failure.
7. TypeScript with ESLint `no-floating-promises`.
8. `expect.soft` when several failures should be reported together.
9. Keep Playwright and the container image on the same version.

## Running in Docker

- `compose.test.yaml` runs the suite in `mcr.microsoft.com/playwright` with the same version as `@playwright/test` (pinned exactly), `ipc: host` and `init: true`, against the production image.
- Generate baselines only with `make e2e-update`; `make e2e` compares against them.
- Never use a single-label host such as `app`, `dev` or `page` for the server: those are HSTS-preloaded top-level domains and Chromium forces HTTPS (`ERR_SSL_PROTOCOL_ERROR`).
- Relative imports in `e2e/` need `.ts` extensions and fixtures cannot use parameter properties (`erasableSyntaxOnly`).
- The oxlint `react/rules-of-hooks` rule is disabled for `e2e/`: the fixture callback `use` is not a React hook.
- Page objects resolve language-dependent labels from the page language, not from hard-coded English.

## Layout

`frontend/e2e/{pages,fixtures,specs}` and `playwright.config.ts`.
