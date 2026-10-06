import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Where the screenshots of the viewers are written, relative to the frontend folder. */
const shots = 'shots'

/** Opens a folder of the preview samples mounted into the SFTP server of the test stack, sorted by name, since the arrows walk it in order. */
async function openSamples(page: Page, quickId: string, folder: string): Promise<void> {
  await page.goto(`/sites/${quickId}?path=${encodeURIComponent(`/preview-samples/${folder}`)}`)
  const grid = page.getByRole('grid', { name: 'Files' })
  await expect(grid).toBeVisible()
  const name = grid.getByRole('columnheader', { name: 'Name' })
  if ((await name.getAttribute('aria-sort')) !== 'ascending') {
    await grid.getByRole('button', { name: 'Name' }).click()
  }
  await expect(name).toHaveAttribute('aria-sort', 'ascending')
}

/**
 * The file viewer (ADR-0018) against the preview samples of the test stack.
 *
 * Scenarios:
 * - Space opens the selected file full screen; a file no viewer shows says so instead of opening its bytes, and shows them in hexadecimal,
 *   read by ranges, on request; the arrows walk the folder; an empty file says so; binary bytes named .txt are taken for binary; Escape
 *   closes and leaves the file selected
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
 *   document never runs; a protected PDF asks for its password inside the isolated page, says when it is wrong and opens with the right
 *   one; a broken PDF says so
 * - archives read by the server show as trees with their format, totals, sizes and compression: ZIP, 7z, RAR, TAR and compressed TARs,
 *   and a single compressed file; an entry opens with its own viewer, an archive nested in another included, and Backspace goes back;
 *   hostile names are marked, a bomb, an encrypted entry, a truncated ZIP and a 7z with encrypted headers are refused with guidance, and
 *   a listing of 12 000 entries stops at 10 000 and says so
 * - Office documents open in the isolated page: a Word document laid out as pages with its bullets, table and picture; an OpenDocument
 *   text; an Excel workbook with its sheets, merged cells, formatted numbers and saved results; legacy Excel and OpenDocument workbooks;
 *   a PowerPoint deck as slides; a hostile document renders without its embedded HTML or remote picture; a legacy Word file says no
 *   viewer shows it and a truncated document says it cannot be opened
 * - a SQLite database opens in the isolated page with its tables and views, rows by pages, NULLs and blobs, its structure, and read-only
 *   queries that refuse writes; a truncated database says it cannot be opened
 * - fonts open in the isolated page with their facts, samples that follow the text typed and the grid of their characters: TrueType,
 *   WOFF and WOFF2; a truncated font says it cannot be opened
 * - certificates and keys show their facts: a chain with validity and fingerprints, an expired certificate, a certificate in DER, a public
 *   key, a certificate request and an OpenSSH key; a private key is never shown and a damaged block says it cannot be read
 * - email messages show their headers, their HTML cleaned in the isolated page with their inline picture, their text, header lines and
 *   attachments that open with their own viewer; a hostile message runs nothing and loads nothing from the network, and says so
 * - the isolated page of the viewers answers inside a sandboxed frame, and has no cookies, no access to the parent and no network
 */
test.describe('file viewer', () => {
  test('says when no viewer shows a file, offers its bytes and walks the folder', async ({ shell, quickId }) => {
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
    await expect(mislabeled).toContainText('No viewer for this kind of file')
    await expect(mislabeled.getByRole('table')).toHaveCount(0)
    await page.screenshot({ path: `${shots}/preview-no-viewer.png`, animations: 'disabled' })
    await mislabeled.getByRole('button', { name: 'Show its bytes anyway' }).click()
    await expect(mislabeled.getByRole('table', { name: 'Bytes of mislabeled.txt' })).toContainText('13 37 00 ff')
    await page.keyboard.press('ArrowRight')
    const random = page.getByRole('dialog', { name: 'random-64k.bin' })
    await expect(random).toContainText('64.0 KB · 3 of 3')
    await random.getByRole('button', { name: 'Show its bytes anyway' }).click()
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
    await expect(viewer).not.toContainText('No viewer for this kind of file')
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
    await expect(rendered.getByRole('heading', { name: 'Transferly preview sample' })).toBeVisible()
    await expect(rendered.getByRole('table')).toContainText('pdf.js')
    await page.screenshot({ path: `${shots}/preview-markdown.png` })

    await page.keyboard.press('ArrowLeft')
    const hostile = page.frameLocator('iframe[title="Rendered hostile.md"]')
    await expect(hostile.getByRole('heading', { name: 'Hostile Markdown' })).toBeVisible()
    await expect(hostile.locator('#viewer').locator('script, iframe, form, [onerror], [onclick]')).toHaveCount(0)
    await expect(hostile.locator('#viewer a[href^="javascript"]')).toHaveCount(0)
    await hostile.getByRole('link', { name: 'a link with a handler' }).click()
    await expect(page.getByRole('dialog', { name: 'hostile.md' })).toBeVisible()
    expect(await page.title()).toBe('Transferly')
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
    await expect(tree).toContainText('"transferly-sample"')
    await expect(tree).toContainText('authors:')
    await page.screenshot({ path: `${shots}/preview-json.png` })
    await page.keyboard.press('Escape')

    for (const [name, expected] of [
      ['config.yaml', 'files.example.com'],
      ['feed.xml', 'Transferly releases'],
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
    await expect(viewer.getByRole('complementary', { name: 'Info' })).toContainText('Transferly Camera Preview One')
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
    expect(await page.title()).toBe('Transferly')
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

    await grid.getByRole('row', { name: /protected\.pdf/ }).click()
    await page.keyboard.press(' ')
    const locked = page.frameLocator('iframe[title="Document protected.pdf"]')
    const password = locked.getByRole('dialog', { name: 'This PDF is protected with a password' })
    await expect(password).toBeVisible()
    await locked.getByRole('textbox', { name: 'Password' }).fill('wrong')
    await locked.getByRole('textbox', { name: 'Password' }).press('Enter')
    await expect(locked.getByRole('alert')).toHaveText('That password is not right. Try again.')
    await page.screenshot({ path: `${shots}/preview-pdf-password.png` })
    await locked.getByRole('textbox', { name: 'Password' }).fill('transferly')
    await locked.getByRole('button', { name: 'Open' }).click()
    await expect(locked.locator('.pdf-position')).toHaveText(/^Page 1 of \d+$/)
    await expect(password).toBeHidden()
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /broken\.pdf/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'broken.pdf' })).toContainText('This PDF cannot be opened')
    await page.keyboard.press('Escape')
  })

  test('shows archives as trees read by the server, and opens their entries with their own viewers', async ({ shell, quickId }) => {
    test.setTimeout(60_000)
    const { page } = shell
    await openSamples(page, quickId, 'archives')
    const grid = page.getByRole('grid', { name: 'Files' })
    const open = async (name: string) => {
      await grid.getByRole('row', { name: new RegExp(`^${name.replaceAll('.', '\\.')}(?![\\w.])`) }).click()
      await page.keyboard.press(' ')
      return page.getByRole('dialog', { name })
    }

    const bomb = await open('bomb.zip')
    await expect(bomb.getByRole('treeitem', { name: /zeros\.bin/ })).toContainText('decompression bomb?')
    await bomb.getByRole('treeitem', { name: /zeros\.bin/ }).dblclick()
    await expect(bomb).toContainText('Too large to show here')
    await page.screenshot({ path: `${shots}/preview-archive-bomb.png`, animations: 'disabled' })
    await page.keyboard.press('Escape')

    const hostile = await open('hostile-paths.zip')
    await expect(hostile.getByText('unsafe name')).toHaveCount(4)
    await page.screenshot({ path: `${shots}/preview-archive-hostile.png` })
    await page.keyboard.press('Escape')

    const many = await open('many-entries.zip')
    await expect(many).toContainText('Only the first 10000 entries are listed')
    await page.keyboard.press('Escape')

    for (const name of ['project.7z', 'project.rar', 'project.tar', 'project.tar.bz2', 'project.tar.gz', 'project.tar.xz', 'project.tar.zst']) {
      const archive = await open(name)
      await expect(archive.getByRole('tree')).toContainText('README.md')
      await page.keyboard.press('Escape')
    }
    const zip = await open('project.zip')
    const tree = zip.getByRole('tree', { name: 'Contents of project.zip' })
    await expect(zip).toContainText('8 files')
    await expect(tree.getByRole('treeitem', { name: /README\.md/ })).toBeVisible()
    await expect(tree.getByRole('treeitem', { name: /inner\.zip/ })).toBeHidden()
    await tree.getByRole('treeitem', { name: /^vendor/ }).dblclick()
    await expect(tree.getByRole('treeitem', { name: /inner\.zip/ })).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-archive-tree.png` })

    await tree.getByRole('treeitem', { name: 'images' }).click()
    await page.keyboard.press('Enter')
    await tree.getByRole('treeitem', { name: /sample\.png/ }).dblclick()
    await expect(zip.getByRole('img', { name: 'sample.png' })).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-archive-entry.png` })
    await page.keyboard.press('Backspace')
    await expect(tree).toBeVisible()

    await tree.getByRole('treeitem', { name: /inner\.zip/ }).dblclick()
    const nested = zip.getByRole('tree', { name: 'Contents of inner.zip' })
    await nested.getByRole('treeitem', { name: /plain\.txt/ }).dblclick()
    await expect(zip.getByLabel('Contents of plain.txt', { exact: true })).toBeVisible()
    await page.keyboard.press('Escape')

    const header = await open('secret.7z')
    await expect(header).toContainText('archive_encrypted')
    await page.keyboard.press('Escape')

    const secret = await open('secret.zip')
    await secret.getByRole('treeitem', { name: /README\.md/ }).dblclick()
    await expect(secret).toContainText('archive_encrypted')
    await page.keyboard.press('Escape')

    const log = await open('server.log.gz')
    await log.getByRole('treeitem', { name: /server\.log/ }).dblclick()
    await expect(log.getByLabel('Contents of server.log', { exact: true })).toContainText('transfer 0 done')
    await page.keyboard.press('Escape')

    const truncated = await open('truncated.zip')
    await expect(truncated).toContainText('archive_unsupported')
    await page.keyboard.press('Escape')
  })

  test('shows Office documents in the isolated page', async ({ shell, quickId }) => {
    test.setTimeout(60_000)
    const { page } = shell
    const requests: string[] = []
    page.on('request', (request) => requests.push(request.url()))
    await openSamples(page, quickId, 'office')
    const grid = page.getByRole('grid', { name: 'Files' })
    const open = async (name: string) => {
      await grid.getByRole('row', { name: new RegExp(`^${name.replace('.', '\\.')}`) }).click()
      await page.keyboard.press(' ')
      return page.frameLocator(`iframe[title="Document ${name}"]`)
    }

    const docx = await open('document.docx')
    await expect(docx.getByText('Transferly preview sample').first()).toBeVisible()
    await expect(docx.locator('table').first()).toContainText('Bogotá')
    await expect(docx.locator('img').first()).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-docx.png` })
    await page.keyboard.press('Escape')

    const odt = await open('document.odt')
    await expect(odt.locator('.odt-page h1')).toHaveText('Transferly preview sample')
    await expect(odt.locator('.odt-page li')).toHaveCount(3)
    await page.screenshot({ path: `${shots}/preview-odt.png` })
    await page.keyboard.press('Escape')

    const hostile = await open('hostile.docx')
    await expect(hostile.getByText('Hostile document')).toBeVisible()
    await expect(hostile.getByText('embedded html', { exact: true })).toHaveCount(0)
    await expect(hostile.locator('iframe')).toHaveCount(0)
    await page.keyboard.press('Escape')
    expect(requests.filter((url) => url.includes('example.com'))).toEqual([])

    await grid.getByRole('row', { name: /^legacy\.doc/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'legacy.doc' })).toContainText('No viewer for this kind of file')
    await page.keyboard.press('Escape')

    const xls = await open('legacy.xls')
    await expect(xls.locator('table.sheet')).toContainText('Santiago')
    await page.keyboard.press('Escape')

    const ods = await open('sheet.ods')
    await expect(ods.getByRole('tab')).toHaveText(['People', 'Cities'])
    await page.keyboard.press('Escape')

    const deck = await open('slides.pptx')
    await expect(deck.locator('.slides-summary')).toHaveText('Slides: 4')
    await expect(deck.locator('.slide')).toHaveCount(4)
    await expect(deck.locator('.slide').nth(2).locator('img')).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-pptx.png` })
    await deck.locator('.slide').nth(2).scrollIntoViewIfNeeded()
    await page.screenshot({ path: `${shots}/preview-pptx-shapes.png` })
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /^truncated\.docx/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'truncated.docx' })).toContainText('This document cannot be opened')
    await page.keyboard.press('Escape')

    const xlsx = await open('workbook.xlsx')
    await expect(xlsx.getByRole('tab')).toHaveText(['Sales', 'Notes', 'Big'])
    await expect(xlsx.locator('table.sheet tbody tr').nth(6)).toContainText('4,471.50')
    await page.screenshot({ path: `${shots}/preview-xlsx.png` })
    await xlsx.getByRole('tab', { name: 'Big' }).click()
    await expect(xlsx.locator('table.sheet tbody tr')).toHaveCount(5001)
    await page.keyboard.press('Escape')
  })

  test('browses SQLite databases in the isolated page', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'sqlite')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /^sample\.sqlite/ }).click()
    await page.keyboard.press(' ')
    const db = page.frameLocator('iframe[title="Database sample.sqlite"]')
    await expect(db.locator('.sqlite-count')).toHaveText('Rows 1–200 of 20000')
    await db.getByRole('button', { name: 'Next' }).click()
    await expect(db.locator('.sqlite-count')).toHaveText('Rows 201–400 of 20000')
    await db.getByRole('button', { name: /^people/ }).click()
    await expect(db.locator('table.sheet')).toContainText('Émilie 日本語 😀')
    await expect(db.locator('.sqlite-null').first()).toHaveText('NULL')
    await page.screenshot({ path: `${shots}/preview-sqlite.png` })
    await db.getByRole('button', { name: 'Structure' }).click()
    await expect(db.locator('.sqlite-sql')).toContainText('CREATE TABLE people')
    const query = db.getByRole('textbox', { name: 'Query' })
    await query.fill('SELECT city, COUNT(*) AS n FROM people GROUP BY city ORDER BY city')
    await query.press('Control+Enter')
    await expect(db.locator('.sqlite-title')).toHaveText('Query result')
    await page.screenshot({ path: `${shots}/preview-sqlite-query.png` })
    await query.fill('DROP TABLE people')
    await db.getByRole('button', { name: 'Run' }).click()
    await expect(db.getByRole('alert')).toBeVisible()
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /^truncated\.sqlite/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'truncated.sqlite' })).toContainText('This database cannot be opened')
    await page.keyboard.press('Escape')
  })

  test('shows fonts in the isolated page', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'fonts')
    const grid = page.getByRole('grid', { name: 'Files' })
    for (const name of ['blocks.ttf', 'blocks.woff', 'blocks.woff2']) {
      await grid.getByRole('row', { name: new RegExp(`^${name.replace('.', '\\.')} `) }).click()
      await page.keyboard.press(' ')
      const font = page.frameLocator(`iframe[title="Font ${name}"]`)
      await expect(font.locator('.font-sample')).toHaveCount(6)
      if (name === 'blocks.ttf') {
        await expect(font.locator('h1')).toHaveText('Transferly Blocks Regular')
        await expect(font.locator('.font-char')).toHaveCount(16)
        await font.getByRole('textbox', { name: 'Try your own text' }).fill('TRANSFERLY')
        await expect(font.locator('.font-sample').first()).toHaveText('TRANSFERLY')
        expect(await font.locator('.font-sample').first().evaluate(() => document.fonts.check('24px "Transferly Preview Font"'))).toBe(true)
        await page.screenshot({ path: `${shots}/preview-font.png` })
      }
      await page.keyboard.press('Escape')
    }
    await grid.getByRole('row', { name: /^truncated\.ttf/ }).click()
    await page.keyboard.press(' ')
    await expect(page.getByRole('dialog', { name: 'truncated.ttf' })).toContainText('This font cannot be opened')
    await page.keyboard.press('Escape')
  })

  test('shows certificates and keys, and never a private key', async ({ shell, quickId }) => {
    const { page } = shell
    await openSamples(page, quickId, 'certificates')
    const grid = page.getByRole('grid', { name: 'Files' })
    const open = async (name: string) => {
      await grid.getByRole('row', { name: new RegExp(`^${name.replace('.', '\\.')} `) }).click()
      await page.keyboard.press(' ')
      return page.getByRole('dialog', { name })
    }
    const chain = await open('chain.pem')
    await expect(chain).toContainText('2 items')
    await expect(chain).toContainText('files.example.com, *.files.example.com, 192.0.2.10')
    await expect(chain.getByText('Certificate authority')).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-certificate.png`, animations: 'disabled' })
    await page.keyboard.press('Escape')
    await expect(await open('expired.pem')).toContainText('Expired on')
    await page.keyboard.press('Escape')
    await expect(await open('leaf.cer')).toContainText('SHA-256')
    await page.keyboard.press('Escape')
    await expect(await open('id_ed25519.pub')).toContainText('SHA256:')
    await page.keyboard.press('Escape')
    const secret = await open('private-key.pem')
    await expect(secret).toContainText('Private key — contents not shown')
    await expect(secret.getByRole('button', { name: 'Source' })).toHaveCount(0)
    await page.screenshot({ path: `${shots}/preview-private-key.png`, animations: 'disabled' })
    await page.keyboard.press('Escape')
    await expect(await open('broken.pem')).toContainText('This part cannot be read')
    await page.keyboard.press('Escape')
  })

  test('shows email messages, and never runs or loads what they hold', async ({ shell, quickId }) => {
    const { page } = shell
    const requests: string[] = []
    const dialogs: string[] = []
    page.on('request', (request) => requests.push(request.url()))
    page.on('dialog', (dialog) => {
      dialogs.push(dialog.message())
      void dialog.dismiss()
    })
    await openSamples(page, quickId, 'mail')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /^newsletter\.eml/ }).click()
    await page.keyboard.press(' ')
    const viewer = page.getByRole('dialog', { name: 'newsletter.eml' })
    await expect(viewer.getByRole('heading', { name: 'Informe de septiembre · 📊 resultados' })).toBeVisible()
    const body = page.frameLocator('iframe[title="Message newsletter.eml"]')
    await expect(body.getByRole('heading', { name: 'Informe de septiembre' })).toBeVisible()
    await expect(body.getByRole('img', { name: 'logo' })).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-email.png`, animations: 'disabled' })
    await viewer.getByRole('list', { name: 'Attachments' }).getByText('grafico.png').click()
    await expect(viewer.getByRole('img', { name: 'grafico.png' })).toBeVisible()
    await viewer.getByRole('button', { name: 'newsletter.eml' }).click()
    await viewer.getByRole('button', { name: 'Headers' }).click()
    await expect(viewer).toContainText('Message-ID:')
    await page.keyboard.press('Escape')

    await grid.getByRole('row', { name: /^hostile\.eml/ }).click()
    await page.keyboard.press(' ')
    const hostile = page.getByRole('dialog', { name: 'hostile.eml' })
    await expect(hostile).toContainText('Remote images and content of this message were not loaded.')
    await expect(page.frameLocator('iframe[title="Message hostile.eml"]').getByText('click me')).toBeVisible()
    await page.screenshot({ path: `${shots}/preview-email-hostile.png`, animations: 'disabled' })
    await page.keyboard.press('Escape')
    expect(dialogs).toEqual([])
    expect(requests.filter((url) => url.includes('example.com'))).toEqual([])
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
    expect(await ready).toBe('transferly:ready')
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
