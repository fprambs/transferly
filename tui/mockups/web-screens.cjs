// Photographs the web side of signing in the terminal client (make tui-web-screens): the approval page, the code to paste and the
// terminal sessions in Security, against the isolated test stack of compose.test.yaml.
const path = require('node:path')
const crypto = require('node:crypto')
const { createRequire } = require('node:module')
const { chromium } = createRequire('/repo/frontend/package.json')('@playwright/test')

const base = 'http://transferly:8080'
const out = '/repo/docs/design/screens'
const account = { email: 'ana@acme.com', password: 'a long enough password' }

async function main() {
  const browser = await chromium.launch()
  const context = await browser.newContext({ baseURL: base, viewport: { width: 1280, height: 760 }, deviceScaleFactor: 1, locale: 'en-US' })
  const api = context.request
  let session = await api.post('/api/v1/setup', { data: account })
  if (session.status() === 409) session = await api.post('/api/v1/auth/login', { data: account })
  await session.json()
  const page = await context.newPage()
  const verifier = crypto.randomBytes(32).toString('base64url')
  const challenge = crypto.createHash('sha256').update(verifier).digest('base64url')
  const params = new URLSearchParams({ code_challenge: challenge, code_challenge_method: 'S256', state: 'st', client_name: 'transferly-tui', client_host: 'laptop' })
  await page.goto(`/cli/authorize?${params}&redirect_uri=${encodeURIComponent('http://127.0.0.1:49152/callback')}`)
  await page.getByRole('heading', { name: 'Sign in from the terminal?' }).waitFor()
  await page.waitForTimeout(800)
  await page.screenshot({ path: path.join(out, 'web-approve.png'), animations: 'disabled' })

  await page.goto(`/cli/authorize?${params}`)
  await page.getByLabel('Your password').fill(account.password)
  await page.getByRole('button', { name: 'Approve' }).click()
  await page.getByLabel('Sign-in code').waitFor()
  await page.waitForTimeout(800)
  await page.screenshot({ path: path.join(out, 'web-code.png'), animations: 'disabled' })

  const code = (await page.getByLabel('Sign-in code').textContent()).trim()
  const exchanged = await (await browser.newContext({ baseURL: base })).request.post('/api/v1/cli/token', { data: { code, codeVerifier: verifier } })
  if (!exchanged.ok()) throw new Error(`exchange ${exchanged.status()}`)
  await page.goto('/?settings=security')
  await page.getByRole('list', { name: 'Terminal sessions' }).waitFor()
  await page.getByRole('heading', { name: 'Terminal sessions' }).scrollIntoViewIfNeeded()
  await page.waitForTimeout(800)
  await page.screenshot({ path: path.join(out, 'web-sessions.png'), animations: 'disabled' })
  await browser.close()
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
