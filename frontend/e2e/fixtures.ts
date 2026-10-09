import { readFile } from 'node:fs/promises'

import { test as base } from '@playwright/test'

import { quickConnectionPath } from './global-setup.ts'
import { ShellPage } from './pages/shell-page.ts'

/**
 * Test fixtures: the account's saved preferences (theme, language, double click), kept per test so parallel tests cannot change each
 * other's look or language (a test that wants the real account passes `accountTheme: 'real'`), an application shell that is already open, signed in by the global setup, the shared quick connection,
 * and an empty folder of the test's own on the SFTP server of the test stack, removed when the test ends, so what a test lists never depends on what other
 * tests left behind. A browser with nothing stored starts with the transfer queue, the log and the details open, as if the user had opened
 * them, so the queue does not collapse by itself while a test reads its rows and the log lines and details can be read. The quick
 * connections a page opens are closed when its test ends, so they never use up the quick connections of the account for later tests. */
export const test = base.extend<{ shell: ShellPage; quickId: string; folder: string; accountTheme: 'mocked' | 'real' }>({
  accountTheme: ['mocked', { option: true }],
  page: async ({ page, accountTheme }, use) => {
    await page.addInitScript(() => {
      if (globalThis.localStorage.getItem('transferly-ui') === null) {
        globalThis.localStorage.setItem('transferly-ui', JSON.stringify({ state: { queueOpen: true, logOpen: true, detailsOpen: true }, version: 0 }))
      }
    })
    if (accountTheme === 'mocked') {
      const preferences: Record<string, unknown> = { theme: 'transferly-dark' }
      await page.route('**/api/v1/me', async (route) => {
        const request = route.request()
        if (request.method() === 'PATCH') {
          Object.assign(preferences, request.postDataJSON() as Record<string, unknown>)
        }
        const response = await route.fetch({ method: 'GET', postData: undefined })
        if (!response.ok()) {
          await route.fulfill({ response })
          return
        }
        const body = (await response.json()) as { user: Record<string, unknown> }
        const user = { ...body.user, ...preferences }
        await route.fulfill({ status: 200, json: request.method() === 'PATCH' ? user : { ...body, user } })
      })
    }
    const opened: Array<Promise<string | undefined>> = []
    page.on('response', (response) => {
      if (response.request().method() === 'POST' && response.ok() && new URL(response.url()).pathname.endsWith('/quick-connections')) {
        opened.push(response.json().then((body: { id: string }) => `${response.url()}/${body.id}`, () => undefined))
      }
    })
    await use(page)
    const urls = (await Promise.all(opened)).filter((url) => url !== undefined)
    if (urls.length > 0) {
      const me = (await (await page.request.get('/api/v1/me')).json()) as { csrfToken: string }
      for (const url of urls) {
        await page.request.delete(url, { headers: { 'X-CSRF-Token': me.csrfToken } })
      }
    }
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
