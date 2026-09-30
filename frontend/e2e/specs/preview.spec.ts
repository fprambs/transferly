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
 * - audio and video play streamed by ranges, with their length, picture size and speed; an AVI the browser cannot play says so
 * - PDFs open in the isolated page with pages, thumbnails, zoom and search; a large one is read by ranges, not whole; the JavaScript of a
 *   document never runs; protected and broken PDFs say so
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

  test('plays audio and video streamed by ranges, and says what the browser cannot play', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'video')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /clip\.webm/ }).click()
    await page.keyboard.press(' ')
    const viewer = page.getByRole('dialog', { name: 'clip.webm' })
    await expect(viewer).toContainText('0:05 · 320 × 180 px')
    await viewer.getByRole('button', { name: '2×' }).click()
    expect(await viewer.getByLabel('clip.webm').evaluate((video: HTMLVideoElement) => video.playbackRate)).toBe(2)
    await viewer.getByLabel('clip.webm').evaluate((video: HTMLVideoElement) => {
      video.currentTime = 2.5
    })
    await page.screenshot({ path: `${shots}/preview-video.png` })
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /unplayable\.avi/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'unplayable.avi' })).toContainText('This browser cannot play this file')
    await page.keyboard.press('Escape')
    await grid.getByRole('row', { name: /clip\.mp4/ }).click()
    await page.keyboard.press(' ')
    const mp4 = page.getByRole('dialog', { name: 'clip.mp4' })
    await expect(mp4.getByText('0:05 · 320 × 180 px').or(mp4.getByText('This browser cannot play this file'))).toBeVisible()
    await page.keyboard.press('Escape')

    await openSamples(page, quickId, 'audio')
    for (const name of ['tone.mp3', 'tone.ogg', 'tone.opus', 'tone.flac', 'tone.wav']) {
      await page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: new RegExp(name.replace('.', '\\.')) }).click()
      await page.keyboard.press(' ')
      await expect(page.getByRole('dialog', { name })).toContainText('0:04')
      if (name === 'tone.mp3') {
        await page.screenshot({ path: `${shots}/preview-audio.png` })
      }
      await page.keyboard.press('Escape')
    }
  })

  test('shows PDFs read by ranges in the isolated page, and never runs their JavaScript', async ({ shell, quickId }) => {
    const { page } = shell
    const dialogs: string[] = []
    page.on('dialog', (dialog) => {
      dialogs.push(dialog.message())
      void dialog.dismiss()
    })
    await openSamples(page, quickId, 'pdf')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /manual\.pdf/ }).click()
    await page.keyboard.press(' ')
    const manual = page.frameLocator('iframe[title="Document manual.pdf"]')
    await expect(manual.locator('.pdf-position')).toHaveText('Page 1 of 30')
    await expect(manual.locator('.pdf-page[data-page="1"] canvas')).toBeVisible()
    await manual.getByRole('button', { name: 'Pages' }).click()
    await expect(manual.locator('.pdf-thumb')).toHaveCount(30)
    await page.screenshot({ path: `${shots}/preview-pdf.png` })
    await manual.getByRole('searchbox', { name: 'Search the text' }).fill('quetzal')
    await manual.getByRole('searchbox', { name: 'Search the text' }).press('Enter')
    await expect(manual.locator('.pdf-found')).toHaveText('Found on page 17 · 1 pages')
    await manual.getByRole('button', { name: 'Zoom in' }).click()
    await expect(manual.locator('.pdf-zoom')).toHaveText('150%')
    await page.keyboard.press('Escape')

    let ranged = 0
    page.on('response', async (response) => {
      if (response.url().includes('large.pdf') && response.status() === 206) {
        ranged += Number(response.headers()['content-length'] ?? 0)
      }
    })
    await grid.getByRole('row', { name: /large\.pdf/ }).click()
    await page.keyboard.press(' ')
    const large = page.frameLocator('iframe[title="Document large.pdf"]')
    await expect(large.locator('.pdf-position')).toHaveText('Page 1 of 400')
    await expect(large.locator('.pdf-page[data-page="1"] canvas')).toBeVisible()
    expect(ranged).toBeGreaterThan(0)
    expect(ranged).toBeLessThan(3_700_000 / 2)
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /with-javascript\.pdf/ }).click()
    await page.keyboard.press(' ')
    await expect(page.frameLocator('iframe[title="Document with-javascript.pdf"]').locator('.pdf-position')).toHaveText('Page 1 of 1')
    await page.keyboard.press('Escape')
    expect(dialogs).toEqual([])

    for (const [name, message] of [
      ['protected.pdf', 'This PDF is protected with a password'],
      ['broken.pdf', 'This PDF cannot be opened'],
    ] as const) {
      await grid.getByRole('row', { name: new RegExp(name.replace('.', '\\.')) }).click()
      await page.keyboard.press(' ')
      await expect(page.getByRole('dialog', { name })).toContainText(message)
      await page.keyboard.press('Escape')
    }
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
