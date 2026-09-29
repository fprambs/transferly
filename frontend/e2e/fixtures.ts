import { readFile } from 'node:fs/promises'

import { test as base } from '@playwright/test'

import { quickConnectionPath } from './global-setup.ts'
import { ShellPage } from './pages/shell-page.ts'

/**
 * Test fixtures: the account's saved preferences (theme, language, double click), kept per test so parallel tests cannot change each
 * other's look or language (a test that wants the real account passes `accountTheme: 'real'`), an application shell that is already open, signed in by the global setup, the shared quick connection,
 * and an empty folder of the test's own on the SFTP server of the test stack, removed when the test ends, so what a test lists never depends on what other
 * tests left behind. */
export const test = base.extend<{ shell: ShellPage; quickId: string; folder: string; accountTheme: 'mocked' | 'real' }>({
  accountTheme: ['mocked', { option: true }],
  page: async ({ page, accountTheme }, use) => {
    if (accountTheme === 'mocked') {
      const preferences: Record<string, unknown> = { theme: 'sentinel-dark' }
      await page.route('**/api/v1/me', async (route) => {
        const request = route.request()
        if (request.method() === 'PATCH') {
          Object.assign(preferences, request.postDataJSON() as Record<string, unknown>)
        }
        const response = await route.fetch({ method: 'GET', postData: undefined })
        const body = (await response.json()) as { user: Record<string, unknown> }
        const user = { ...body.user, ...preferences }
        await route.fulfill({ status: 200, json: request.method() === 'PATCH' ? user : { ...body, user } })
      })
    }
    await use(page)
  },
  quickId: async ({ page: _page }, use) => {
    const { quickId } = JSON.parse(await readFile(quickConnectionPath, 'utf8')) as { quickId: string }
    await use(quickId)
  },
  folder: async ({ request, quickId }, use) => {
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const path = `/upload/e2e-${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const created = await request.post(`${files}/directories`, { headers, data: { path } })
    if (!created.ok()) {
      throw new Error(`Could not create the folder of the test: ${created.status()}`)
    }
    await use(path)
    await request.delete(files, { params: { path, recursive: 'true' }, headers })
  },
  shell: async ({ page }, use) => {
    const shell = new ShellPage(page)
    await shell.goto()
    await use(shell)
  },
})

export { expect } from '@playwright/test'
