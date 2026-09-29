import type { Locator } from '@playwright/test'

/** Chooses an option of one of the interface's selects: opens its list from the trigger and clicks the option with the name given. */
export async function choose(select: Locator, option: string | RegExp): Promise<void> {
  await select.click()
  await select.page().getByRole('option', { name: option, exact: typeof option === 'string' }).click()
}
