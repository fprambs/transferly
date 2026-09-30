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
 * - text and code are highlighted, read-only, with lines that wrap on demand and the editor a click away; the styles of the editor load
 *   under the Content-Security-Policy, through the nonce of the page
 * - Markdown is rendered inside the isolated page; hostile Markdown keeps no script, handler, frame, form or javascript link, its links do
 *   not navigate, and its source is a click away
 * - JSON, YAML, XML and JSON Lines are trees; broken JSON and a YAML alias bomb say they do not parse
 * - CSV and TSV are tables that tell rows, columns and delimiter, and sort by column
 * - images show with their size, zoom, rotation and camera metadata: PNG, JPEG, GIF, WebP, AVIF, BMP, ICO, SVG and TIFF (on a canvas); a
 *   hostile SVG runs nothing, and a truncated PNG never breaks the viewer
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

  test('shows text and code highlighted, with lines that wrap on demand', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'text')
    await page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /code\.go/ }).click()
    await page.keyboard.press(' ')
    const viewer = page.getByRole('dialog', { name: 'code.go' })
    await expect(viewer.getByLabel('Contents of code.go')).toContainText('func hello(w http.ResponseWriter')
    await expect(viewer.locator('.cm-gutters')).toHaveCSS('display', 'flex')
    await expect(viewer).not.toContainText('no viewer of this kind')
    await expect(viewer.getByRole('button', { name: 'Open in the editor' })).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-code.png` })
    await viewer.getByRole('button', { name: 'Wrap lines' }).click()
    await expect(viewer.getByRole('button', { name: 'Wrap lines' })).toHaveAttribute('aria-pressed', 'true')
  })

  test('renders Markdown in the isolated page, where hostile content neither runs nor loads', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'markdown')
    await page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /README\.md/ }).click()
    await page.keyboard.press(' ')
    const rendered = page.frameLocator('iframe[title="Rendered README.md"]')
    await expect(rendered.getByRole('heading', { name: 'Sentinel preview sample' })).toBeVisible()
    await expect(rendered.getByRole('table')).toContainText('pdf.js')
    await page.screenshot({ path: `${shots}/preview-markdown.png` })

    await page.keyboard.press('ArrowLeft')
    const hostile = page.frameLocator('iframe[title="Rendered hostile.md"]')
    await expect(hostile.getByRole('heading', { name: 'Hostile Markdown' })).toBeVisible()
    await expect(hostile.locator('#viewer').locator('script, iframe, form, [onerror], [onclick]')).toHaveCount(0)
    await expect(hostile.locator('#viewer a[href^="javascript"]')).toHaveCount(0)
    await hostile.getByRole('link', { name: 'a link with a handler' }).click()
    await expect(page.getByRole('dialog', { name: 'hostile.md' })).toBeVisible()
    expect(await page.title()).toBe('Sentinel')
    await page.getByRole('dialog', { name: 'hostile.md' }).getByRole('button', { name: 'Source' }).click()
    await expect(page.getByLabel('Contents of hostile.md')).toContainText('<script>')
  })

  test('shows structured files as trees, and says what does not parse', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'data')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /package\.json/ }).click()
    await page.keyboard.press(' ')
    const tree = page.getByRole('tree', { name: 'Structure of package.json' })
    await expect(tree).toContainText('"sentinel-sample"')
    await expect(tree).toContainText('authors:')
    await page.screenshot({ path: `${shots}/preview-json.png` })
    await page.keyboard.press('Escape')

    for (const [name, expected] of [
      ['config.yaml', 'files.example.com'],
      ['feed.xml', 'Sentinel releases'],
      ['events.jsonl', 'download'],
    ] as const) {
      await grid.getByRole('row', { name: new RegExp(name.replace('.', '\\.')) }).click()
      await page.keyboard.press(' ')
      await expect(page.getByRole('tree', { name: `Structure of ${name}` })).toContainText(expected)
      await page.keyboard.press('Escape')
    }
    for (const name of ['broken.json', 'aliases-bomb.yaml']) {
      await grid.getByRole('row', { name: new RegExp(name.replace('.', '\\.')) }).click()
      await page.keyboard.press(' ')
      await expect(page.getByRole('dialog', { name }).getByRole('alert')).toContainText('This file does not parse')
      await page.keyboard.press('Escape')
    }
  })

  test('shows CSV and TSV as tables that sort', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'data')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /people\.csv/ }).click()
    await page.keyboard.press(' ')
    const viewer = page.getByRole('dialog', { name: 'people.csv' })
    await expect(viewer).toContainText('2000 rows · 6 columns · separated by commas')
    await viewer.getByRole('columnheader', { name: 'files' }).click()
    await expect(viewer.getByRole('columnheader', { name: 'files' })).toHaveAttribute('aria-sort', 'ascending')
    await page.screenshot({ path: `${shots}/preview-csv.png` })
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /semicolon\.csv/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'semicolon.csv' })).toContainText('separated by semicolons')
    await expect(page.getByRole('dialog', { name: 'semicolon.csv' }).getByRole('cell', { name: '1.299,90' })).toBeVisible()
  })

  test('shows images with their size, zoom, rotation and camera metadata', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'images')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /sample\.jpg/ }).click()
    await page.keyboard.press(' ')
    const viewer = page.getByRole('dialog', { name: 'sample.jpg' })
    await expect(viewer).toContainText('640 × 400 px')
    await viewer.getByRole('button', { name: 'Info' }).click()
    await expect(viewer.getByRole('complementary', { name: 'Info' })).toContainText('Sentinel Camera Preview One')
    await expect(viewer.getByRole('complementary', { name: 'Info' })).toContainText('200')
    await page.screenshot({ path: `${shots}/preview-image.png` })
    await viewer.getByRole('button', { name: 'Zoom in' }).click()
    await expect(viewer).toContainText('150%')
    await viewer.getByRole('button', { name: 'Rotate right' }).click()
    await expect(viewer.getByRole('img', { name: 'sample.jpg' })).toHaveAttribute('style', /rotate\(90deg\)/)
    await page.keyboard.press('Escape')

    for (const name of ['sample.png', 'sample.webp', 'sample.avif', 'sample.bmp', 'sample.tiff', 'animated.gif', 'sample.ico', 'logo.svg']) {
      await grid.getByRole('row', { name: new RegExp(name.replace('.', '\\.')) }).click()
      await page.keyboard.press(' ')
      await expect(page.getByRole('dialog', { name }).getByText(/\d+ × \d+ px/).first()).toBeVisible()
      if (name === 'sample.tiff') {
        await page.screenshot({ path: `${shots}/preview-tiff.png` })
      }
      await page.keyboard.press('Escape')
    }

    await grid.getByRole('row', { name: /hostile\.svg/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'hostile.svg' }).getByText('320 × 200 px')).toBeVisible()
    expect(await page.title()).toBe('Sentinel')
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /truncated\.png/ }).click()
    await page.keyboard.press(' ')
    const truncated = page.getByRole('dialog', { name: 'truncated.png' })
    await expect(truncated.getByText('This image cannot be shown').or(truncated.getByText(/\d+ × \d+ px/))).toBeVisible()
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
