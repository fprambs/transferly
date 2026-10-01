import type { Page } from '@playwright/test'

/**
 * Waits until the animations that end have ended, such as an entrance that fades a row in, so an accessibility check measures the colours
 * people read and not a frame halfway through. Endless ones, such as a spinner, are left running.
 */
export async function settle(page: Page): Promise<void> {
  await page.evaluate(async () => {
    const finite = document.getAnimations().filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity)
    await Promise.all(finite.map((animation) => animation.finished.catch(() => undefined)))
  })
}
