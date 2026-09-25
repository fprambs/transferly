import { readFile } from 'node:fs/promises'

import { test as base } from '@playwright/test'

import { quickConnectionPath } from './global-setup.ts'
import { ShellPage } from './pages/shell-page.ts'

/** Test fixtures: an application shell that is already open, signed in by the global setup, and the shared quick connection. */
export const test = base.extend<{ shell: ShellPage; quickId: string }>({
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
