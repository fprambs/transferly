import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.SENTINEL_E2E_URL
if (baseURL === undefined) {
  throw new Error('SENTINEL_E2E_URL is not set. Run the browser tests with `make e2e`, which uses the isolated test stack and never a development instance.')
}

export default defineConfig({
  globalSetup: './e2e/global-setup.ts',
  testDir: './e2e/specs',
  snapshotPathTemplate: '{testDir}/../__screenshots__/{testFilePath}/{arg}{ext}',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.01 } },
  use: {
    baseURL,
    trace: 'on-first-retry',
    locale: 'en-US',
    timezoneId: 'UTC',
    colorScheme: 'light',
    storageState: 'e2e/.auth/state.json',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
