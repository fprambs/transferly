import { expect, test } from '../fixtures.ts'

/**
 * Smoke test of the production image.
 *
 * Scenarios:
 * - the shell renders its landmark regions
 * - the liveness and readiness probes answer ok
 * - the response carries the security headers and an unknown API path is a problem document
 * - a signed-in user who opens the login route lands on the shell
 * - a session that ends while browsing a site sends the user to login without reloading
 */
test.describe('smoke', () => {
  test('renders the landmark regions', async ({ shell, page }) => {
    await expect(page).toHaveTitle('Transferly')
    await expect(shell.heading).toHaveText('Transferly')
    await expect(page.getByRole('banner')).toBeVisible()
    await expect(page.getByRole('navigation', { name: 'Sites' })).toBeVisible()
    await expect(page.getByRole('main', { name: 'Explorer' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Transfers' })).toBeVisible()
  })

  test('answers the health probes', async ({ request }) => {
    for (const path of ['/healthz', '/readyz']) {
      const response = await request.get(path)
      expect(response.ok()).toBe(true)
      expect(await response.json()).toEqual({ status: 'ok' })
    }
  })

  test('sends security headers and problem documents', async ({ request }) => {
    const page = await request.get('/')
    const headers = page.headers()
    expect(headers['x-content-type-options']).toBe('nosniff')
    expect(headers['x-frame-options']).toBe('DENY')
    expect(headers['content-security-policy']).toContain("script-src 'self'")

    const missing = await request.get('/api/v1/unknown')
    expect(missing.status()).toBe(404)
    expect(missing.headers()['content-type']).toContain('application/problem+json')
    expect(await missing.json()).toMatchObject({ code: 'not_found', status: 404 })
  })

  test('sends a signed-in user from the login route to the shell', async ({ page }) => {
    await page.goto('/login')

    await expect(page.getByRole('heading', { level: 1, name: 'Transferly' })).toBeVisible()
    await expect(page).toHaveURL(/\/$/)
  })

  test('sends the user to login when the session ends while browsing', async ({ page, context, quickId }) => {
    await page.goto(`/sites/${quickId}?path=%2F`)
    await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()

    await context.clearCookies()
    await page.getByRole('toolbar', { name: 'Toolbar' }).getByRole('button', { name: 'Refresh the folder' }).click()

    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
    await expect(page).toHaveURL(/\/login$/)
  })
})
