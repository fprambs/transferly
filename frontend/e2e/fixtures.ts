import { readFile } from 'node:fs/promises'

import { test as base } from '@playwright/test'

import { quickConnectionPath } from './global-setup.ts'
import { ShellPage } from './pages/shell-page.ts'

/**
 * Test fixtures: the account's saved theme, kept per test so parallel tests cannot change each other's look (a test that
 * wants the real account passes `accountTheme: 'real'`), an application shell that is already open, signed in by the global setup, and the shared quick connection. */
export const test = base.extend<{ shell: ShellPage; quickId: string; accountTheme: 'mocked' | 'real' }>({
  accountTheme: ['mocked', { option: true }],
  page: async ({ page, accountTheme }, use) => {
    if (accountTheme === 'mocked') {
      let theme = 'sentinel-dark'
      await page.route('**/api/v1/me', async (route) => {
        const request = route.request()
        if (request.method() === 'PATCH') {
          theme = (request.postDataJSON() as { theme?: string }).theme ?? theme
        }
        const response = await route.fetch({ method: 'GET', postData: undefined })
        const body = (await response.json()) as { user: Record<string, unknown> }
        const user = { ...body.user, theme }
        await route.fulfill({ status: 200, json: request.method() === 'PATCH' ? user : { ...body, user } })
      })
    }
    await use(page)
  },
  quickId: async ({ page: _page }, use) => {
    const { quickId } = JSON.parse(await readFile(quickConnectionPath, 'utf8')) as { quickId: string }
    await use(quickId)
  },
  shell: async ({ page }, use) => {
    const shell = new ShellPage(page)
    await shell.goto()
    await use(shell)
  },
})

export { expect } from '@playwright/test'
