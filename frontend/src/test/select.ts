import { screen } from '@testing-library/react'
import type { UserEvent } from '@testing-library/user-event'

/** Chooses an option of a {@link Select}: opens its list from the trigger and clicks the option with the name given. */
export async function choose(user: UserEvent, trigger: HTMLElement, option: string | RegExp): Promise<void> {
  await user.click(trigger)
  await user.click(await screen.findByRole('option', { name: option }))
}

/** Opens the list of a {@link Select}, reads the names of its options and closes it again with Escape. */
export async function optionsOf(user: UserEvent, trigger: HTMLElement): Promise<string[]> {
  await user.click(trigger)
  await screen.findAllByRole('option')
  const names = screen.getAllByRole('option').map((option) => (option.textContent ?? '').trim())
  await user.keyboard('{Escape}')
  return names
}
