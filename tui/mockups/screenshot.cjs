// Photographs the HTML pages tui/mockups wrote, one PNG per screen, with the Chromium of the Playwright image (make tui-mockups).
// Usage: node screenshot.cjs <folder of pages> <folder of images>; Playwright comes from frontend/node_modules.
const fs = require('node:fs')
const path = require('node:path')
const { createRequire } = require('node:module')

const { chromium } = createRequire(path.resolve(__dirname, '../../frontend/package.json'))('@playwright/test')

/** Opens every page of the first folder and saves the screen element as a PNG of the same name in the second folder. */
async function main() {
  const [pages, images] = process.argv.slice(2)
  fs.mkdirSync(images, { recursive: true })
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 }, deviceScaleFactor: 1 })
  for (const name of fs.readdirSync(pages).filter((file) => file.endsWith('.html')).sort()) {
    await page.goto(`file://${path.resolve(pages, name)}`)
    await page.locator('#screen').screenshot({ path: path.join(images, name.replace(/\.html$/, '.png')) })
  }
  await browser.close()
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
