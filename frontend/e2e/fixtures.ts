import { test as base } from '@playwright/test'

import { ShellPage } from './pages/shell-page.ts'

/** Test fixtures: an application shell that is already open, signed in by the global setup. */
export const test = base.extend<{ shell: ShellPage }>({
  shell: async ({ page }, use) => {
    const shell = new ShellPage(page)
    await shell.goto()
    await use(shell)
  },
})

export { expect } from '@playwright/test'
