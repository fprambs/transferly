import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Where the screenshots of the viewers are written, relative to the frontend folder. */
const shots = 'shots'

/** Opens a folder of the preview samples mounted into the SFTP server of the test stack. */
async function openSamples(page: Page, quickId: string, folder: string): Promise<void> {
  await page.goto(`/sites/${quickId}?path=${encodeURIComponent(`/preview-samples/${folder}`)}`)
  await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()
}

/**
 * The file viewer (ADR-0018) against the preview samples of the test stack.
 *
 * Scenarios:
 * - Space opens the selected file full screen; a binary file shows its bytes in hexadecimal, read by ranges; the arrows walk the folder;
 *   an empty file says so; binary bytes named .txt are shown as binary; Escape closes and leaves the file selected
 * - a text file, whose viewer comes in a later stage, is shown as bytes with a note that says so
 * - the isolated page of the viewers answers inside a sandboxed frame, and has no cookies, no access to the parent and no network
 */
test.describe('file viewer', () => {
  test('shows binary files as bytes and walks the folder', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'binary')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /empty\.bin/ }).click()
    await page.keyboard.press(' ')

    const viewer = page.getByRole('dialog', { name: 'empty.bin' })
    await expect(viewer).toContainText('This file is empty.')
    await page.keyboard.press('ArrowRight')
    const mislabeled = page.getByRole('dialog', { name: 'mislabeled.txt' })
    await expect(mislabeled).toContainText('Binary file')
    await expect(mislabeled.getByRole('table', { name: 'Bytes of mislabeled.txt' })).toContainText('13 37 00 ff')
    await page.keyboard.press('ArrowRight')
    const random = page.getByRole('dialog', { name: 'random-64k.bin' })
    await expect(random).toContainText('64.0 KB · 3 of 3')
    await expect(random.getByRole('table')).toContainText('00000000')
    await page.screenshot({ path: `${shots}/preview-hex.png` })

    await page.keyboard.press('Escape')
    await expect(random).toBeHidden()
    await expect(grid.getByRole('row', { name: /random-64k\.bin/ })).toHaveAttribute('aria-selected', 'true')
  })

  test('shows a file without a viewer yet as bytes, and says so', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'text')
    await page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /plain\.txt/ }).click()
    await page.keyboard.press(' ')
    const viewer = page.getByRole('dialog', { name: 'plain.txt' })
    await expect(viewer).toContainText('no viewer of this kind of file exists yet')
    await expect(viewer.getByRole('button', { name: 'Open in the editor' })).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-fallback.png` })
  })

  test('keeps the isolated page of the viewers away from the session', async ({ shell }) => {
    const { page } = shell
    const ready = page.evaluate(
      () =>
        new Promise<string>((resolve) => {
          const frame = document.createElement('iframe')
          frame.setAttribute('sandbox', 'allow-scripts')
          frame.name = 'viewer-probe'
          window.addEventListener('message', (event) => {
            if (event.source === frame.contentWindow) {
              resolve(String((event.data as { type: string }).type))
            }
          })
          frame.src = '/sandbox/viewer.html'
          document.body.append(frame)
        }),
    )
    expect(await ready).toBe('sentinel:ready')
    const frame = page.frame({ name: 'viewer-probe' })
    expect(frame).not.toBeNull()
    const probe = await frame!.evaluate(async () => {
      const blocked = (action: () => unknown): boolean => {
        try {
          action()
          return false
        } catch {
          return true
        }
      }
      const network = await fetch('/api/v1/me').then(
        () => 'reached',
        () => 'blocked',
      )
      return { origin: window.origin, cookie: blocked(() => document.cookie), parent: blocked(() => window.parent.document.title), network }
    })
    expect(probe).toEqual({ origin: 'null', cookie: true, parent: true, network: 'blocked' })
  })
})
