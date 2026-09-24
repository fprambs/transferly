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

## Layout

`frontend/e2e/{pages,fixtures,specs}` and `playwright.config.ts`.
