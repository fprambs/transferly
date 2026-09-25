import { expect, test } from '../fixtures.ts'

/**
 * Smoke test of the production image.
 *
 * Scenarios:
 * - the shell renders its landmark regions
 * - the liveness and readiness probes answer ok
 * - the response carries the security headers and an unknown API path is a problem document
 * - a signed-in user who opens the login route lands on the shell
 */
test.describe('smoke', () => {
  test('renders the landmark regions', async ({ shell, page }) => {
    await expect(page).toHaveTitle('Sentinel')
    await expect(shell.heading).toHaveText('Sentinel')
    await expect(page.getByRole('banner')).toBeVisible()
    await expect(page.getByRole('navigation', { name: 'Sites' })).toBeVisible()
    await expect(page.getByRole('main', { name: 'File panes' })).toBeVisible()
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

    await expect(page.getByRole('heading', { level: 1, name: 'Sentinel' })).toBeVisible()
    await expect(page).toHaveURL(/\/$/)
  })
})
